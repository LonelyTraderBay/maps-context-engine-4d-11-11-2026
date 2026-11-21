//go:build linux

package compose

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

type nativePreviewAuthority struct {
	core     *CoreBackend
	scope    *nativeScope
	resolver string
	maxFile  int
	scanner  *policy.Scanner
}

func newNativePreviewAuthority(owner *nativeCoreParts) (*nativePreviewAuthority, error) {
	n := owner.scope
	maxFile := int(minInt64(n.effective.Core.MaxFileBytes, n.binding.Policy.MaxFileBytes, 2<<20))
	scanner, err := policy.NewScanner(maxFile)
	if err != nil {
		return nil, err
	}
	resolver, err := model.ResolverDigest(n.uiResolver(), 8192)
	if err != nil {
		return nil, err
	}
	return &nativePreviewAuthority{core: owner.backend, scope: n, resolver: resolver, maxFile: maxFile, scanner: scanner}, nil
}

func (a *nativePreviewAuthority) Permission(ctx context.Context, id string, now time.Time) (model.PreviewPermissionSummary, error) {
	return a.scope.catalog.ReadPreviewPermission(ctx, id, now, a.scope.boundary)
}

func (a *nativePreviewAuthority) FrameOrigins(ctx context.Context, now time.Time) ([]string, error) {
	heads, more, err := a.scope.catalog.ListPreviewPermissions(ctx, "", 100, now, a.scope.boundary)
	if err != nil || more {
		return nil, err
	}
	var origins []string
	for _, head := range heads {
		p := head.Profile
		if head.EffectiveStatus == "approved" && p.AllowAttach && slices.Contains(p.SourceWorktreeIDs, a.scope.binding.WorktreeID) &&
			!slices.Contains(origins, p.ApprovedOrigin) {
			origins = append(origins, p.ApprovedOrigin)
		}
	}
	return origins, nil
}

func (a *nativePreviewAuthority) Resolve(ctx context.Context, requested *model.ScopeRequest) (model.ScopeVector, error) {
	p, err := a.core.Authority.Authenticate(ctx)
	if err != nil {
		return model.ScopeVector{}, coreStoreError(err)
	}
	if err := a.core.Authority.AuthorizeRequest(ctx, p, "mce4d_workspace_status", model.StatusRequest{Scope: requested}); err != nil {
		return model.ScopeVector{}, coreStoreError(err)
	}
	if requested != nil && requested.PinnedVector != nil {
		observed, err := a.core.Reader.Recheck(ctx, p, *requested.PinnedVector)
		return observed.Vector, coreStoreError(err)
	}
	generation := ""
	if requested != nil && requested.GenerationID != nil {
		generation = *requested.GenerationID
	}
	observed, err := a.core.Reader.Resolve(ctx, p, generation)
	return observed.Vector, coreStoreError(err)
}

func (a *nativePreviewAuthority) Recheck(ctx context.Context, vector model.ScopeVector) error {
	p, err := a.core.Authority.Authenticate(ctx)
	if err != nil {
		return coreStoreError(err)
	}
	_, err = a.core.Reader.Recheck(ctx, p, vector)
	return coreStoreError(err)
}

func (a *nativePreviewAuthority) Binding(ctx context.Context, vector model.ScopeVector, build string) (explorer.PreviewBinding, error) {
	if len(vector.Members) != 1 {
		return explorer.PreviewBinding{}, app.ErrCoreInput
	}
	member := vector.Members[0]
	n := a.scope
	value, sources, err := a.core.Reader.Index.ReadUIBinding(ctx, a.graphRequest(member), build, a.resolver)
	if errors.Is(err, index.ErrSnapshotUnavailable) {
		return explorer.PreviewBinding{}, explorer.ErrPreviewMapMissing
	}
	if err != nil {
		return explorer.PreviewBinding{}, coreStoreError(err)
	}
	bytesLeft := minInt64(n.effective.Core.MaxSourceBytesPerSnapshot, n.binding.Policy.MaxSourceBytes)
	for _, source := range sources {
		file, err := inventory.CaptureFile(ctx, n.target, a.scanner, source.Path, int(minInt64(bytesLeft, int64(a.maxFile))),
			func(ctx context.Context, path string) error {
				if policy.CheckConfiguredPath(path, n.binding.Policy.DeniedGlobs) != nil {
					return ErrAuthority
				}
				return n.boundary(ctx)
			})
		if err != nil || file.ContentHash() != source.ContentHash {
			if ctx.Err() != nil {
				return explorer.PreviewBinding{}, ctx.Err()
			}
			return explorer.PreviewBinding{}, explorer.ErrPreviewMapMissing
		}
		bytesLeft -= int64(file.Len())
	}
	if err := a.Recheck(ctx, vector); err != nil {
		return explorer.PreviewBinding{}, err
	}
	return explorer.PreviewBinding{ID: value.BindingID, ResolverDigest: value.ResolverDigest}, nil
}

func (a *nativePreviewAuthority) graphRequest(member model.ScopeMember) index.PublishedGraphRequest {
	n := a.scope
	authorize := func(ctx context.Context, worktree, profile, digest string, epoch int64) error {
		if worktree != n.binding.WorktreeID || profile != n.binding.ProfileID || digest != n.binding.PolicyDigest || epoch != n.binding.PolicyEpoch {
			return ErrAuthority
		}
		return n.recheckBinding(ctx)
	}
	return index.PublishedGraphRequest{Member: member, AuthorizeGeneration: authorize, MaxManifestBytes: a.core.Reader.MaxManifestBytes}
}

func (a *nativePreviewAuthority) ResolveAnchor(ctx context.Context, scope model.ScopeVector, binding explorer.PreviewBinding, anchor model.DOMAnchorRef) (*model.MappingMatch, error) {
	if len(scope.Members) != 1 || binding.ResolverDigest != a.resolver {
		return nil, explorer.ErrPreviewChanged
	}
	member := scope.Members[0]
	match, err := a.core.Reader.Index.ReadUIAnchorMatch(ctx, a.graphRequest(member), index.UIBinding{
		BindingID: binding.ID, BuildMapID: anchor.BuildMapID, SourceGenerationID: member.GenerationID,
		ResolverDigest: binding.ResolverDigest,
	}, anchor.AnchorID)
	if err != nil || match == nil {
		return nil, coreStoreError(err)
	}
	if err := a.checkMappingScope(scope, *match); err != nil {
		return nil, err
	}
	return match, nil
}

func (a *nativePreviewAuthority) ResolveTarget(ctx context.Context, scope model.ScopeVector, binding explorer.PreviewBinding, build string,
	target model.GraphRef, after string, limit int) ([]model.MappingMatch, int64, error) {
	if len(scope.Members) != 1 || binding.ResolverDigest != a.resolver {
		return nil, 0, explorer.ErrPreviewChanged
	}
	member := scope.Members[0]
	if _, err := model.MatchPinnedMember(scope, target, nil); err != nil {
		return nil, 0, ErrScopeChanged
	}
	if file, ok := target.(*model.FileRef); ok && policy.CheckConfiguredPath(file.Path, a.scope.binding.Policy.DeniedGlobs) != nil {
		return nil, 0, ErrAuthority
	}
	matches, total, err := a.core.Reader.Index.ReadUITargetMatches(ctx, a.graphRequest(member), index.UIBinding{
		BindingID: binding.ID, BuildMapID: build, SourceGenerationID: member.GenerationID, ResolverDigest: binding.ResolverDigest,
	}, target, after, limit)
	if err != nil {
		return nil, 0, coreStoreError(err)
	}
	for _, match := range matches {
		if err := a.checkMappingScope(scope, match); err != nil {
			return nil, 0, err
		}
	}
	return matches, total, nil
}

func (a *nativePreviewAuthority) checkMappingScope(scope model.ScopeVector, match model.MappingMatch) error {
	if _, err := model.MatchPinnedMember(scope, match.TargetRef, nil); err != nil {
		return ErrScopeChanged
	}
	for _, source := range match.SourceRefs {
		if policy.CheckConfiguredPath(source.Path, a.scope.binding.Policy.DeniedGlobs) != nil {
			return ErrAuthority
		}
	}
	return nil
}
