//go:build linux

package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// The controller seeds metadata phases, not restored databases. These tests
// establish serving isolation and read-only reporting, not restore completion.
func TestProtectedMaintenanceJournalSeedFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	r, err := paths.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m, _, err := readProfileManifest(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	target, phase := os.Getenv("MCE4D_MAINTENANCE_TARGET"), os.Getenv("MCE4D_MAINTENANCE_PHASE")
	relative := "control/catalog"
	j := maintenanceJournalFixture()
	j.ProfileInstallationID = m.ProfileID
	j.OldStores[0].Incarnation = m.CatalogIncarnation
	if target == "worktree" {
		j.Target = target
		j.WorktreeID = ownerWorktree
		relative = "control/worktrees/" + ownerWorktree
		control, err := r.OpenDirectory(relative)
		if err != nil {
			t.Fatal(err)
		}
		w, _, err := readWorktreeManifest(ctx, control, m.ProfileID, ownerWorktree)
		if err := errors.Join(err, control.Close()); err != nil {
			t.Fatal(err)
		}
		j.OldStores = []MaintenanceStore{{Kind: "index", Incarnation: w.IndexIncarnation, LogicalSHA256: strings.Repeat("b", 64)}, {Kind: "records", Incarnation: w.RecordsIncarnation, LogicalSHA256: strings.Repeat("c", 64)}}
		first, err := profileUUID()
		if err != nil {
			t.Fatal(err)
		}
		second, err := profileUUID()
		if err != nil {
			t.Fatal(err)
		}
		j.NewStores = []MaintenanceStore{{Kind: "index", Incarnation: first}, {Kind: "records", Incarnation: second}}
	} else if target != "catalog" {
		t.Fatal("invalid fixture target")
	}
	control, err := r.OpenDirectory(relative)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if err := writeMaintenanceJournal(ctx, control, j, false); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(root + "/" + relative + "/maintenance.json")
	if err != nil {
		t.Fatal(err)
	}
	// Initial write must not overwrite an existing operation.
	if err := writeMaintenanceJournal(ctx, control, j, false); err == nil {
		t.Fatal("existing journal overwritten")
	}
	after, err := os.ReadFile(root + "/" + relative + "/maintenance.json")
	if err != nil || string(original) != string(after) {
		t.Fatal("journal changed after refused retry")
	}
	if phase != "NEEDS_RECOVERY" {
		for i := range j.NewStores {
			j.NewStores[i].LogicalSHA256 = strings.Repeat("d", 64)
		}
		for _, step := range maintenanceSteps(j.Target) {
			j.Files = append(j.Files, MaintenanceFileProof{Step: step, OldSHA256: strings.Repeat("a", 64), NewSHA256: strings.Repeat("e", 64)})
		}
		j.Phase = "PREPARED"
		if err := writeMaintenanceJournal(ctx, control, j, true); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "REPLACING" || phase == "VERIFYING" {
		j.Phase = "REPLACING"
		if err := writeMaintenanceJournal(ctx, control, j, true); err != nil {
			t.Fatal(err)
		}
		for _, step := range maintenanceSteps(j.Target) {
			j.CompletedSteps = append(j.CompletedSteps, step)
			if err := writeMaintenanceJournal(ctx, control, j, true); err != nil {
				t.Fatal(err)
			}
			if phase == "REPLACING" {
				break
			}
		}
	}
	if phase == "VERIFYING" {
		j.Phase = phase
		if err := writeMaintenanceJournal(ctx, control, j, true); err != nil {
			t.Fatal(err)
		}
	}
	if j.Phase != phase {
		t.Fatal("unsupported fixture phase")
	}
	if _, err := ReadProtectedRecoveryReport(ctx, root, j.Target, j.WorktreeID); err != nil {
		t.Fatal(err)
	}
	fmt.Println("NATIVE_MAINTENANCE_JOURNAL_METADATA_PHASE_SEEDED")
}

func TestProtectedMaintenanceJournalServingGateFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	target, phase := os.Getenv("MCE4D_MAINTENANCE_TARGET"), os.Getenv("MCE4D_MAINTENANCE_PHASE")
	worktree := ""
	if target == "worktree" {
		worktree = ownerWorktree
	}
	before, err := ReadProtectedRecoveryReport(ctx, root, target, worktree)
	if err != nil || !before.JournalPresent || before.Phase != phase || before.ActualStateVerified {
		t.Fatalf("report: %+v %v", before, err)
	}
	p, err := openProtectedProfile(ctx, root, "READY")
	if err != nil {
		t.Fatal(err)
	}
	if target == "catalog" {
		if err := p.Check(ctx); !errors.Is(err, ErrStoreRecoveryRequired) {
			t.Fatalf("catalog gate: %v", err)
		}
	} else if err := p.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = OpenProtectedWorktree(ctx, root, ownerWorktree, func(context.Context) error { return nil })
	if !errors.Is(err, ErrStoreRecoveryRequired) {
		t.Fatalf("worktree served incomplete operation: %v", err)
	}
	if target == "worktree" {
		other := maintenanceChild(ctx, "other")
		if raw, err := other.CombinedOutput(); err != nil {
			t.Fatalf("unrelated writer blocked: %s %v", raw, err)
		}
	}
	after, err := ReadProtectedRecoveryReport(ctx, root, target, worktree)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("report mutated metadata")
	}
	fmt.Println("NATIVE_MAINTENANCE_PHASE_BLOCKS_SERVING_AND_READONLY_REPORT_PASS")
}
