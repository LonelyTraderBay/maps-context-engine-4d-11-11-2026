//go:build linux

package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

func restoreFixtureScope(t *testing.T) (target, worktree, scope string) {
	t.Helper()
	switch os.Getenv("MCE4D_RESTORE_FIXTURE_TARGET") {
	case "catalog":
		return "catalog", "", "catalog"
	case "worktree":
		return "worktree", ownerWorktree, ownerWorktree
	default:
		t.Fatal("missing synthetic restore target")
		return "", "", ""
	}
}

func restoreFixtureOutput(root, scope string) string {
	base := profileStatePath(root)
	if scope != "catalog" {
		base = worktreeStatePath(root, scope)
	}
	return filepath.Join(base, "backups", "restore-fixture")
}

func TestProtectedRestoreCreateBackupFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	target, worktree, scope := restoreFixtureScope(t)
	var observe func(context.Context) (catalog.BackupRevisions, error)
	if scope != "catalog" {
		// This storage-only fixture provisions a slot directly. The packaged
		// acceptance scenario uses actual persisted catalog bindings.
		p, err := openProtectedProfile(ctx, root, "READY")
		if err != nil {
			t.Fatal(err)
		}
		revisions := catalog.BackupRevisions{CatalogIncarnation: p.manifest.CatalogIncarnation, Worktrees: []catalog.BackupWorktreeRevision{{WorktreeID: worktree, Status: "registered", BindingRevision: 1, Profiles: []catalog.BackupProfileRevision{}}}}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		observe = func(context.Context) (catalog.BackupRevisions, error) { return revisions, nil }
	}
	options := ProtectedBackupOptions{Target: target, WorktreeID: worktree, Output: restoreFixtureOutput(root, scope), Wait: time.Second, MaxDatabaseBytes: 16 << 20}
	if _, err := CreateProtectedBackup(ctx, root, options, func(context.Context) error { return nil }, observe); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedRestoreCrashFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	_, _, scope := restoreFixtureScope(t)
	point := os.Getenv("MCE4D_RESTORE_FIXTURE_BOUNDARY")
	if point == "" {
		t.Fatal("missing synthetic stop boundary")
	}
	_, err := restoreProtectedStore(ctx, root, filepath.Join(restoreFixtureOutput(root, scope), "backup.json"), true, time.Second, func(current string) {
		if current == point {
			fmt.Println("RESTORE_FIXTURE_CONTROLLER_SIGKILL", current)
			_ = unix.Kill(unix.Getpid(), unix.SIGKILL)
			select {}
		}
	})
	t.Fatalf("restore did not reach %s: %v", point, err)
}

func TestProtectedRestoreBlockedFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	target, worktree, _ := restoreFixtureScope(t)
	report, err := ReadProtectedRecoveryReport(ctx, root, target, worktree)
	if err != nil || report.ActualStateVerified || !report.JournalPresent || report.Phase == "VERIFIED" {
		t.Fatalf("report %+v: %v", report, err)
	}
	if worktree == "" {
		if state, err := OpenProtectedState(ctx, root); !errors.Is(err, ErrStoreRecoveryRequired) || state != nil {
			t.Fatalf("catalog admitted: %v", err)
		}
	} else {
		if owner, err := OpenProtectedWorktree(ctx, root, worktree, func(context.Context) error { return nil }); !errors.Is(err, ErrStoreRecoveryRequired) || owner != nil {
			t.Fatalf("worktree admitted: %v", err)
		}
	}
}

func TestProtectedRestoreFinishFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	target, worktree, scope := restoreFixtureScope(t)
	before, err := ReadProtectedRecoveryReport(ctx, root, target, worktree)
	if err != nil || before.Phase == "VERIFIED" {
		t.Fatalf("expected unfinished operation: %+v %v", before, err)
	}
	if _, err := RecoverProtectedStore(ctx, root, target, worktree, false, time.Second); !errors.Is(err, ErrProfileInput) {
		t.Fatal("recovery did not require confirmation")
	}
	out, err := RecoverProtectedStore(ctx, root, target, worktree, true, time.Second)
	if err != nil || out.OperationID != before.OperationID || out.Outcome != "restored" || len(out.CompletedSteps) != len(maintenanceSteps(target)) {
		t.Fatalf("restore %+v: %v", out, err)
	}
	r, err := paths.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m, _, err := readProfileManifest(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	layout := layoutRestore(r, m, scope, out.OperationID)
	c, err := r.OpenDirectory(layout.control)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	j, present, err := readMaintenanceJournal(ctx, c, m.ProfileID, scope)
	if err != nil || !present || j.Phase != "VERIFIED" {
		t.Fatalf("final journal: %v", err)
	}
	for i, step := range layout.exchanges(target) {
		swapped, err := layout.restorePairState(ctx, step, j.Files[i])
		if err != nil || !swapped {
			t.Fatalf("rollback pair %s: %v", step.step, err)
		}
	}
	repeat, err := RecoverProtectedStore(ctx, root, target, worktree, true, time.Second)
	if err != nil || repeat.OperationID != out.OperationID {
		t.Fatalf("recovery retry: %v", err)
	}
}

func TestProtectedRestoreRefusesInconsistentPairFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	target, worktree, _ := restoreFixtureScope(t)
	report, err := ReadProtectedRecoveryReport(ctx, root, target, worktree)
	if err != nil || report.Phase != "PREPARED" {
		t.Fatalf("inconsistent fixture setup: %+v %v", report, err)
	}
	result, err := RecoverProtectedStore(ctx, root, target, worktree, true, time.Second)
	if !errors.Is(err, ErrStoreRecoveryRequired) || result.OperationID != "" {
		t.Fatalf("unproven recovery: %+v %v", result, err)
	}
}

func TestProtectedRestoreServingFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	_, _, scope := restoreFixtureScope(t)
	o, err := openQuiescentOwner(ctx, root, scope, time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	control := o.control
	if scope == "catalog" {
		control = o.profile.maintenanceControl
	}
	j, present, err := readMaintenanceJournal(ctx, control, o.profile.manifest.ProfileID, scope)
	if err != nil || !present || j.Phase != "VERIFIED" {
		t.Fatalf("recovered journal: %v", err)
	}
	for i, store := range o.stores {
		image, err := o.authenticatedImage(ctx, store)
		if err != nil || fmt.Sprintf("%x", image.SHA256) != j.NewStores[i].LogicalSHA256 {
			t.Fatalf("new WAL receipt: %v", err)
		}
		if err := o.openReadPool(ctx, store); err != nil {
			t.Fatal(err)
		}
		info, err := o.sourceMetadata(ctx, store)
		if err := errors.Join(err, o.closeReadPool()); err != nil || info.Incarnation != j.NewStores[i].Incarnation {
			t.Fatalf("new store identity: %v", err)
		}
	}
}
