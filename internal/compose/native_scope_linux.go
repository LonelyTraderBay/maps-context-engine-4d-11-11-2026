//go:build linux

package compose

import (
	"context"
	"errors"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

// Shared startup ownership for bound query and foreground index/watch. None of
// these resources or scope selectors are accepted from tool arguments as grants.
type nativeScope struct {
	installation   string
	state          *sqliteowner.ProtectedState
	config         *app.RuntimeConfigService
	effective      model.RuntimeConfig
	catalog        *catalog.Store
	binding        catalog.Binding
	target, common *paths.Root
	linked         string
	stores         *sqliteowner.ProtectedWorktreeOwner
	rename         *nativeRenameOwner
}

func openNativeScope(ctx context.Context, installation, selected, worktree, profile string, cpus int) (_ *nativeScope, resultErr error) {
	selection := model.ScopeRequest{WorktreeID: &worktree, ProfileID: &profile}
	if mode, err := selection.SelectionMode(1); err != nil || mode != "single" {
		return nil, app.ErrCoreAuthentication
	}
	n := &nativeScope{installation: installation}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, n.Close())
		}
	}()
	var err error
	n.state, err = sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return nil, err
	}
	if err := n.state.CheckWorktreeMaintenance(ctx, worktree); err != nil {
		return nil, err
	}
	n.config, err = protectedConfig(ctx, n.state, selected, cpus)
	if err != nil {
		return nil, err
	}
	n.effective, _, err = n.config.StartupConfig()
	if err != nil {
		return nil, err
	}
	n.catalog, err = n.state.OpenCatalog(ctx)
	if err != nil {
		return nil, err
	}
	n.binding, err = n.catalog.ReadBinding(ctx, worktree, profile, func(ctx context.Context, _, _ string) error { return n.state.Check(ctx) })
	if err != nil || !n.state.TargetApproved(n.binding.CanonicalRoot) {
		return nil, app.ErrCoreAuthentication
	}
	n.target, err = paths.OpenRoot(n.binding.CanonicalRoot)
	if err != nil {
		return nil, err
	}
	n.common, n.linked, err = runtime.OpenApprovedGitMetadata(n.target, n.state.OpenTargetGitMetadata)
	if err != nil || n.binding.VerifyRoots(n.target, n.common, n.linked) != nil {
		return nil, app.ErrCoreAuthentication
	}
	// The worktree owner independently checks this same installation manifest
	// on every operation. Its caller check supplies the approved target handles;
	// catalog/profile/policy revisions are checked by the scope coordinator.
	n.stores, err = sqliteowner.OpenProtectedWorktreeWithCacheLimit(ctx, installation, worktree, n.effective.Core.MaxWorktreeCacheBytes, func(ctx context.Context) error {
		if !n.state.TargetApproved(n.binding.CanonicalRoot) {
			return ErrAuthority
		}
		return errors.Join(ctx.Err(), n.target.Verify(), n.common.Verify())
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (n *nativeScope) recheckBinding(ctx context.Context) error {
	if err := n.state.Check(ctx); err != nil {
		return err
	}
	current, err := n.catalog.ReadBinding(ctx, n.binding.WorktreeID, n.binding.ProfileID, func(ctx context.Context, _, _ string) error { return n.state.Check(ctx) })
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, n.binding) {
		return ErrScopeChanged
	}
	return errors.Join(n.stores.Check(ctx), n.binding.VerifyRoots(n.target, n.common, n.linked))
}

// This is a path/OS lifetime check, not a cached catalog authorization. The
// inventory coordinator checks the catalog at both observation boundaries;
// each captured body and SQL publication separately rechecks it as well.
func (n *nativeScope) boundary(ctx context.Context) error {
	return errors.Join(n.state.Check(ctx), n.stores.Check(ctx), n.target.Verify(), n.common.Verify())
}

func (n *nativeScope) Close() error {
	if n.rename != nil {
		if err := n.rename.Close(); err != nil {
			return err
		}
		n.rename = nil
	}
	if n.stores != nil {
		if err := n.stores.Close(); err != nil {
			return err
		}
		n.stores = nil
	}
	var err error
	if n.target != nil {
		err = errors.Join(err, n.target.Close())
		n.target = nil
	}
	if n.common != nil {
		err = errors.Join(err, n.common.Close())
		n.common = nil
	}
	if n.state != nil {
		err = errors.Join(err, n.state.Close())
		n.state = nil
	}
	return err
}
