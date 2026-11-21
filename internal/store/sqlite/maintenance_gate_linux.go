//go:build linux

package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// ReadProtectedRecoveryReport reads protected metadata only. It takes no DB
// lease, opens no SQLite handle, starts no worker and creates no state. Its
// phase is an observation, not verification of files or authority to recover.
func ReadProtectedRecoveryReport(ctx context.Context, root, target, worktree string) (out StoreRecoveryReport, resultErr error) {
	if ctx == nil || !validProfilePath(root) {
		return out, ErrProfileInput
	}
	scope := "catalog"
	relative := "control"
	if target == "worktree" {
		if _, err := profileUUIDBytes(worktree); err != nil {
			return out, err
		}
		scope = worktree
		relative = "control/worktrees/" + worktree
	} else if target != "catalog" || worktree != "" {
		return out, ErrProfileInput
	}
	r, err := paths.OpenRoot(root)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, r.Close()) }()
	m, _, err := readProfileManifest(ctx, r)
	if err != nil {
		return out, err
	}
	uid := uint32(unix.Geteuid())
	if uid == 0 {
		if administratorProfileProcess(ctx) != nil {
			return out, ErrProfileDenied
		}
	} else if uid != m.EngineUID || uint32(unix.Getegid()) != m.EngineGID || HardenProtectedProfileWorker() != nil || unprivilegedStateProcess(ctx) != nil {
		return out, ErrProfileDenied
	}
	control, err := r.OpenDirectory(relative)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	journal, present, err := readMaintenanceJournal(ctx, control, m.ProfileID, scope)
	if err == nil && !present && scope == "catalog" {
		// A development profile may have used the former head directory.
		legacy, openErr := control.OpenDirectory("catalog")
		if openErr != nil {
			return out, openErr
		}
		journal, present, err = readMaintenanceJournal(ctx, legacy, m.ProfileID, scope)
		err = errors.Join(err, legacy.Close())
	}
	if err != nil {
		return out, err
	}
	out = StoreRecoveryReport{Target: target, WorktreeID: worktree, ReportOnly: true, JournalPresent: present, Phase: "none", CompletedSteps: []string{}, ActualStateVerified: false}
	if present {
		out.OperationID = journal.OperationID
		out.Phase = journal.Phase
		out.Outcome = journal.Outcome
		out.CompletedSteps = append(out.CompletedSteps, journal.CompletedSteps...)
	}
	return out, nil
}

// Caller holds the selected exclusive lifecycle/writer through its owned
// worker. A new journal never overwrites one; advancing checks the operation.
// Failed temporary files are retained, never inferred disposable from a name.
func writeMaintenanceJournal(ctx context.Context, control *paths.Root, journal MaintenanceJournal, advance bool) error {
	if administratorProfileProcess(ctx) != nil || rootManagedAncestors(ctx, control, true) != nil {
		return ErrProfileDenied
	}
	scope := "catalog"
	if journal.Target == "worktree" {
		scope = journal.WorktreeID
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if _, err := decodeMaintenanceJournal(raw, journal.ProfileInstallationID, scope); err != nil {
		return err
	}
	flags := uint(unix.RENAME_NOREPLACE)
	if advance {
		old, present, err := readMaintenanceJournal(ctx, control, journal.ProfileInstallationID, scope)
		if err != nil || !present || !validMaintenanceAdvance(old, journal) {
			return errors.Join(ErrStoreRecoveryRequired, err)
		}
		flags = 0
	} else {
		if journal.Phase != "PREPARED" && (journal.Phase != "NEEDS_RECOVERY" || len(journal.Files) != 0 || len(journal.CompletedSteps) != 0) {
			return ErrStoreRecoveryRequired
		}
		old, present, err := readMaintenanceJournal(ctx, control, journal.ProfileInstallationID, scope)
		if err != nil {
			return err
		}
		if present {
			if old.Phase != "VERIFIED" || old.OperationID == journal.OperationID {
				return ErrStoreRecoveryRequired
			}
			// Keep the previous completed record at the temporary path.
			flags = unix.RENAME_EXCHANGE
		}
	}
	id, err := profileUUID()
	if err != nil {
		return err
	}
	name := "maintenance.pending." + id + ".json"
	if err := profileFile(control, name, raw, 0, 0, 0444, false); err != nil {
		return err
	}
	dir, err := control.DirectoryHandle()
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := unix.Renameat2(int(dir.Fd()), name, int(dir.Fd()), "maintenance.json", flags); err != nil {
		return err
	}
	return errors.Join(syncWALDirectory(control), control.Verify())
}

func readMaintenanceJournal(ctx context.Context, control *paths.Root, profile, scope string) (out MaintenanceJournal, present bool, resultErr error) {
	if ctx == nil || ctx.Err() != nil || control == nil {
		return out, false, ErrStoreRecoveryRequired
	}
	if rootManagedAncestors(ctx, control, true) != nil {
		return out, false, ErrStoreRecoveryRequired
	}
	f, err := control.OpenRegular("maintenance.json")
	if errors.Is(err, os.ErrNotExist) {
		return out, false, nil
	}
	if err != nil {
		return out, false, errors.Join(ErrStoreRecoveryRequired, err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, f.Close())
		if resultErr != nil {
			out = MaintenanceJournal{}
		}
	}()
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 || st.Nlink != 1 || st.Size < 1 || st.Size > maintenanceJournalMaxBytes {
		return out, true, ErrStoreRecoveryRequired
	}
	raw, err := io.ReadAll(io.LimitReader(f, maintenanceJournalMaxBytes+1))
	if err != nil {
		return out, true, errors.Join(ErrStoreRecoveryRequired, err)
	}
	out, err = decodeMaintenanceJournal(raw, profile, scope)
	if err != nil {
		return out, true, err
	}
	if err := errors.Join(ctx.Err(), control.Verify(), paths.VerifyRegularFile(f)); err != nil {
		return MaintenanceJournal{}, true, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return out, true, nil
}

func checkMaintenanceGate(ctx context.Context, control *paths.Root, profile, scope, operation string) error {
	journal, present, err := readMaintenanceJournal(ctx, control, profile, scope)
	if err != nil {
		return err
	}
	if present && journal.Phase != "VERIFIED" && (operation == "" || operation != journal.OperationID) {
		return ErrStoreRecoveryRequired
	}
	return nil
}
