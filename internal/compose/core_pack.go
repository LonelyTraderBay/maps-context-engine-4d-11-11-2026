package compose

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const packFactLimit = 8 * 1024 * 1024
const packSourceLimit int64 = 8 * 1024 * 1024

func (b *CoreBackend) setPackGraphAuthority(request *index.PublishedGraphRequest, principal app.CorePrincipal, config model.PolicyConfig) {
	member := request.Member
	checkScopePath := func(worktree, digest string, epoch int64) error {
		if worktree != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		return nil
	}
	if checkMetadata := b.Authority.CheckMetadataPath; checkMetadata != nil {
		request.AuthorizeMetadataPath = func(ctx context.Context, worktree, name, digest string, epoch int64) error {
			if err := checkScopePath(worktree, digest, epoch); err != nil {
				return err
			}
			if err := checkMetadata(name, config.DeniedGlobs); err != nil {
				return ErrAuthority
			}
			return ctx.Err()
		}
	}
	if checkBoundPath := b.Authority.AuthorizeBoundPath; checkBoundPath != nil {
		request.AuthorizePath = func(ctx context.Context, worktree, name, digest string, epoch int64) error {
			if err := checkScopePath(worktree, digest, epoch); err != nil {
				return err
			}
			if err := checkBoundPath(ctx, principal, config, name); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return ErrAuthority
			}
			return ctx.Err()
		}
	}
}

func (b *CoreBackend) packGuard(principal app.CorePrincipal) contextpack.CursorGuard {
	return func(ctx context.Context, binding contextpack.CursorBinding) error {
		if err := b.checkPackPrincipal(ctx, principal, binding); err != nil {
			return err
		}
		_, err := b.Reader.Recheck(ctx, principal, binding.Scope)
		if errors.Is(err, ErrScopeSnapshotExpired) {
			return app.ErrCoreSnapshotExpired
		}
		return err
	}
}

func (b *CoreBackend) checkPackPrincipal(ctx context.Context, principal app.CorePrincipal, binding contextpack.CursorBinding) error {
	if binding.PrincipalDigest != principal.Digest || binding.BindingDigest != principal.BindingDigest ||
		(binding.OrderVersion != "pack-v1" && binding.OrderVersion != contextpack.ExplorePackOrder) {
		return ErrAuthority
	}
	return b.Authority.samePrincipal(ctx, principal)
}

func (b *CoreBackend) ContextPack(ctx context.Context, principal app.CorePrincipal, request model.PackRequest, options app.CoreCallOptions) ([][]byte, error) {
	if ctx == nil {
		return nil, app.ErrCoreInput
	}
	if !b.valid() || b.packCache == nil || b.buildGate == nil {
		return nil, app.ErrCoreUnsupported
	}
	if request.View == nil || *request.View != "symbol" && *request.View != "overview" {
		return nil, app.ErrCoreUnsupported
	}
	if options.DeliveryMode == "cli" && !b.SupportsCLIPackRetention() {
		return nil, app.ErrCoreUnsupported
	}
	if *request.View == "overview" && (request.TaskKind != "plan" && request.TaskKind != "explore" || b.planSource == nil) {
		return nil, app.ErrCoreUnsupported
	}
	if request.FreshnessMode == "checked_workspace" && b.verifyWorkspace == nil {
		return nil, app.ErrFreshnessUnknown
	}
	select {
	case b.buildGate <- struct{}{}:
		defer func() { <-b.buildGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	observation, err := b.resolveRequestScope(ctx, principal, request.Scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	member := observation.Vector.Members[0]
	if options.MaxOutputBytes > observation.Binding.Policy.MaxOutputBytes {
		options.MaxOutputBytes = observation.Binding.Policy.MaxOutputBytes
	}
	maxFile := minInt64(observation.Binding.Policy.MaxFileBytes, b.limits.MaxFileBytes, packSourceLimit)
	maxSource := minInt64(observation.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot, packSourceLimit)
	if maxFile < 1 || maxSource < 1 {
		return nil, app.ErrCoreLimit
	}
	maxScan := maxInt64(maxFile, int64(b.Reader.MaxManifestBytes), packFactLimit)
	scanner, err := policy.NewScanner(int(maxScan))
	if err != nil {
		return nil, err
	}
	authorizeGeneration := func(ctx context.Context, worktree, profile, digest string, epoch int64) error {
		if worktree != member.WorktreeID || profile != member.ProfileID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		return b.Authority.samePrincipal(ctx, principal)
	}
	authorizePath := func(ctx context.Context, worktree, name, digest string, epoch int64) error {
		if worktree != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		if err := b.Authority.samePrincipal(ctx, principal); err != nil {
			return err
		}
		return b.Authority.path(ctx, principal, observation.Binding.Policy, name)
	}
	if *request.View == "overview" {
		return b.contextPlan(ctx, principal, request, options, observation, scanner, maxFile, maxSource, authorizeGeneration, authorizePath)
	}
	resolvePosition := func(ctx context.Context, position model.PositionSelector, scope model.ScopeVector, service *string) (model.SymbolRef, error) {
		if service != nil || model.ComparePinnedScope(scope, observation.Vector, 32768) != nil {
			return model.SymbolRef{}, ErrAuthority
		}
		result, err := b.Reader.Index.Search(ctx, search.Request{ProfileID: member.ProfileID, GenerationID: member.GenerationID, Mode: "position", Position: &position, Limit: 2, MaxOutputBytes: 1024 * 1024, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner})
		if err != nil {
			return model.SymbolRef{}, coreStoreError(err)
		}
		if result.Ambiguous || result.Truncated {
			return model.SymbolRef{}, app.ErrCoreAmbiguous
		}
		if len(result.Symbols) != 1 || result.GenerationID != member.GenerationID || result.IndexIncarnation != member.IndexIncarnation || result.SnapshotDigest != member.SnapshotDigest {
			return model.SymbolRef{}, contextpack.ErrTarget
		}
		return model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SymbolID: result.Symbols[0].Symbol.SymbolID}, nil
	}
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	var target model.SymbolRef
	switch selector := request.Selector.(type) {
	case *model.SymbolSelector:
		if selector == nil {
			return nil, app.ErrCoreInput
		}
		target = model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: selector.SymbolID}
	case *model.RefSelector:
		if selector == nil || selector.ServiceID != nil {
			return nil, app.ErrCoreUnsupported
		}
		ref, ok := selector.Ref.(*model.SymbolRef)
		if !ok || ref == nil {
			return nil, app.ErrCoreUnsupported
		}
		if _, err := model.MatchPinnedMember(observation.Vector, ref, nil); err != nil {
			return nil, app.ErrCoreInput
		}
		target = *ref
	case *model.PositionSelector:
		if selector == nil {
			return nil, app.ErrCoreInput
		}
		target, err = resolvePosition(ctx, *selector, observation.Vector, nil)
		if err != nil {
			return nil, err
		}
	default:
		return nil, app.ErrCoreUnsupported
	}
	graphRequest := index.PublishedGraphRequest{Member: member, VectorDigest: observation.Vector.VectorDigest, AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000}
	b.setPackGraphAuthority(&graphRequest, principal, observation.Binding.Policy)
	graph, err := b.Reader.Index.ReadPublishedGraph(ctx, graphRequest)
	if err != nil {
		return nil, coreStoreError(err)
	}
	scopeAuth := func(ctx context.Context, scope model.ScopeVector, _ model.ScopeMember) error {
		_, err := b.Reader.Recheck(ctx, principal, scope)
		return err
	}
	pathAuth := func(ctx context.Context, member model.ScopeMember, name string) error {
		return graphRequest.AuthorizePath(ctx, member.WorktreeID, name, member.PolicyDigest, member.PolicyEpoch)
	}
	maxNodes := minInt64(b.limits.MaxGraphNodes, 100000)
	maxEdges := minInt64(b.limits.MaxGraphEdges, 1000000)
	maxDepth := minInt64(b.limits.MaxGraphDepth, 8)
	selection, err := contextpack.Select(ctx, contextpack.Input{Scope: observation.Vector, Target: target, Snapshot: graph.Snapshot, Packages: graph.Packages, Symbols: graph.Symbols, Relations: graph.Relations, Coverage: graph.Coverage, Scanner: scanner, AuthorizeScope: scopeAuth, AuthorizePath: pathAuth, VerifyFacts: graph.VerifyFacts, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFactBytes: packFactLimit, MaxSourceBytes: maxSource, MaxFacts: 100000, MaxNodes: int(maxNodes), MaxEdges: int(maxEdges), MaxDepth: int(maxDepth), MaxOutputBytes: packFactLimit})
	if err != nil {
		return nil, coreStoreError(err)
	}
	rules := make([]model.SourceRef, 0, len(observation.Binding.Policy.TrustedRulePaths))
	for _, name := range observation.Binding.Policy.TrustedRulePaths {
		file, ok := graph.Snapshot.File(name)
		if !ok {
			continue
		}
		rules = append(rules, model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: name, ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: int64(file.Len())}})
	}
	intent := ""
	if request.Intent != nil {
		intent = *request.Intent
	}
	slice, err := contextpack.BuildTaskSlice(ctx, contextpack.SliceInput{Selection: selection, Policy: observation.Binding.Policy, TaskKind: request.TaskKind, Intent: intent, Constraints: request.Constraints, RuleAnchors: rules, VerifyNoApprovedRecords: func(ctx context.Context, scope model.ScopeVector) (bool, error) {
		return b.Reader.NoApprovedRecords(ctx, principal, scope)
	}, MaxBytes: packFactLimit})
	if err != nil {
		return nil, coreStoreError(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	digest, err := model.RequestDigest("context_pack", principal.ID, "PackRequest", request, 65536, 4)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	order := "pack-v1"
	if request.TaskKind == "explore" {
		order = contextpack.ExplorePackOrder
	}
	binding := contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: principal.Digest, BindingDigest: principal.BindingDigest, RequestDigest: digest, OrderVersion: order, Scope: observation.Vector}
	guard := b.packGuard(principal)
	validation := app.ValidateCapturedInput{Scope: observation.Vector, MaxInputBytes: 65536, MaxManifestBytes: b.Reader.MaxManifestBytes,
		CurrentScope: func(ctx context.Context) (model.ScopeVector, error) {
			current, err := b.Reader.Recheck(ctx, principal, observation.Vector)
			return current.Vector, err
		},
		ResolveCapture: func(ctx context.Context, requested model.ScopeMember) (*snapshot.Snapshot, error) {
			if requested.WorktreeID != member.WorktreeID || requested.ProfileID != member.ProfileID || requested.GenerationID != member.GenerationID || requested.IndexIncarnation != member.IndexIncarnation {
				return nil, ErrScopeChanged
			}
			if _, err := b.Reader.Recheck(ctx, principal, observation.Vector); err != nil {
				return nil, err
			}
			return graph.Snapshot, nil
		},
		AuthorizeSource: func(ctx context.Context, requested model.ScopeMember, ref model.SourceRef) error {
			return authorizePath(ctx, requested.WorktreeID, ref.Path, requested.PolicyDigest, requested.PolicyEpoch)
		},
	}
	if b.verifyWorkspace != nil {
		validation.VerifyWorkspace = func(ctx context.Context, member model.ScopeMember, snap *snapshot.Snapshot) (app.WorkspaceObservation, error) {
			return b.verifyWorkspace(ctx, principal, member, snap)
		}
	}
	captureLive := func(ctx context.Context, requested model.ScopeMember, name string) (snapshot.File, error) {
		if requested.WorktreeID != member.WorktreeID || requested.GenerationID != member.GenerationID {
			return snapshot.File{}, ErrAuthority
		}
		return inventory.CaptureFile(ctx, b.Reader.WorktreeRoot, scanner, name, int(maxFile), func(ctx context.Context, path string) error {
			return authorizePath(ctx, requested.WorktreeID, path, requested.PolicyDigest, requested.PolicyEpoch)
		})
	}
	pack, token, err := app.NewFreshnessPack(ctx, app.FreshnessPackInput{Slice: slice, Codec: b.Cursor, Binding: binding, Guard: guard, RequestJSON: raw, PrincipalID: principal.ID, Validation: validation, CaptureLive: captureLive, ResolvePosition: resolvePosition, RequestLimit: 65536})
	if err != nil {
		return nil, coreStoreError(err)
	}
	extra, err := json.Marshal(struct {
		Packages    []model.PackageFact
		Symbols     []model.SymbolFact
		Relations   []model.RelationFact
		Diagnostics []model.DiagnosticFact
		Coverage    model.Coverage
		Policy      model.PolicyConfig
	}{graph.Packages, graph.Symbols, graph.Relations, graph.Diagnostics, graph.Coverage, observation.Binding.Policy})
	if err != nil || len(extra) > packFactLimit+1024*1024 {
		return nil, app.ErrCoreLimit
	}
	if options.DeliveryMode == "cli" {
		retention, err := b.cliSymbolRetention(principal, graph)
		if err != nil {
			return nil, coreStoreError(err)
		}
		handle, err := retention.Save(ctx, pack, principal.Digest, principal.ID, raw, guard)
		if err != nil || handle == nil || handle.Token() != token {
			if err == nil {
				err = app.ErrValidationAuthority
			}
			return nil, coreStoreError(err)
		}
		page, err := handle.First(ctx, packPageOptions(options))
		if err != nil {
			guarded, _ := app.GuardOversizedError(err, handle.Recheck, nil)
			return nil, coreStoreError(guarded)
		}
		// First has just checked the installed bytes, TTL and full live
		// authority after preparing the private page; no callback runs here.
		app.RememberSymbolPackDelivery(ctx, handle)
		return page.Blocks, nil
	}
	if err := b.packCache.Admit(ctx, pack, token, principal.Digest, guard, int64(len(extra))); err != nil {
		return nil, coreStoreError(err)
	}
	page, err := pack.First(ctx, packPageOptions(options))
	if err != nil {
		guarded, described := app.GuardOversizedError(err, func(ctx context.Context) error {
			return b.recheckPackCache(ctx, pack, token, principal, guard)
		}, func() { b.packCache.Discard(pack) })
		if described {
			return nil, coreStoreError(guarded)
		}
		b.packCache.Discard(pack)
		return nil, coreStoreError(err)
	}
	if err := b.recheckPackCache(ctx, pack, token, principal, guard); err != nil {
		b.packCache.Discard(pack)
		return nil, err
	}
	return page.Blocks, nil
}

func (b *CoreBackend) ExpandPack(ctx context.Context, principal app.CorePrincipal, request model.ExpandRequest, options app.CoreCallOptions) ([][]byte, error) {
	if !b.valid() || b.packCache == nil {
		return nil, app.ErrCoreUnsupported
	}
	if options.DeliveryMode == "cli" {
		return b.expandCLIPack(ctx, principal, request, options)
	}
	guard := b.packGuard(principal)
	retained, err := b.packCache.GetRetained(ctx, request.PackToken, principal.Digest, guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	pack, ok := retained.(coreRetainedPack)
	if !ok {
		return nil, app.ErrCoreBackend
	}
	claims, err := b.Cursor.VerifyPrincipal(ctx, request.PackToken, "pack", principal.Digest, guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	observation, err := b.Reader.Recheck(ctx, principal, claims.Binding.Scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if options.MaxOutputBytes > observation.Binding.Policy.MaxOutputBytes {
		options.MaxOutputBytes = observation.Binding.Policy.MaxOutputBytes
	}
	page, err := pack.Continue(ctx, request.PackToken, request.NextCursor, principal.Digest, guard, packPageOptions(options))
	if err != nil {
		guarded, _ := app.GuardOversizedError(err, func(ctx context.Context) error {
			return b.recheckPackCache(ctx, pack, request.PackToken, principal, guard)
		}, nil)
		return nil, coreStoreError(guarded)
	}
	if err := b.recheckPackCache(ctx, pack, request.PackToken, principal, guard); err != nil {
		return nil, err
	}
	return page.Blocks, nil
}

func (b *CoreBackend) ValidatePack(ctx context.Context, principal app.CorePrincipal, request model.ValidateRequest, options app.CoreCallOptions) ([][]byte, error) {
	if request.ResultToken != nil && request.PackToken == nil {
		return b.validateRename(ctx, principal, *request.ResultToken, options)
	}
	if !b.valid() || b.packCache == nil || request.PackToken == nil {
		return nil, app.ErrCoreUnsupported
	}
	if options.DeliveryMode == "cli" {
		return b.validateCLIPack(ctx, principal, request, options)
	}
	guard := b.packGuard(principal)
	retained, err := b.packCache.GetRetained(ctx, *request.PackToken, principal.Digest, guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	pack, ok := retained.(coreRetainedPack)
	if !ok {
		return nil, app.ErrCoreBackend
	}
	claims, err := b.Cursor.VerifyPrincipal(ctx, *request.PackToken, "pack", principal.Digest, guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	observation, err := b.Reader.Recheck(ctx, principal, claims.Binding.Scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if options.MaxOutputBytes > observation.Binding.Policy.MaxOutputBytes {
		options.MaxOutputBytes = observation.Binding.Policy.MaxOutputBytes
	}
	validation, err := pack.Validate(ctx, *request.PackToken, principal.Digest, guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if err := b.recheckPackCache(ctx, pack, *request.PackToken, principal, guard); err != nil {
		return nil, err
	}
	response := baseResponse(options, observation.Vector)
	response.Freshness, response.Data = validation.Freshness, validation.Data
	return encodeResponse(response)
}

type coreRetainedPack interface {
	app.RetainedPack
	Continue(context.Context, string, string, string, contextpack.CursorGuard, contextpack.PackPageOptions) (contextpack.PackPage, error)
	Validate(context.Context, string, string, contextpack.CursorGuard) (app.CapturedValidation, error)
}

func (b *CoreBackend) recheckPackCache(ctx context.Context, expected app.RetainedPack, token string, principal app.CorePrincipal, guard contextpack.CursorGuard) error {
	current, err := b.packCache.GetRetained(ctx, token, principal.Digest, guard)
	if err != nil {
		return coreStoreError(err)
	}
	if current != expected {
		return app.ErrValidationAuthority
	}
	return nil
}

func packPageOptions(options app.CoreCallOptions) contextpack.PackPageOptions {
	return contextpack.PackPageOptions{RequestID: options.RequestID, MaxOutputBytes: options.MaxOutputBytes, DeliveryMode: options.DeliveryMode, VerifyStructuredHost: options.VerifyStructuredHost}
}

func minInt64(values ...int64) int64 {
	v := values[0]
	for _, x := range values[1:] {
		if x < v {
			v = x
		}
	}
	return v
}
func maxInt64(values ...int64) int64 {
	v := values[0]
	for _, x := range values[1:] {
		if x > v {
			v = x
		}
	}
	return v
}
