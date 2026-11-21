//go:build linux

package compose

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func RunNativeBackup(ctx context.Context, installation, selected, target, worktree, output string, cpus int) (summary sqliteowner.ProtectedBackupSummary, resultErr error) {
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return summary, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, state.Close())
		if resultErr != nil {
			summary = sqliteowner.ProtectedBackupSummary{}
		}
	}()
	base, err := state.StateRoot().CanonicalPath()
	if err != nil {
		return summary, err
	}
	if target == "worktree" {
		if !model.ValidUUID(worktree) {
			return summary, app.ErrCoreInput
		}
		if err := state.CheckWorktreeMaintenance(ctx, worktree); err != nil {
			return summary, err
		}
		base = filepath.Join(base, "worktrees", worktree)
	} else if target != "catalog" || worktree != "" {
		return summary, app.ErrCoreInput
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output || filepath.Dir(output) != filepath.Join(base, "backups") {
		return summary, app.ErrCoreInput
	}
	config, err := protectedConfig(ctx, state, selected, cpus)
	if err != nil {
		return summary, err
	}
	effective, _, err := config.StartupConfig()
	if err != nil {
		return summary, err
	}
	wait, err := nativeMilliseconds(effective.Core.MaintenanceWaitMs)
	if err != nil || wait > time.Hour {
		return summary, app.ErrCoreUnsupported
	}
	options := sqliteowner.ProtectedBackupOptions{Target: target, WorktreeID: worktree, Output: output, Wait: wait, MaxDatabaseBytes: 4 << 30}
	var revisions func(context.Context) (catalog.BackupRevisions, error)
	if target == "worktree" {
		store, err := state.OpenCatalog(ctx)
		if err != nil {
			return summary, err
		}
		root, err := store.ReadWorktreeRoot(ctx, worktree, state.Check)
		if err != nil || !state.TargetApproved(root) {
			return summary, ErrAuthority
		}
		revisions = func(ctx context.Context) (catalog.BackupRevisions, error) {
			return store.ReadBackupRevisions(ctx, worktree, state.Check)
		}
	}
	return sqliteowner.CreateProtectedBackup(ctx, installation, options, state.Check, revisions)
}
