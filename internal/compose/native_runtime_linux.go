//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func protectedConfig(ctx context.Context, state *sqliteowner.ProtectedState, selected string, cpus int) (*app.RuntimeConfigService, error) {
	root, err := state.StateRoot().CanonicalPath()
	if err != nil {
		return nil, err
	}
	if selected != "" && selected != filepath.Join(root, "runtime.json") {
		return nil, app.ErrAdminDenied
	}
	return NewGuardedConfigService(ctx, state.StateRoot(), "runtime.json", state.TargetRoots(), cpus, state.Check)
}

func OpenNativeConfig(ctx context.Context, installation, selected string, cpus int) (*app.RuntimeConfigService, func() error, error) {
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := sqliteowner.OpenProtectedState(bounded, installation)
	if err != nil {
		if errors.Is(err, sqliteowner.ErrProfileDenied) {
			err = errors.Join(app.ErrAdminDenied, err)
		}
		return nil, nil, err
	}
	service, err := protectedConfig(bounded, state, selected, cpus)
	if err != nil {
		return nil, nil, errors.Join(err, state.Close())
	}
	return service, state.Close, nil
}

type NativeCatalogAdmin struct {
	state          *sqliteowner.ProtectedState
	store          *catalog.Store
	mutation       *runtime.Writer
	kind, worktree string
	installation   string
}

func OpenNativeAdmin(ctx context.Context, installation, selected, kind, worktree string) (_ *NativeCatalogAdmin, release func() error, resultErr error) {
	if kind != "profile" && kind != "policy" {
		return nil, nil, app.ErrAdminUnsupported
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := sqliteowner.OpenProtectedState(bounded, installation)
	if err != nil {
		return nil, nil, err
	}
	admin := &NativeCatalogAdmin{state: state, kind: kind, worktree: worktree, installation: installation}
	closeAll := func() error {
		var err error
		if admin.mutation != nil {
			err = admin.mutation.Close()
			if err == nil {
				admin.mutation = nil
			}
		}
		if err != nil {
			return err
		}
		return state.Close()
	}
	release = closeAll
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, closeAll())
		}
	}()
	if selected != "" {
		root, _ := state.StateRoot().CanonicalPath()
		if selected != filepath.Join(root, "runtime.json") {
			return nil, nil, app.ErrAdminDenied
		}
	}
	store, err := state.OpenCatalog(bounded)
	if err != nil {
		return nil, nil, err
	}
	root, err := store.ReadWorktreeRoot(bounded, worktree, state.Check)
	if err != nil {
		return nil, nil, nativeAdminError(err)
	}
	if !state.TargetApproved(root) {
		return nil, nil, app.ErrAdminDenied
	}
	operation, err := nativeOperationID()
	if err != nil {
		return nil, nil, err
	}
	admin.mutation, err = runtime.AcquireCatalogMutation(bounded, state.CatalogLease(), operation, 5*time.Second)
	if err != nil {
		return nil, nil, err
	}
	admin.store, err = store.WithAuthorizedMutation(bounded, admin.mutation, operation, state.Check)
	if err != nil {
		return nil, nil, err
	}
	return admin, release, nil
}

func (a *NativeCatalogAdmin) SetProfile(ctx context.Context, raw []byte) (model.ProfileAdminData, error) {
	request, err := model.DecodeProfileAdminRequestJSON(raw, 32768)
	if err != nil {
		return model.ProfileAdminData{}, err
	}
	if a == nil || a.kind != "profile" || a.worktree != request.WorktreeID {
		return model.ProfileAdminData{}, app.ErrAdminDenied
	}
	if _, goProfile := request.Profile.(*model.GoProfile); !goProfile {
		root, err := a.state.StateRoot().CanonicalPath()
		if err != nil {
			return model.ProfileAdminData{}, err
		}
		provider, err := openNativeAddonProvider(ctx, a.installation, filepath.Join(root, "worktrees"), request.Profile)
		if err != nil {
			return model.ProfileAdminData{}, errors.Join(app.ErrAdminUnsupported, err)
		}
		if err := provider.worker.VerifyInstallation(ctx); err != nil {
			return model.ProfileAdminData{}, errors.Join(app.ErrAdminUnsupported, err)
		}
		data, err := a.store.SetAddonProfile(ctx, raw)
		return data, nativeAdminError(err)
	}
	data, err := a.store.SetProfile(ctx, raw)
	return data, nativeAdminError(err)
}
func (a *NativeCatalogAdmin) SetPolicy(ctx context.Context, raw []byte) (model.PolicyAdminData, error) {
	request, err := model.DecodePolicyAdminRequestJSON(raw, 32768)
	if err != nil {
		return model.PolicyAdminData{}, err
	}
	if a == nil || a.kind != "policy" || a.worktree != request.WorktreeID {
		return model.PolicyAdminData{}, app.ErrAdminDenied
	}
	for _, root := range append(append([]string(nil), request.Policy.AllowedRoots...), request.Policy.DependencyRoots...) {
		if !a.state.TargetApproved(root) {
			return model.PolicyAdminData{}, app.ErrAdminDenied
		}
	}
	data, err := a.store.SetPolicy(ctx, raw)
	return data, nativeAdminError(err)
}

func nativeAdminError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, catalog.ErrCatalogCommitUnknown), errors.Is(err, sqliteowner.ErrNativeOutcomeUnknown):
		return err
	case errors.Is(err, catalog.ErrRevisionConflict):
		return app.ErrAdminConflict
	case errors.Is(err, catalog.ErrWorktreeUnavailable):
		return app.ErrAdminUnavailable
	case errors.Is(err, sqliteowner.ErrProfileDenied):
		return app.ErrAdminDenied
	case errors.Is(err, catalog.ErrUnsupportedProfile):
		return app.ErrAdminUnsupported
	default:
		return err
	}
}

// OpenNativeCore opens only one explicitly selected, registered scope. Merely
// knowing a catalog ID does not bypass the installation's approved root set.
// MCP uses its process-local cache; retained CLI results are an explicit owner.
func OpenNativeCore(ctx context.Context, installation, selected, worktree, profile string, cpus int) (*app.CoreService, func() error, error) {
	return openNativeCore(ctx, installation, selected, worktree, profile, cpus, false)
}

func OpenNativeRetainedCore(ctx context.Context, installation, selected, worktree, profile string, cpus int) (*app.CoreService, func() error, error) {
	return openNativeCore(ctx, installation, selected, worktree, profile, cpus, true)
}

func openNativeCore(ctx context.Context, installation, selected, worktree, profile string, cpus int, retained bool) (*app.CoreService, func() error, error) {
	native, release, err := openNativeCoreParts(ctx, installation, selected, worktree, profile, cpus, retained, true)
	if err != nil {
		return nil, nil, err
	}
	service, err := app.NewCoreService(native.config, native.backend.Authority, native.backend, nil)
	if err != nil {
		return nil, nil, errors.Join(err, release())
	}
	return service, release, nil
}

type nativeCoreParts struct {
	backend   *CoreBackend
	config    *app.RuntimeConfigService
	effective model.RuntimeConfig
	scope     *nativeScope
}

func openNativeCoreParts(ctx context.Context, installation, selected, worktree, profile string, cpus int, retained, rename bool) (_ *nativeCoreParts, release func() error, resultErr error) {
	selection := model.ScopeRequest{WorktreeID: &worktree, ProfileID: &profile}
	if mode, err := selection.SelectionMode(1); err != nil || mode != "single" {
		return nil, nil, app.ErrCoreAuthentication
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	n, err := openNativeScope(bounded, installation, selected, worktree, profile, cpus)
	if err != nil {
		if errors.Is(err, sqliteowner.ErrCacheCapacity) {
			return nil, nil, errors.Join(app.ErrCoreLimit, err)
		}
		return nil, nil, err
	}
	release = n.Close
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, n.Close())
		}
	}()
	state, config, effective, cat := n.state, n.config, n.effective, n.catalog
	target, common, stores, linked := n.target, n.common, n.stores, n.linked
	idx, err := stores.Index.Store()
	if err != nil {
		return nil, nil, err
	}
	records, err := stores.Records.Store()
	if err != nil {
		return nil, nil, err
	}
	principalID := "installation:" + state.InstallationID()
	hash := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	principal := app.CorePrincipal{ID: principalID, Digest: hash("MCE4D-OS-PRINCIPAL1\x00" + principalID), BindingDigest: hash("MCE4D-BINDING1\x00" + principalID + "\x00" + worktree + "\x00" + profile)}
	boundStores, sourceRoot, metadataRoot := stores, target, common
	access := func(ctx context.Context, p app.CorePrincipal, w, prof string) error {
		if p != principal || w != worktree || prof != profile {
			return ErrAuthority
		}
		return errors.Join(boundStores.Check(ctx), sourceRoot.Verify(), metadataRoot.Verify())
	}
	reader := &ScopeReader{Catalog: cat, Index: idx, Records: records, WorktreeRoot: target, CommonRoot: common, LinkedRelative: linked, WorktreeID: worktree, ProfileID: profile, Access: access, MaxHeads: 100000, MaxManifestBytes: 64 << 20}
	boundPath := func(ctx context.Context, p app.CorePrincipal, config model.PolicyConfig, name string) error {
		if p != principal {
			return ErrAuthority
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := policy.CheckConfiguredPath(name, config.DeniedGlobs); err != nil {
			return err
		}
		file, err := sourceRoot.OpenRegular(name)
		if err != nil {
			return err
		}
		return errors.Join(file.Close(), sourceRoot.Verify(), metadataRoot.Verify())
	}
	authority := &BoundCoreAuthority{Reader: reader, AuthenticatePrincipal: func(ctx context.Context) (app.CorePrincipal, error) {
		return principal, access(ctx, principal, worktree, profile)
	},
		CheckMetadataPath:  policy.CheckConfiguredPath,
		AuthorizeBoundPath: boundPath,
		AuthorizePath: func(ctx context.Context, p app.CorePrincipal, policyConfig model.PolicyConfig, name string) error {
			if err := access(ctx, p, worktree, profile); err != nil {
				return err
			}
			return boundPath(ctx, p, policyConfig, name)
		}}
	key, err := state.CursorKey(bounded)
	if err != nil {
		return nil, nil, err
	}
	codec, err := contextpack.NewCursorCodec(key)
	clear(key[:])
	if err != nil {
		return nil, nil, err
	}
	cache, err := app.NewFreshnessPackCache(codec, int(effective.Core.MaxRetainedQueryResults), effective.Core.QueryResultCacheBytes, minInt64(effective.Core.PackTtlSeconds, 900))
	if err != nil {
		return nil, nil, err
	}
	backend, err := NewCoreBackendWithPlan(config, reader, authority, codec, cache, nativeWorkspaceVerifier(n, reader, principal), nativePlanSource(n, reader, principal))
	if err != nil {
		return nil, nil, err
	}
	if rename {
		backend, err = attachNativeRename(bounded, n, backend, principal)
		if err != nil {
			return nil, nil, err
		}
	}
	if retained {
		root, lease, protected, err := boundStores.CacheResources(bounded)
		if err != nil {
			return nil, nil, err
		}
		dir, err := root.OpenDirectory("scratch/pack")
		if err != nil {
			return nil, nil, err
		}
		if err := dir.Close(); err != nil {
			return nil, nil, err
		}
		quota := minInt64(effective.Core.QueryResultCacheBytes, 32<<20)
		count := minInt64(effective.Core.MaxRetainedQueryResults, 32)
		if quota < 1024 || count < 1 {
			return nil, nil, app.ErrCoreLimit
		}
		spool, err := packstore.New(root, protected, lease, worktree, quota, quota, int(count),
			func(ctx context.Context) error { return access(ctx, principal, worktree, profile) },
			func(ctx context.Context, n int64) (packstore.BudgetReservation, error) {
				return boundStores.ReserveCache(ctx, n)
			})
		if err != nil {
			return nil, nil, err
		}
		backend, err = backend.WithCLIPackRetention(spool)
		if err != nil {
			return nil, nil, err
		}
	}
	return &nativeCoreParts{backend: backend, config: config, effective: effective, scope: n}, release, nil
}
