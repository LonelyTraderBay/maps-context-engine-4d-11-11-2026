package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// statusWorkspaceFreshness performs a new bounded observation of the pinned
// snapshot on every call. It neither caches live truth nor grants edit rights.
func (b *CoreBackend) statusWorkspaceFreshness(ctx context.Context, principal app.CorePrincipal, observed ScopeObservation) (model.Freshness, error) {
	if ctx == nil || b == nil || b.verifyWorkspace == nil || b.buildGate == nil || !boundedSingleScope(observed.Vector) {
		return model.Freshness{}, app.ErrFreshnessUnknown
	}
	select {
	case b.buildGate <- struct{}{}:
		defer func() { <-b.buildGate }()
	case <-ctx.Done():
		return model.Freshness{}, ctx.Err()
	}
	member := observed.Vector.Members[0]
	maxFile := minInt64(observed.Binding.Policy.MaxFileBytes, b.limits.MaxFileBytes, packSourceLimit)
	maxSource := minInt64(observed.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot, packSourceLimit)
	if maxFile < 1 || maxSource < 1 {
		return model.Freshness{}, app.ErrFreshnessUnknown
	}
	scanner, err := policy.NewScanner(int(maxInt64(maxFile, int64(b.Reader.MaxManifestBytes), packFactLimit)))
	if err != nil {
		return model.Freshness{}, err
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
		return b.Authority.path(ctx, principal, observed.Binding.Policy, name)
	}
	graph, err := b.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: observed.Vector.VectorDigest, AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000})
	if err != nil {
		return model.Freshness{}, coreStoreError(err)
	}
	if !app.WorkspaceCaptureComparable(graph.Snapshot.Manifest()) {
		return model.Freshness{}, app.ErrFreshnessUnknown
	}
	state, err := b.verifyWorkspace(ctx, principal, member, graph.Snapshot)
	if err != nil {
		if cause := ctx.Err(); cause != nil {
			return model.Freshness{}, cause
		}
		if errors.Is(err, ErrAuthority) || errors.Is(err, app.ErrValidationAuthority) || errors.Is(err, ErrScopeChanged) {
			return model.Freshness{}, coreStoreError(err)
		}
		return model.Freshness{}, app.ErrFreshnessUnknown
	}
	if state != app.WorkspaceObservedCurrent && state != app.WorkspaceChanged {
		return model.Freshness{}, app.ErrFreshnessUnknown
	}
	for _, file := range graph.Snapshot.Manifest().Files {
		if err := authorizePath(ctx, member.WorktreeID, file.Path, member.PolicyDigest, member.PolicyEpoch); err != nil {
			return model.Freshness{}, coreStoreError(err)
		}
	}
	if _, err := b.Reader.Recheck(ctx, principal, observed.Vector); err != nil {
		return model.Freshness{}, coreStoreError(err)
	}
	if err := ctx.Err(); err != nil {
		return model.Freshness{}, err
	}
	checked := time.Now().UTC().Format(time.RFC3339Nano)
	freshness := model.Freshness{Status: string(state), Scope: "workspace", CheckedAt: &checked, BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}
	if state == app.WorkspaceObservedCurrent {
		zero := int64(0)
		freshness.ChangedCount = &zero
	}
	return freshness, nil
}
