package sqlite

import (
	"context"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// PrepareRenameStorage provisions the fixed owner-only result path on a
// worktree authenticated by this session. Returned resources are borrowed
// until the pools close. Existing paths must pass the controller ACL check.
func (p *WindowsScopedPools) PrepareRenameStorage(ctx context.Context) (_ *paths.Root, _ *runtime.Lifecycle, resultErr error) {
	if p == nil || p.worktreeLease == nil {
		return nil, nil, ErrProfileDenied
	}
	if err := p.Check(ctx); err != nil {
		return nil, nil, err
	}
	slot, lease := p.worktree, p.worktreeLease
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	parent, err := openWindowsRenameDirectory(ctx, slot, "scratch")
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.control.Close()) }()
	rename, err := openWindowsRenameDirectory(ctx, parent, "rename")
	if err != nil {
		return nil, nil, err
	}
	if err := rename.control.Close(); err != nil {
		return nil, nil, err
	}
	if err := errors.Join(p.Check(ctx), lease.CheckRoot(ctx, slot.control, slot.scope, ports.SharedLease)); err != nil {
		return nil, nil, err
	}
	return slot.control, lease, nil
}

func openWindowsRenameDirectory(ctx context.Context, parent windowsMaintenanceFiles, name string) (windowsMaintenanceFiles, error) {
	child, err := parent.openWindowsPoolDirectory(name)
	if errors.Is(err, os.ErrNotExist) {
		if err := createWindowsProfileDirectory(ctx, parent.control, parent.acl, name); err != nil && !errors.Is(err, ErrProfileExists) {
			return windowsMaintenanceFiles{}, err
		}
		child, err = parent.openWindowsPoolDirectory(name)
	}
	if err != nil {
		return windowsMaintenanceFiles{}, err
	}
	dir, err := child.directory(ctx)
	if err == nil {
		err = dir.Close()
	}
	if err != nil {
		return windowsMaintenanceFiles{}, errors.Join(err, child.control.Close())
	}
	return child, nil
}
