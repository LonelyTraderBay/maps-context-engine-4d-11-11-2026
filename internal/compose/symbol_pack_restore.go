package compose

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// SymbolRestoreFactory binds a retention owner to this principal and the real
// stores already supplied to the backend. It reopens only the generation in a
// verified token; it does not enable a CLI route or open any state itself.
func (b *CoreBackend) SymbolRestoreFactory(principal app.CorePrincipal) (app.SymbolRestoreFactory, error) {
	return b.symbolRestoreFactory(principal, nil, nil)
}

func (b *CoreBackend) symbolRestoreFactory(principal app.CorePrincipal, captured *index.PublishedGraph, captureGraph func(*index.PublishedGraph)) (app.SymbolRestoreFactory, error) {
	if !b.valid() || principal.ID == "" || principal.Digest == "" || principal.BindingDigest == "" {
		return nil, app.ErrCoreInput
	}
	return func(ctx context.Context, binding contextpack.CursorBinding) (app.FreshnessPackRestoreInput, error) {
		var empty app.FreshnessPackRestoreInput
		if ctx == nil || binding.Purpose != "pack" || binding.ResultID == "" {
			return empty, app.ErrCoreInput
		}
		guard := b.packGuard(principal)
		if err := guard(ctx, binding); err != nil {
			return empty, err
		}
		observation, err := b.Reader.Recheck(ctx, principal, binding.Scope)
		if err != nil {
			return empty, coreStoreError(err)
		}
		// Keep the closure's pinned scope separate from the returned value.
		rawScope, err := json.Marshal(observation.Vector)
		if err != nil || len(rawScope) > 32768 {
			return empty, app.ErrCoreLimit
		}
		pinned, err := model.DecodeScopeVectorJSON(rawScope, 32768)
		if err != nil || pinned.Kind != "single" || len(pinned.Members) != 1 || len(pinned.UnavailableMembers) != 0 {
			return empty, ErrScopeChanged
		}
		outputScope, err := model.DecodeScopeVectorJSON(rawScope, 32768)
		if err != nil {
			return empty, ErrScopeChanged
		}
		member := pinned.Members[0]
		maxFile := minInt64(observation.Binding.Policy.MaxFileBytes, b.limits.MaxFileBytes, packSourceLimit)
		maxSource := minInt64(observation.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot, packSourceLimit)
		if maxFile < 1 || maxSource < 1 {
			return empty, app.ErrCoreLimit
		}
		scanner, err := policy.NewScanner(int(maxInt64(maxFile, int64(b.Reader.MaxManifestBytes), packFactLimit)))
		if err != nil {
			return empty, err
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
		graphRequest := index.PublishedGraphRequest{Member: member, VectorDigest: pinned.VectorDigest, AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000}
		b.setPackGraphAuthority(&graphRequest, principal, observation.Binding.Policy)
		graph := captured
		if graph == nil {
			graph, err = b.Reader.Index.ReadPublishedGraph(ctx, graphRequest)
			if err != nil {
				return empty, coreStoreError(err)
			}
		} else {
			// Save may restore its new archive using the catalog read earlier
			// in this same call. Recheck its digest, lease and all captured paths.
			if err := graph.VerifyCatalogPaths(ctx, member); err != nil {
				return empty, coreStoreError(err)
			}
		}
		scopeAuth := func(ctx context.Context, scope model.ScopeVector, requested model.ScopeMember) error {
			if !reflect.DeepEqual(requested, member) || model.ComparePinnedScope(scope, pinned, 32768) != nil {
				return ErrScopeChanged
			}
			_, err := b.Reader.Recheck(ctx, principal, pinned)
			return err
		}
		pathAuth := func(ctx context.Context, requested model.ScopeMember, name string) error {
			if !reflect.DeepEqual(requested, member) {
				return ErrScopeChanged
			}
			return authorizePath(ctx, requested.WorktreeID, name, requested.PolicyDigest, requested.PolicyEpoch)
		}
		packPathAuth := func(ctx context.Context, requested model.ScopeMember, name string) error {
			if !reflect.DeepEqual(requested, member) {
				return ErrScopeChanged
			}
			return graphRequest.AuthorizePath(ctx, requested.WorktreeID, name, requested.PolicyDigest, requested.PolicyEpoch)
		}
		validation := app.ValidateCapturedInput{Scope: outputScope, MaxInputBytes: 65536, MaxManifestBytes: b.Reader.MaxManifestBytes,
			CurrentScope: func(ctx context.Context) (model.ScopeVector, error) {
				current, err := b.Reader.Recheck(ctx, principal, pinned)
				return current.Vector, err
			},
			ResolveCapture: func(ctx context.Context, requested model.ScopeMember) (*snapshot.Snapshot, error) {
				if err := scopeAuth(ctx, pinned, requested); err != nil {
					return nil, err
				}
				return graph.Snapshot, nil
			},
			AuthorizeSource: func(ctx context.Context, requested model.ScopeMember, ref model.SourceRef) error {
				if ref.WorktreeID != member.WorktreeID || ref.ProfileID != member.ProfileID || ref.GenerationID != member.GenerationID {
					return ErrScopeChanged
				}
				return pathAuth(ctx, requested, ref.Path)
			},
		}
		if b.verifyWorkspace != nil {
			validation.VerifyWorkspace = func(ctx context.Context, requested model.ScopeMember, snap *snapshot.Snapshot) (app.WorkspaceObservation, error) {
				if snap != graph.Snapshot || !reflect.DeepEqual(requested, member) {
					return app.WorkspaceUnknown, ErrScopeChanged
				}
				return b.verifyWorkspace(ctx, principal, requested, snap)
			}
		}
		capture := func(ctx context.Context, requested model.ScopeMember, name string) (snapshot.File, error) {
			if !reflect.DeepEqual(requested, member) {
				return snapshot.File{}, ErrScopeChanged
			}
			return inventory.CaptureFile(ctx, b.Reader.WorktreeRoot, scanner, name, int(maxFile), func(ctx context.Context, path string) error { return pathAuth(ctx, requested, path) })
		}
		resolvePosition := func(ctx context.Context, position model.PositionSelector, scope model.ScopeVector, service *string) (model.SymbolRef, error) {
			if service != nil || model.ComparePinnedScope(scope, pinned, 32768) != nil {
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
		if err := guard(ctx, binding); err != nil {
			return empty, err
		}
		if captureGraph != nil {
			captureGraph(graph)
		}
		return app.FreshnessPackRestoreInput{
			Pack: contextpack.PackRestoreInput{Snapshot: graph.Snapshot, Scanner: scanner, AuthorizeScope: scopeAuth, AuthorizePath: packPathAuth, VerifyFacts: graph.VerifyFacts,
				VerifyNoApprovedRecords: func(ctx context.Context, scope model.ScopeVector) (bool, error) {
					if model.ComparePinnedScope(scope, pinned, 32768) != nil {
						return false, ErrScopeChanged
					}
					return b.Reader.NoApprovedRecords(ctx, principal, pinned)
				}},
			PrincipalID: principal.ID, Validation: validation, CaptureLive: capture, ResolvePosition: resolvePosition, RequestLimit: 65536,
		}, nil
	}, nil
}
