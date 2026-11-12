package index

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func beginFixture(t *testing.T) (*Store, *sql.DB, BeginGenerationRequest) {
	t.Helper()
	store, db, capture := captureWriteFixture(t, "main.go", "package main\n")
	if _, err := db.Exec(`UPDATE generations SET state='FAILED' WHERE generation_id=?`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM generations WHERE generation_id=?`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	request := BeginGenerationRequest{GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: capture.Snapshot, Writer: &writerProbe{}, MaxManifestBytes: 8192,
		Authorize: func(context.Context, string, string, string, int64) error { return nil }}
	return store, db, request
}

func generationCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM generations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestBeginGenerationPinsSnapshotAndDoesNotPublish(t *testing.T) {
	store, db, request := beginFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	checks := 0
	request.Authorize = func(_ context.Context, worktree, profile, policy string, epoch int64) error {
		checks++
		manifest := request.Snapshot.Manifest()
		if worktree != fixtureWorktree || profile != manifest.ProfileID || policy != manifest.PolicyDigest || epoch != 1 {
			t.Fatal("wrong authorization scope")
		}
		return nil
	}
	if err := store.BeginGeneration(ctx, request); err != nil {
		t.Fatal(err)
	}
	var state, digest, owner string
	if err := db.QueryRow(`SELECT state,snapshot_digest,owner_job_id FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state, &digest, &owner); err != nil {
		t.Fatal(err)
	}
	if state != "BUILDING" || digest != request.Snapshot.Manifest().SnapshotDigest || owner != fixtureJob || checks != 2 {
		t.Fatalf("unexpected row state=%s digest=%s owner=%s checks=%d", state, digest, owner, checks)
	}
	var active int
	if err := db.QueryRow(`SELECT count(*) FROM active_generations`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatal("BUILDING generation became active")
	}
	if err := store.BeginGeneration(ctx, request); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("duplicate generation accepted: %v", err)
	}
}

func TestBeginGenerationRollsBackOnAuthorityAndLeaseLoss(t *testing.T) {
	for _, test := range []struct {
		name   string
		modify func(*Store, *BeginGenerationRequest)
	}{
		{"authority", func(_ *Store, r *BeginGenerationRequest) {
			calls := 0
			r.Authorize = func(context.Context, string, string, string, int64) error {
				calls++
				if calls == 2 {
					return errFixtureLease
				}
				return nil
			}
		}},
		{"writer", func(_ *Store, r *BeginGenerationRequest) { r.Writer = &writerProbe{failAt: 2} }},
		{"lifecycle", func(s *Store, _ *BeginGenerationRequest) {
			lease := s.lease.(*leaseProbe)
			lease.failAt = lease.calls + 2
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, db, request := beginFixture(t)
			test.modify(store, &request)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := store.BeginGeneration(ctx, request); !errors.Is(err, errFixtureLease) {
				t.Fatalf("lost authority accepted: %v", err)
			}
			if generationCount(t, db) != 0 {
				t.Fatal("failed transaction left BUILDING row")
			}
		})
	}
}

func TestBeginGenerationRejectsMissingDeadlineAndCancellation(t *testing.T) {
	store, db, request := beginFixture(t)
	if err := store.BeginGeneration(context.Background(), request); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("missing deadline accepted: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	cancel()
	if err := store.BeginGeneration(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request accepted: %v", err)
	}
	if generationCount(t, db) != 0 {
		t.Fatal("invalid request wrote generation")
	}
}

type failingCloseWriter struct{}
type failingCloseGuard struct{}

func (failingCloseWriter) CheckWriter(context.Context, string, string) error { return nil }
func (failingCloseWriter) HoldWriter(context.Context, string, string) (ports.LeaseGuard, error) {
	return failingCloseGuard{}, nil
}
func (failingCloseGuard) Close() error { return errFixtureLease }

func TestBeginGenerationReportsCommittedFinalizationFailure(t *testing.T) {
	store, db, request := beginFixture(t)
	request.Writer = failingCloseWriter{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := store.BeginGeneration(ctx, request)
	if !errors.Is(err, ErrAfterCommit) || !errors.Is(err, errFixtureLease) || generationCount(t, db) != 1 {
		t.Fatalf("commit/finalization outcome hidden: %v", err)
	}
}
