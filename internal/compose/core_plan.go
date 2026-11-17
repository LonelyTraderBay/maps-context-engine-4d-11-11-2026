package compose

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// contextPlan requires an owner-provided complete declaration set and checks
// that approved records are absent. Until record bodies are wired, it cannot
// safely issue a complete plan when any approved record exists.
func (b *CoreBackend) contextPlan(ctx context.Context, principal app.CorePrincipal, request model.PackRequest, options app.CoreCallOptions, observation ScopeObservation, scanner *policy.Scanner, maxFile, maxSource int64, authorizeGeneration func(context.Context, string, string, string, int64) error, authorizePath func(context.Context, string, string, string, int64) error) ([][]byte, error) {
	member := observation.Vector.Members[0]
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > 65536 {
		return nil, app.ErrCoreInput
	}
	requestCopy, err := model.DecodePackRequestJSON(raw, 65536)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	scopeRaw, err := json.Marshal(observation.Vector)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	scopeCopy, err := model.DecodeScopeVectorJSON(scopeRaw, 65536)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	declarations, err := b.planSource(ctx, principal, scopeCopy, requestCopy)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if len(declarations.Items) > 512 || declarations.Recheck == nil || declarations.AuthorizeDocument == nil || declarations.RetainedBytes < 0 || declarations.RetainedBytes > 32*1024*1024 || declarations.RevisionDigest != "" && !declarationRevisionValid(declarations.RevisionDigest) {
		return nil, app.ErrCoreInput
	}
	recheckDeclarations := declarations.Recheck
	authorizeDocument := declarations.AuthorizeDocument
	providerBytes := declarations.RetainedBytes
	revisionDigest := declarations.RevisionDigest
	items := append(declarations.Items[:0:0], declarations.Items...)
	declarations = PlanDeclarations{}
	if err := recheckDeclarations(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, app.ErrValidationAuthority
	}
	empty, err := b.Reader.NoApprovedRecords(ctx, principal, observation.Vector)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if !empty {
		return nil, contextpack.ErrContinuationUnavailable
	}
	graph, err := b.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: observation.Vector.VectorDigest, AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000})
	if err != nil {
		return nil, coreStoreError(err)
	}
	plan, err := b.buildPlan(ctx, principal, observation.Vector, graph, scanner, maxSource, request, items, authorizeDocument)
	if err != nil {
		return nil, coreStoreError(err)
	}
	// A rule is an obligation in every plan. Until the selected plan can
	// enumerate each entire trusted file, refuse to issue a partial success.
	report, err := plan.Read(ctx)
	if err != nil {
		return nil, coreStoreError(err)
	}
	for _, name := range observation.Binding.Policy.TrustedRulePaths {
		file, present := graph.Snapshot.File(name)
		covered := false
		if present {
			for _, item := range report.Items {
				if item.Kind == "constraint" && item.RequiredForReview && item.Source.Path == name && item.Source.ContentHash == file.ContentHash() && item.Source.Range.StartByte == 0 && item.Source.Range.EndByte == int64(file.Len()) {
					covered = true
					break
				}
			}
		}
		if !covered {
			return nil, contextpack.ErrContinuationUnavailable
		}
	}
	digest, err := model.RequestDigest("context_pack", principal.ID, "PackRequest", request, 65536, 4)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	binding := contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: principal.Digest, BindingDigest: principal.BindingDigest, RequestDigest: digest, OrderVersion: "pack-v1", Scope: observation.Vector}
	baseGuard := b.packGuard(principal)
	guard := b.planAuthorityGuard(principal, observation.Vector, digest, recheckDeclarations)
	pathAuth := func(ctx context.Context, requested model.ScopeMember, name string) error {
		return authorizePath(ctx, requested.WorktreeID, name, requested.PolicyDigest, requested.PolicyEpoch)
	}
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
			return pathAuth(ctx, requested, ref.Path)
		},
	}
	if b.verifyWorkspace != nil {
		validation.VerifyWorkspace = func(ctx context.Context, requested model.ScopeMember, snap *snapshot.Snapshot) (app.WorkspaceObservation, error) {
			return b.verifyWorkspace(ctx, principal, requested, snap)
		}
	}
	captureLive := func(ctx context.Context, requested model.ScopeMember, name string) (snapshot.File, error) {
		if requested.WorktreeID != member.WorktreeID || requested.GenerationID != member.GenerationID {
			return snapshot.File{}, ErrAuthority
		}
		return inventory.CaptureFile(ctx, b.Reader.WorktreeRoot, scanner, name, int(maxFile), func(ctx context.Context, path string) error { return pathAuth(ctx, requested, path) })
	}
	pack, token, err := app.NewFreshnessPlan(ctx, app.FreshnessPlanInput{Plan: plan, Codec: b.Cursor, Binding: binding, Guard: guard, RequestJSON: raw, PrincipalID: principal.ID, Coverage: graph.Coverage, Scanner: scanner, Validation: validation, CaptureLive: captureLive, RequestLimit: 65536, DeclarationRevisionDigest: revisionDigest})
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
	if err != nil || len(extra) > packFactLimit+1024*1024 || providerBytes > 32*1024*1024-int64(len(extra)) {
		return nil, app.ErrCoreLimit
	}
	if options.DeliveryMode == "cli" {
		retention, err := b.cliPlanRetention(principal)
		if err != nil {
			return nil, coreStoreError(err)
		}
		handle, err := retention.Save(ctx, pack, principal.Digest, principal.ID, raw, baseGuard)
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
		// First checked installed bytes and full declaration/source authority
		// after preparing this private page. No callback runs before return.
		return page.Blocks, nil
	}
	if err := b.packCache.AdmitRetained(ctx, pack, token, principal.Digest, baseGuard, int64(len(extra))+providerBytes); err != nil {
		return nil, coreStoreError(err)
	}
	page, err := pack.First(ctx, packPageOptions(options))
	if err != nil {
		guarded, described := app.GuardOversizedError(err, func(ctx context.Context) error {
			return b.recheckPackCache(ctx, pack, token, principal, baseGuard)
		}, func() { b.packCache.DiscardRetained(pack) })
		if described {
			return nil, coreStoreError(guarded)
		}
		b.packCache.DiscardRetained(pack)
		return nil, coreStoreError(err)
	}
	if err := b.recheckPackCache(ctx, pack, token, principal, baseGuard); err != nil {
		b.packCache.DiscardRetained(pack)
		return nil, err
	}
	return page.Blocks, nil
}

// NoApprovedRecords already performs a full current scope observation. Use
// that proof once, between independent declaration-revision checks, rather
// than nesting three identical index/catalog/records observations. Principal
// authentication and the exact pinned binding are checked on every invocation.
func (b *CoreBackend) planAuthorityGuard(principal app.CorePrincipal, scope model.ScopeVector, requestDigest string, registry func(context.Context) error) contextpack.CursorGuard {
	return func(ctx context.Context, binding contextpack.CursorBinding) error {
		if ctx == nil || registry == nil || binding.RequestDigest != requestDigest || model.ComparePinnedScope(binding.Scope, scope, 32768) != nil {
			return ErrScopeChanged
		}
		if _, ok := ctx.Deadline(); !ok {
			return ErrScopeInput
		}
		if err := b.checkPackPrincipal(ctx, principal, binding); err != nil {
			return err
		}
		checkRegistry := func() error {
			if err := registry(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return ErrAuthority
			}
			return nil
		}
		if err := checkRegistry(); err != nil {
			return err
		}
		empty, err := b.Reader.NoApprovedRecords(ctx, principal, scope)
		if err != nil {
			return err
		}
		if !empty {
			return contextpack.ErrContinuationUnavailable
		}
		if err := checkRegistry(); err != nil {
			return err
		}
		return b.checkPackPrincipal(ctx, principal, binding)
	}
}
