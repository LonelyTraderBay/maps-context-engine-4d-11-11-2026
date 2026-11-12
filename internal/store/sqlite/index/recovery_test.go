package index

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const fixtureRecoveryID = "99999999-9999-4999-8999-999999999999"

type recoveryProbe struct{}

type failingRecoveryProbe struct {
	checks int
	failAt int
}

func (p *failingRecoveryProbe) HoldRecovery(ctx context.Context, worktree, id string) (ports.LeaseGuard, error) {
	if err := p.CheckRecovery(ctx, worktree, id); err != nil {
		return nil, err
	}
	return fixtureWriterGuard{}, nil
}
func (p *failingRecoveryProbe) CheckRecovery(ctx context.Context, worktree, id string) error {
	p.checks++
	if p.checks == p.failAt {
		return errFixtureLease
	}
	return (recoveryProbe{}).CheckRecovery(ctx, worktree, id)
}

func (recoveryProbe) HoldRecovery(ctx context.Context, worktree, id string) (ports.LeaseGuard, error) {
	if err := (recoveryProbe{}).CheckRecovery(ctx, worktree, id); err != nil {
		return nil, err
	}
	return fixtureWriterGuard{}, nil
}
func (recoveryProbe) CheckRecovery(ctx context.Context, worktree, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if worktree != fixtureWorktree || id != fixtureRecoveryID {
		return errFixtureLease
	}
	return nil
}

func permitRecovery(context.Context, string, ports.RecoveryCandidate) error { return nil }

func TestRecoverAbandonedPreservesActiveAndRollsBackRevocation(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	old := insertPreviousActive(t, db, stage)
	page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 2, permitRecovery)
	if err != nil || len(page) != 1 || page[0].GenerationID != fixtureGeneration || page[0].OwnerJobID != fixtureJob {
		t.Fatalf("wrong recovery page: %+v %v", page, err)
	}
	calls := 0
	revoke := func(context.Context, string, ports.RecoveryCandidate) error {
		calls++
		if calls == 2 {
			return errFixtureLease
		}
		return nil
	}
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], revoke); !errors.Is(err, errFixtureLease) {
		t.Fatalf("revocation ignored: %v", err)
	}
	status, err := store.ObserveRecoveryCandidate(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], permitRecovery)
	if err != nil || status != "building" {
		t.Fatalf("rejected recovery changed state: %s %v", status, err)
	}
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], permitRecovery); err != nil {
		t.Fatal(err)
	}
	status, err = store.ObserveRecoveryCandidate(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], permitRecovery)
	if err != nil || status != "failed" {
		t.Fatalf("wrong recovered outcome: %s %v", status, err)
	}
	var active string
	if err := db.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != old || blobCount(t, db) != 1 {
		t.Fatalf("recovery changed active or source blob: %s", active)
	}
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], permitRecovery); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("replayed recovery accepted: %v", err)
	}
}

func TestRecoveryPagesRejectIdentityMismatchAndDeniedAuthority(t *testing.T) {
	store, db, _ := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, id := range []string{"55555555-5555-4555-8555-555555555555", "77777777-7777-4777-8777-777777777777"} {
		if _, err := db.Exec(`INSERT INTO generations(generation_id,profile_id,snapshot_digest,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) SELECT ?,profile_id,snapshot_digest,'BUILDING',owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at FROM generations WHERE generation_id=?`, id, fixtureGeneration); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 1, permitRecovery)
	if err != nil || len(first) != 1 {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, first[0].GenerationID, 2, permitRecovery)
	if err != nil || len(second) != 2 || second[0].GenerationID <= first[0].GenerationID {
		t.Fatalf("second page: %+v %v", second, err)
	}
	wrong := first[0]
	wrong.PolicyEpoch++
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, wrong, permitRecovery); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("changed metadata accepted: %v", err)
	}
	if _, err := store.ObserveRecoveryCandidate(ctx, fixtureRecoveryID, recoveryProbe{}, wrong, permitRecovery); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("changed metadata observed: %v", err)
	}
	wrong = first[0]
	wrong.OwnerJobID = "88888888-8888-4888-8888-888888888888"
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, wrong, permitRecovery); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("changed owner accepted: %v", err)
	}
	wrong = first[0]
	wrong.SnapshotDigest = strings.Repeat("d", 64)
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, wrong, permitRecovery); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("changed snapshot accepted: %v", err)
	}
	denied := func(context.Context, string, ports.RecoveryCandidate) error { return errFixtureLease }
	page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 2, denied)
	if !errors.Is(err, errFixtureLease) || len(page) != 0 {
		t.Fatalf("denied listing leaked data: %+v %v", page, err)
	}
	wrong = first[0]
	wrong.OwnerJobID = fixtureRecoveryID
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, wrong, permitRecovery); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("own job accepted as abandoned: %v", err)
	}
}

func TestRecoveryRollsBackWhenLeaseIsLostBeforeCommit(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	old := insertPreviousActive(t, db, stage)
	page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 1, permitRecovery)
	if err != nil || len(page) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, &failingRecoveryProbe{failAt: 3}, page[0], permitRecovery); !errors.Is(err, errFixtureLease) {
		t.Fatalf("lost lease accepted: %v", err)
	}
	var active, state string
	if err := db.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if active != old || state != "BUILDING" {
		t.Fatalf("lease loss changed active=%s state=%s", active, state)
	}
}

func TestRecoveryListingHidesPageWhenLeaseIsLostBeforeReturn(t *testing.T) {
	store, _, _ := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, &failingRecoveryProbe{failAt: 4}, "", 1, permitRecovery)
	if !errors.Is(err, errFixtureLease) || len(page) != 0 {
		t.Fatalf("lost lease leaked page: %+v %v", page, err)
	}
}

func TestRecoveryReconcilesUnknownCommit(t *testing.T) {
	for _, test := range []struct {
		name string
		mode int32
		want string
	}{{"rolled_back", 1, "building"}, {"committed", 2, "failed"}} {
		t.Run(test.name, func(t *testing.T) {
			store, db, _ := stageFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 1, permitRecovery)
			if err != nil || len(page) != 1 {
				t.Fatalf("page: %+v %v", page, err)
			}
			var path string
			if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
				t.Fatal(err)
			}
			var mode atomic.Int32
			faultDB := sql.OpenDB(faultConnector{dsn: (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String(), mode: &mode})
			faultDB.SetMaxOpenConns(1)
			defer faultDB.Close()
			worker, err := Open(ctx, db, faultDB, fixtureWorktree, store.lease)
			if err != nil {
				t.Fatal(err)
			}
			mode.Store(test.mode)
			if err := worker.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], permitRecovery); !errors.Is(err, ErrCommitUnknown) || !errors.Is(err, errInjectedCommit) {
				t.Fatalf("uncertainty lost: %v", err)
			}
			state, err := store.ObserveRecoveryCandidate(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], permitRecovery)
			if err != nil || state != test.want {
				t.Fatalf("wrong reconciled state %s: %v", state, err)
			}
		})
	}
}

func TestRecoveryRejectsTamperedSchemaWithoutChangingRows(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	old := insertPreviousActive(t, db, stage)
	page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 1, permitRecovery)
	if err != nil || len(page) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	if _, err := db.Exec(`DROP TRIGGER generation_state_guard`); err != nil {
		t.Fatal(err)
	}
	if err := store.FailAbandonedGeneration(ctx, fixtureRecoveryID, recoveryProbe{}, page[0], permitRecovery); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("tampered schema accepted: %v", err)
	}
	var active, state string
	if err := db.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if active != old || state != "BUILDING" || blobCount(t, db) != 1 {
		t.Fatalf("tampered store mutated: active=%s state=%s", active, state)
	}
}

func TestRecoveryListingRejectsEmptyIdentityAndChecksEmptyScope(t *testing.T) {
	store, db, _ := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := db.Exec(`INSERT INTO generations(generation_id,profile_id,snapshot_digest,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) SELECT '',profile_id,snapshot_digest,'BUILDING',owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at FROM generations WHERE generation_id=?`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 2, permitRecovery); !errors.Is(err, ErrPublishIntegrity) || len(page) != 0 {
		t.Fatalf("bad identity escaped first page: %+v %v", page, err)
	}
	if _, err := db.Exec(`UPDATE generations SET state='FAILED' WHERE generation_id IN ('',?)`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	denied := func(context.Context, string, ports.RecoveryCandidate) error { return errFixtureLease }
	if page, err := store.ListBuildingForRecovery(ctx, fixtureRecoveryID, recoveryProbe{}, "", 2, denied); !errors.Is(err, errFixtureLease) || len(page) != 0 {
		t.Fatalf("empty listing bypassed scope authorization: %+v %v", page, err)
	}
}
