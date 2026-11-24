//go:build linux

package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func checkRepoConfig(state *sqliteowner.ProtectedState, selected string) error {
	if selected == "" {
		return nil
	}
	root, err := state.StateRoot().CanonicalPath()
	if err != nil || selected != filepath.Join(root, "runtime.json") {
		return app.ErrAdminDenied
	}
	return nil
}

func listNativeWorktrees(ctx context.Context, state *sqliteowner.ProtectedState, store *catalog.Store, worktree string) ([]model.LocalWorktree, error) {
	var roots []string
	for _, root := range state.TargetRoots() {
		name, err := root.CanonicalPath()
		if err != nil {
			return nil, err
		}
		roots = append(roots, name)
	}
	return store.ListLocalWorktrees(ctx, roots, worktree, state.Check)
}

func RunNativeRepoList(ctx context.Context, installation, selected, worktree string) (_ []model.LocalWorktree, resultErr error) {
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return nil, nativeAdminError(err)
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	if err := checkRepoConfig(state, selected); err != nil {
		return nil, err
	}
	store, err := state.OpenCatalog(ctx)
	if err != nil {
		return nil, nativeAdminError(err)
	}
	out, err := listNativeWorktrees(ctx, state, store, worktree)
	if errors.Is(err, catalog.ErrWorktreeListLimit) {
		return nil, app.ErrCoreLimit
	}
	return out, nativeAdminError(err)
}

// RunNativeRepoUpdate keeps the catalog writer and an exclusive worktree
// lifecycle lease across the administrative revision change.
func RunNativeRepoUpdate(ctx context.Context, installation, selected, worktree, newRoot string, purge bool, cpus int) (revision int64, resultErr error) {
	if purge && newRoot != "" {
		return 0, app.ErrAdminDenied
	}
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return 0, nativeAdminError(err)
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	if err := checkRepoConfig(state, selected); err != nil {
		return 0, err
	}
	store, err := state.OpenCatalog(ctx)
	if err != nil {
		return 0, nativeAdminError(err)
	}
	operation, err := nativeOperationID()
	if err != nil {
		return 0, err
	}
	writer, err := runtime.AcquireCatalogMutation(ctx, state.CatalogLease(), operation, 5*time.Second)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	entries, err := listNativeWorktrees(ctx, state, store, worktree)
	if err != nil {
		return 0, nativeAdminError(err)
	}
	if len(entries) != 1 || newRoot != "" && !state.TargetApproved(newRoot) {
		return 0, app.ErrAdminDenied
	}
	if err := state.CheckWorktreeMaintenance(ctx, worktree); err != nil {
		return 0, err
	}
	authorizeRoots := func(ctx context.Context) error {
		if err := state.CheckWorktreeMaintenance(ctx, worktree); err != nil {
			return err
		}
		if !state.TargetApproved(entries[0].Root) || newRoot != "" && !state.TargetApproved(newRoot) {
			return app.ErrAdminDenied
		}
		return nil
	}
	slot := "worktrees/" + worktree
	dir, err := state.StateRoot().OpenDirectory(slot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	var lease *runtime.Lifecycle
	if dir != nil {
		defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
		if purge {
			config, err := protectedConfig(ctx, state, selected, cpus)
			if err != nil {
				return 0, err
			}
			effective, _, err := config.StartupConfig()
			if err != nil {
				return 0, err
			}
			bound, err := store.WithAuthorizedMutation(ctx, writer, operation, authorizeRoots)
			if err != nil {
				return 0, nativeAdminError(err)
			}
			err = sqliteowner.PurgeProtectedDerivedIndex(ctx, installation, worktree, effective.Core.MaxWorktreeCacheBytes, authorizeRoots, func(ctx context.Context) error {
				var err error
				revision, err = bound.Remove(ctx, worktree, entries[0].BindingRevision, true)
				return err
			})
			return revision, nativeAdminError(err)
		}
		lease, err = runtime.AcquireLifecycle(ctx, dir, worktree, ports.ExclusiveLease, 5*time.Second)
		if err != nil {
			return 0, err
		}
		defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	}
	authorize := func(ctx context.Context) error {
		if err := authorizeRoots(ctx); err != nil {
			return err
		}
		if lease != nil {
			return lease.Check(ctx, worktree, ports.ExclusiveLease)
		}
		appeared, err := state.StateRoot().OpenDirectory(slot)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return errors.Join(runtime.ErrStoreMaintenance, appeared.Close())
	}
	bound, err := store.WithAuthorizedMutation(ctx, writer, operation, authorize)
	if err != nil {
		return 0, nativeAdminError(err)
	}
	if newRoot == "" {
		revision, err = bound.Remove(ctx, worktree, entries[0].BindingRevision, true)
		return revision, nativeAdminError(err)
	}
	root, err := paths.OpenRoot(newRoot)
	if err != nil {
		return 0, errors.Join(app.ErrAdminDenied, err)
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	common, linked, err := runtime.OpenApprovedGitMetadata(root, state.OpenTargetGitMetadata)
	if err != nil {
		return 0, errors.Join(app.ErrAdminDenied, err)
	}
	defer func() { resultErr = errors.Join(resultErr, common.Close()) }()
	revision, err = bound.Rebind(ctx, worktree, entries[0].BindingRevision, true, root, common, linked)
	if errors.Is(err, catalog.ErrRootNotAllowed) || errors.Is(err, catalog.ErrRootRebindRequired) || errors.Is(err, catalog.ErrRegistrationExists) {
		return 0, errors.Join(app.ErrAdminDenied, err)
	}
	return revision, nativeAdminError(err)
}
