package sqlite

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func maintenanceJournalFixture() MaintenanceJournal {
	id := "11111111-1111-4111-8111-111111111111"
	profile := "22222222-2222-4222-8222-222222222222"
	return MaintenanceJournal{FormatVersion: 1, OperationID: id, ProfileInstallationID: profile, Target: "catalog", BackupDirectory: "approved-backup", BackupManifestSHA256: strings.Repeat("a", 64), Phase: "NEEDS_RECOVERY", PermissionReviewConfirmed: true, OldStores: []MaintenanceStore{{Kind: "catalog", Incarnation: profile, LogicalSHA256: strings.Repeat("b", 64)}}, NewStores: []MaintenanceStore{{Kind: "catalog", Incarnation: id}}, Files: []MaintenanceFileProof{}, CompletedSteps: []string{}}
}

func TestMaintenanceJournalRejectsWrongScopeAndUnprovenCompletion(t *testing.T) {
	m := maintenanceJournalFixture()
	raw, _ := json.Marshal(m)
	if _, err := decodeMaintenanceJournal(raw, m.ProfileInstallationID, "catalog"); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*MaintenanceJournal){
		func(m *MaintenanceJournal) { m.Phase = "VERIFIED"; m.Outcome = "restored" },
		func(m *MaintenanceJournal) { m.CompletedSteps = []string{"catalog_head"} },
		func(m *MaintenanceJournal) { m.BackupDirectory = "../../target" },
		func(m *MaintenanceJournal) { m.PermissionReviewConfirmed = false },
		func(m *MaintenanceJournal) { m.NewStores[0].Incarnation = m.OldStores[0].Incarnation },
	} {
		m := maintenanceJournalFixture()
		change(&m)
		raw, _ := json.Marshal(m)
		if _, err := decodeMaintenanceJournal(raw, m.ProfileInstallationID, "catalog"); err == nil {
			t.Fatal("unproven journal accepted")
		}
	}
	if _, err := decodeMaintenanceJournal(raw, m.ProfileInstallationID, m.OperationID); err == nil {
		t.Fatal("journal accepted for a different store")
	}
}

func TestMaintenanceAdvanceCannotChangePlanOrSkipSteps(t *testing.T) {
	old := maintenanceJournalFixture()
	next := maintenanceJournalFixture()
	next.Phase = "PREPARED"
	next.NewStores[0].LogicalSHA256 = strings.Repeat("d", 64)
	for _, step := range maintenanceSteps("catalog") {
		next.Files = append(next.Files, MaintenanceFileProof{Step: step, OldSHA256: strings.Repeat("b", 64), NewSHA256: strings.Repeat("c", 64)})
	}
	if !validMaintenanceAdvance(old, next) {
		t.Fatal("preparation refused")
	}
	for _, change := range []func(*MaintenanceJournal){
		func(m *MaintenanceJournal) { m.BackupDirectory = "other" },
		func(m *MaintenanceJournal) { m.BackupManifestSHA256 = strings.Repeat("e", 64) },
		func(m *MaintenanceJournal) {
			m.NewStores = append([]MaintenanceStore(nil), m.NewStores...)
			m.NewStores[0].Incarnation = m.ProfileInstallationID
		},
		func(m *MaintenanceJournal) { m.Phase = "VERIFYING"; m.CompletedSteps = maintenanceSteps("catalog") },
	} {
		bad := next
		change(&bad)
		if validMaintenanceAdvance(old, bad) {
			t.Fatal("changed plan/phase accepted")
		}
	}
	old = next
	next.Phase = "REPLACING"
	next.CompletedSteps = []string{"catalog_main"}
	if !validMaintenanceAdvance(old, next) {
		t.Fatal("first replacement refused")
	}
	old = next
	next.CompletedSteps = maintenanceSteps("catalog")
	if validMaintenanceAdvance(old, next) {
		t.Fatal("skipped unjournaled steps")
	}
}

func TestMaintenanceIndexRollbackBindsScopeAndPreservedRecords(t *testing.T) {
	prepare := func() MaintenanceJournal {
		j := maintenanceJournalFixture()
		j.Target, j.WorktreeID, j.Phase = "worktree", "33333333-3333-4333-8333-333333333333", "PREPARED"
		j.BackupDirectory = "index-preimage"
		j.OldStores[0].Kind = "index"
		j.OldStores[0].LogicalSHA256 = j.BackupManifestSHA256
		j.NewStores[0].Kind = "index"
		j.NewStores[0].LogicalSHA256 = strings.Repeat("b", 64)
		j.IndexRollback = &MaintenanceIndexRollback{
			Records:  MaintenanceStore{"records", "44444444-4444-4444-8444-444444444444", strings.Repeat("c", 64)},
			Sidecars: []MaintenanceSidecar{{Suffix: "-wal", SHA256: strings.Repeat("d", 64)}},
		}
		for _, step := range maintenanceJournalSteps(j) {
			j.Files = append(j.Files, MaintenanceFileProof{step, strings.Repeat("e", 64), strings.Repeat("f", 64)})
		}
		return j
	}
	valid := func(j MaintenanceJournal) bool {
		raw, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeMaintenanceJournal(raw, j.ProfileInstallationID, j.WorktreeID)
		return err == nil
	}
	if j := prepare(); !valid(j) {
		t.Fatal("scoped index rollback journal refused")
	}
	for _, change := range []func(*MaintenanceJournal){
		func(j *MaintenanceJournal) { j.WorktreeID = "catalog" },
		func(j *MaintenanceJournal) { j.IndexRollback.Sidecars[0].Suffix = "-other" },
		func(j *MaintenanceJournal) {
			j.IndexRollback.Sidecars = append(j.IndexRollback.Sidecars, j.IndexRollback.Sidecars[0])
		},
		func(j *MaintenanceJournal) { j.IndexRollback.Records.Incarnation = j.NewStores[0].Incarnation },
		func(j *MaintenanceJournal) { j.Files[0].Step = "records_main" },
	} {
		j := prepare()
		change(&j)
		if valid(j) {
			t.Fatal("invalid rollback journal accepted")
		}
	}
	previous, next := prepare(), prepare()
	next.Phase = "REPLACING"
	next.IndexRollback.Sidecars[0].SHA256 = strings.Repeat("e", 64)
	if validMaintenanceAdvance(previous, next) {
		t.Fatal("replacement changed the authorized sidecar proof")
	}
}

func TestMaintenanceIndexRebuildBindsPreservedRecords(t *testing.T) {
	rebuild := func() MaintenanceJournal {
		j := maintenanceJournalFixture()
		j.Target, j.WorktreeID, j.Phase = "worktree", "33333333-3333-4333-8333-333333333333", "PREPARED"
		j.BackupDirectory = "index-rebuild"
		j.OldStores[0].Kind, j.OldStores[0].LogicalSHA256 = "index", j.BackupManifestSHA256
		j.NewStores[0].Kind, j.NewStores[0].LogicalSHA256 = "index", strings.Repeat("b", 64)
		j.IndexRebuild = &MaintenanceIndexRebuild{
			RecordsIncarnation: "44444444-4444-4444-8444-444444444444", RecordsSHA256: strings.Repeat("c", 64),
		}
		for _, step := range maintenanceJournalSteps(j) {
			j.Files = append(j.Files, MaintenanceFileProof{step, strings.Repeat("e", 64), strings.Repeat("f", 64)})
		}
		return j
	}
	check := func(j MaintenanceJournal) error {
		raw, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeMaintenanceJournal(raw, j.ProfileInstallationID, j.WorktreeID)
		return err
	}
	if err := check(rebuild()); err != nil {
		t.Fatalf("index-only rebuild journal: %v", err)
	}
	for _, change := range []func(*MaintenanceJournal){
		func(j *MaintenanceJournal) { j.IndexRebuild.RecordsIncarnation = j.NewStores[0].Incarnation },
		func(j *MaintenanceJournal) { j.IndexRebuild.RecordsSHA256 = "" },
		func(j *MaintenanceJournal) { j.Files[0].Step = "records_main" },
		func(j *MaintenanceJournal) { j.IndexRollback = &MaintenanceIndexRollback{} },
	} {
		j := rebuild()
		change(&j)
		if err := check(j); !errors.Is(err, ErrStoreRecoveryRequired) {
			t.Fatalf("invalid rebuild journal: %v", err)
		}
	}
	before, after := rebuild(), rebuild()
	after.Phase = "REPLACING"
	after.IndexRebuild.RecordsSHA256 = strings.Repeat("d", 64)
	if validMaintenanceAdvance(before, after) {
		t.Fatal("replacement changed the preserved records proof")
	}
}
