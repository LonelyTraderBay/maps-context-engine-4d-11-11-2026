package index

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func publishRequest(stage StageFilesRequest) PublishCapturedRequest {
	return PublishCapturedRequest{Capture: stage.Capture, AuthorizeGeneration: func(context.Context, string, string, string, int64) error { return nil }, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }}
}

func insertPreviousActive(t *testing.T, db *sql.DB, stage StageFilesRequest) string {
	t.Helper()
	const old = "66666666-6666-4666-8666-666666666666"
	m := stage.Capture.Snapshot.Manifest()
	_, err := db.Exec(`INSERT INTO generations(generation_id,profile_id,snapshot_digest,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,?,'BUILDING',?,?,1,?,'2026-01-01T00:00:00Z')`, old, m.ProfileID, m.SnapshotDigest, fixtureJob, m.PolicyDigest, m.ProviderDigest)
	if err != nil {
		t.Fatal(err)
	}
	publishFixture(t, db, old, m, true)
	return old
}

func TestPublishCapturedOnlyCommitsPartialAndActiveTogether(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	old := insertPreviousActive(t, db, stage)
	request := publishRequest(stage)
	checks := 0
	request.Revalidate = func(_ context.Context, captured *snapshot.Snapshot) error {
		checks++
		if captured != stage.Capture.Snapshot {
			return ErrCaptureMismatch
		}
		return nil
	}
	if err := store.PublishCapturedOnly(ctx, request); err != nil {
		t.Fatal(err)
	}
	var active, state, capability, diagnostic string
	if err := db.QueryRow(`SELECT a.generation_id,g.state FROM active_generations a JOIN generations g ON g.generation_id=a.generation_id WHERE a.profile_id=?`, stage.Capture.Snapshot.Manifest().ProfileID).Scan(&active, &state); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM capability_results WHERE generation_id=?`, fixtureGeneration).Scan(&capability); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT code FROM diagnostics WHERE generation_id=?`, fixtureGeneration).Scan(&diagnostic); err != nil {
		t.Fatal(err)
	}
	if active == old || active != fixtureGeneration || state != "READY_PARTIAL" || capability != "unknown" || diagnostic != "PROVIDER_NOT_RUN" || checks != 1 {
		t.Fatalf("partial publish mismatch active=%s state=%s capability=%s diagnostic=%s checks=%d", active, state, capability, diagnostic, checks)
	}
	file, err := store.ReadPublishedFile(ctx, readFixtureRequest(t, stage))
	if err != nil || string(file.Bytes) != "package main\n" || file.State != "READY_PARTIAL" {
		t.Fatalf("published source unavailable: %+v %v", file, err)
	}
}

func TestPublishCapturedOnlyRollsBackAfterWriterLoss(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	old := insertPreviousActive(t, db, stage)
	request := publishRequest(stage)
	request.Capture.Writer = &writerProbe{failAt: 2}
	if err := store.PublishCapturedOnly(ctx, request); !errors.Is(err, errFixtureLease) {
		t.Fatalf("writer loss accepted: %v", err)
	}
	var active, state string
	var diagnostics int
	if err := db.QueryRow(`SELECT generation_id FROM active_generations WHERE profile_id=?`, stage.Capture.Snapshot.Manifest().ProfileID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM diagnostics WHERE generation_id=?`, fixtureGeneration).Scan(&diagnostics); err != nil {
		t.Fatal(err)
	}
	if active != old || state != "BUILDING" || diagnostics != 0 {
		t.Fatalf("partial commit active=%s state=%s diagnostics=%d", active, state, diagnostics)
	}
}

func TestPublishCapturedOnlyRejectsUnstableOrIncompleteStaging(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, *Store, *sql.DB, StageFilesRequest, *PublishCapturedRequest)
		want  error
	}{
		{"missing-file", func(t *testing.T, s *Store, db *sql.DB, stage StageFilesRequest, r *PublishCapturedRequest) {}, ErrPublishIntegrity},
		{"unstable", func(t *testing.T, s *Store, db *sql.DB, stage StageFilesRequest, r *PublishCapturedRequest) {
			if err := s.StageFiles(contextWithDeadline(t), stage); err != nil {
				t.Fatal(err)
			}
			r.Revalidate = func(context.Context, *snapshot.Snapshot) error { return errFixtureLease }
		}, errFixtureLease},
		{"unexpected-facts", func(t *testing.T, s *Store, db *sql.DB, stage StageFilesRequest, r *PublishCapturedRequest) {
			if err := s.StageFiles(contextWithDeadline(t), stage); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO capability_results(generation_id,capability_key,scope_key,status,summary_json) VALUES(?,'unexpected','workspace','unknown','{}')`, fixtureGeneration); err != nil {
				t.Fatal(err)
			}
		}, ErrPublishIntegrity},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, db, stage := stageFixture(t)
			request := publishRequest(stage)
			test.setup(t, store, db, stage, &request)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := store.PublishCapturedOnly(ctx, request); !errors.Is(err, test.want) {
				t.Fatalf("invalid publish accepted: %v", err)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM active_generations`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("failed publish changed active pointer")
			}
		})
	}
}

func contextWithDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPublishCapturedOnlyRejectsCorruptFTS(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO search_fts(rowid,name,qualified_name,body) VALUES(999,'orphan','orphan','orphan')`); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishCapturedOnly(ctx, publishRequest(stage)); !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("corrupt FTS accepted: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM active_generations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("corrupt FTS changed active pointer")
	}
}

func TestPublishCapturedOnlyRejectsActiveProviderStream(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.PublishCapturedOnly(ctx, publishRequest(StageFilesRequest{Capture: stage.Capture})); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("active provider stream bypassed: %v", err)
	}
}

func TestPublishRejectsConcurrentDatabaseChangeAfterValidation(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	old := insertPreviousActive(t, db, stage)
	var sequence int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	dsn := fixtureSQLiteURI(path)
	other, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	request := publishRequest(stage)
	request.Revalidate = func(ctx context.Context, _ *snapshot.Snapshot) error {
		_, err := other.ExecContext(ctx, `INSERT INTO diagnostics(generation_id,diagnostic_id,path,package_key,code,severity,sanitized_message,scope_json) VALUES(?,'concurrent',NULL,NULL,'CONCURRENT','warning','Concurrent change.',?)`, fixtureGeneration, `{"scope_description":"workspace"}`)
		return err
	}
	if err := store.PublishCapturedOnly(ctx, request); !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("concurrent mutation escaped validation: %v", err)
	}
	var active, state string
	if err := db.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if active != old || state != "BUILDING" {
		t.Fatalf("failed publish changed active=%s state=%s", active, state)
	}
}

func TestPublishCancellationBeforeCommitKeepsPreviousActive(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	old := insertPreviousActive(t, db, stage)
	request := publishRequest(stage)
	checks := 0
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error {
		checks++
		if checks == 3 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	if err := store.PublishCapturedOnly(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publication was accepted: %v", err)
	}
	var active, state string
	if err := db.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if active != old || state != "BUILDING" {
		t.Fatalf("cancel changed active=%s state=%s", active, state)
	}
}

type failingStreamReleaseWriter struct{ *writerProbe }
type failingStreamReleaseGuard struct{ ports.LeaseGuard }

func (w failingStreamReleaseWriter) HoldIndexStream(ctx context.Context, worktree, job string) (ports.LeaseGuard, error) {
	guard, err := w.writerProbe.HoldIndexStream(ctx, worktree, job)
	if err != nil {
		return nil, err
	}
	return failingStreamReleaseGuard{guard}, nil
}

func (g failingStreamReleaseGuard) Close() error {
	return errors.Join(g.LeaseGuard.Close(), errFixtureLease)
}

func TestPublishReconcilesFailureAfterCommitByJob(t *testing.T) {
	store, _, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	stage.Capture.Writer = failingStreamReleaseWriter{&writerProbe{}}
	err := store.PublishCapturedOnly(ctx, publishRequest(stage))
	if !errors.Is(err, ErrAfterCommit) || !errors.Is(err, errFixtureLease) {
		t.Fatalf("lost committed outcome: %v", err)
	}
	outcome, err := store.PublicationOutcomeByJob(ctx, PublicationOutcomeRequest{GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: stage.Capture.Snapshot, MaxManifestBytes: 8192, Authorize: func(context.Context, string, string, string, int64) error { return nil }})
	if err != nil || outcome.Status != "published_active" {
		t.Fatalf("could not reconcile committed job: %+v %v", outcome, err)
	}
}

func TestPublishRejectsWrongOwnerAndProfile(t *testing.T) {
	for _, kind := range []string{"owner", "profile"} {
		t.Run(kind, func(t *testing.T) {
			store, db, stage := stageFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := store.StageFiles(ctx, stage); err != nil {
				t.Fatal(err)
			}
			old := insertPreviousActive(t, db, stage)
			request := publishRequest(stage)
			if kind == "owner" {
				if _, err := db.Exec(`UPDATE generations SET owner_job_id=? WHERE generation_id=?`, "77777777-7777-4777-8777-777777777777", fixtureGeneration); err != nil {
					t.Fatal(err)
				}
			} else {
				manifest := stage.Capture.Snapshot.Manifest()
				manifest.ProfileID, manifest.SnapshotDigest = strings.Repeat("d", 64), ""
				file, _ := stage.Capture.Snapshot.File("main.go")
				other, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
				if err != nil {
					t.Fatal(err)
				}
				request.Capture.Snapshot = other
			}
			if err := store.PublishCapturedOnly(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
				t.Fatalf("wrong %s accepted: %v", kind, err)
			}
			var active, state string
			if err := db.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if active != old || state != "BUILDING" {
				t.Fatalf("wrong %s changed active=%s state=%s", kind, active, state)
			}
		})
	}
}
