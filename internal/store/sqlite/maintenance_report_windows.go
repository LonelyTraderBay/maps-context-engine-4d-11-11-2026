package sqlite

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// ReadProtectedRecoveryReport inspects authorized controller metadata.
// It never opens SQLite, modifies files, or grants authority to apply restore.
func ReadProtectedRecoveryReport(ctx context.Context, name, target, worktree string) (out StoreRecoveryReport, resultErr error) {
	if ctx == nil || !validWindowsPreparationRoot(name) {
		return out, ErrProfileInput
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	scope, relative := "catalog", "control"
	switch {
	case target == "worktree" && model.ValidUUID(worktree):
		scope = worktree
		relative = "control/worktrees/" + worktree
	case target == "catalog" && worktree == "":
	default:
		return out, ErrProfileInput
	}
	root, err := paths.OpenRoot(name)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	profile, acl, err := readWindowsProfilePreparation(ctx, root, name)
	if err != nil {
		return out, err
	}
	control, err := root.OpenDirectory(relative)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	files := windowsMaintenanceFiles{control: control, acl: acl, profile: profile.ProfileID, scope: scope}
	state, present, err := files.read(ctx)
	if err != nil {
		return out, err
	}
	out = StoreRecoveryReport{Target: target, WorktreeID: worktree, ReportOnly: true,
		JournalPresent: present, Phase: "none", CompletedSteps: []string{}}
	if present {
		out.OperationID, out.Phase, out.Outcome = state.OperationID, state.Phase, state.Outcome
		out.CompletedSteps = append(out.CompletedSteps, state.CompletedSteps...)
	}
	return out, nil
}
