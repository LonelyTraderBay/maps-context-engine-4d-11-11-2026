package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const gcActive = "77777777-7777-4777-8777-777777777777"
const gcOther = "88888888-8888-4888-8888-888888888888"

// Actual migrated SQLite and Store GC, fake lease/authorization. Synthetic SQL
// facts deliberately include all R1 FK dependencies; this is not a provider or
// native publication test. The active captured file remains readable by Store.
func gcStoreFixture(t *testing.T, cycle bool) (context.Context, *Store, *sql.DB, GCRequest, StageFilesRequest) {
	t.Helper()
	store, db, stage := stageFixture(t)
	ctx := contextWithDeadline(t)
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	m := stage.Capture.Snapshot.Manifest()
	exec(`INSERT INTO packages VALUES(?,'pkg','example.invalid/p','module','production',?,'partial')`, fixtureGeneration, m.SnapshotDigest)
	exec(`UPDATE files SET package_key='pkg' WHERE generation_id=?`, fixtureGeneration)
	exec(`INSERT INTO symbols VALUES(?,'symbol','main.go','pkg','function','main','p.main','scope',0,7,'',0)`, fixtureGeneration)
	exec(`INSERT INTO relations VALUES(?,'relation','symbol','symbol','ref','syntax_exact','main.go',0,7,'proof',NULL)`, fixtureGeneration)
	exec(`INSERT INTO search_documents(generation_id,item_id,path,kind,name,qualified_name,body) VALUES(?,'doc','main.go','file','oldphrase','oldphrase','package main')`, fixtureGeneration)
	exec(`INSERT INTO diagnostics VALUES(?,'diagnostic','main.go','pkg','FIXTURE','warning','Fixture.','{}')`, fixtureGeneration)
	exec(`INSERT INTO capability_results VALUES(?,'fixture','workspace','partial','{}')`, fixtureGeneration)
	for _, pair := range [][2]string{{"root", ""}, {"middle", "root"}, {"leaf", "middle"}} {
		var parent any
		if pair[1] != "" {
			parent = pair[1]
		}
		exec(`INSERT INTO modules VALUES(?,?,?,?,'module','observed_static','main.go',0,7)`, fixtureGeneration, pair[0], parent, pair[0])
	}
	if cycle {
		exec(`UPDATE modules SET parent_module_id='leaf' WHERE generation_id=? AND module_id='root'`, fixtureGeneration)
	}
	exec(`INSERT INTO module_members VALUES(?,'leaf','pkg')`, fixtureGeneration)
	// An unreferenced blob is owned cache too; referenced shared bytes must stay.
	orphan := []byte("bounded unreferenced synthetic bytes\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(orphan))
	exec(`INSERT INTO content_blobs VALUES(?,?,?)`, hash, orphan, len(orphan))
	for i, id := range []string{gcActive, gcOther} {
		profile := m.ProfileID
		if i == 1 {
			profile = strings.Repeat("d", 64)
		}
		exec(`INSERT INTO generations(generation_id,profile_id,snapshot_digest,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,?,'BUILDING',?,?,1,?,'2026-02-01T00:00:00Z')`, id, profile, m.SnapshotDigest, fixtureJob, m.PolicyDigest, m.ProviderDigest)
		exec(`INSERT INTO files(generation_id,path,content_hash,kind,status,byte_length) SELECT ?,'main.go',content_hash,'go','indexed',byte_length FROM files WHERE generation_id=?`, id, fixtureGeneration)
		copy := m
		copy.ProfileID = profile
		publishFixture(t, db, id, copy, true)
	}
	publishFixture(t, db, fixtureGeneration, m, false)
	r := GCRequest{ProfileID: m.ProfileID, JobID: fixtureJob, KeepGenerations: 1, Writer: &writerProbe{}, Authorize: func(context.Context, string, string) error { return nil }}
	return ctx, store, db, r, stage
}

func gcData(t *testing.T, db *sql.DB) [32]byte {
	t.Helper()
	var all []any
	for _, table := range []string{"schema_meta", "store_identity", "generations", "active_generations", "content_blobs", "packages", "files", "symbols", "relations", "diagnostics", "capability_results", "search_documents", "modules", "module_members", "search_fts"} {
		rows, err := db.Query(`SELECT rowid,* FROM ` + table + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			pointers := make([]any, len(cols))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			all = append(all, []any{table, values})
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(raw)
}

func TestGCReadOnlyReportExactRowsSharedBlobAndActiveProtection(t *testing.T) {
	ctx, store, db, r, stage := gcStoreFixture(t, false)
	before := gcData(t, db)
	var changes int64
	if err := db.QueryRow(`SELECT total_changes()`).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanGC(ctx, r, 1)
	if err != nil || !reflect.DeepEqual(plan.GenerationIDs, []string{fixtureGeneration}) || plan.More {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	dry, err := store.CollectGeneration(ctx, r, fixtureGeneration, false)
	if err != nil || dry.Applied || dry.Rows["modules"] != 3 || dry.Rows["content_blobs"] != 1 || dry.BlobBytes != int64(len("bounded unreferenced synthetic bytes\n")) {
		t.Fatalf("dry: %+v %v", dry, err)
	}
	var afterChanges int64
	if err := db.QueryRow(`SELECT total_changes()`).Scan(&afterChanges); err != nil || changes != afterChanges || before != gcData(t, db) {
		t.Fatalf("report mutated database: %d -> %d %v", changes, afterChanges, err)
	}
	for _, protected := range []string{gcActive, gcOther} {
		if _, err := store.CollectGeneration(ctx, r, protected, true); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("protected generation accepted: %s %v", protected, err)
		}
	}
	applied, err := store.CollectGeneration(ctx, r, fixtureGeneration, true)
	if err != nil || !applied.Applied || !reflect.DeepEqual(applied.Rows, dry.Rows) || applied.BlobBytes != dry.BlobBytes {
		t.Fatalf("apply differs from report: %+v %+v %v", dry, applied, err)
	}
	if blobCount(t, db) != 1 {
		t.Fatal("shared blob removed")
	}
	file, err := store.ReadPublishedFile(ctx, readFixtureRequest(t, stage))
	if err != nil || file.GenerationID != gcActive || string(file.Bytes) != "package main\n" {
		t.Fatalf("active source lost: %+v %v", file, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM search_fts WHERE search_fts MATCH 'oldphrase'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("FTS trigger not synchronized: %d %v", count, err)
	}
	again, err := store.CollectGeneration(ctx, r, fixtureGeneration, true)
	if err != nil || !again.AlreadyAbsent || again.Applied {
		t.Fatalf("idempotent retry: %+v %v", again, err)
	}
	plan, err = store.PlanGC(ctx, r, 128)
	if err != nil || len(plan.GenerationIDs) != 0 {
		t.Fatalf("replan: %+v %v", plan, err)
	}
}

func TestGCRollbackAtEveryAuthorityBoundaryAndCancellation(t *testing.T) {
	ctx, store, _, r, _ := gcStoreFixture(t, false)
	checks := 0
	r.Authorize = func(context.Context, string, string) error { checks++; return nil }
	if _, err := store.CollectGeneration(ctx, r, fixtureGeneration, true); err != nil {
		t.Fatal(err)
	}
	if checks < 14 {
		t.Fatalf("missing staged authority checks: %d", checks)
	}
	for failure := 1; failure <= checks; failure++ {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			ctx, store, db, r, _ := gcStoreFixture(t, false)
			before := gcData(t, db)
			calls := 0
			r.Authorize = func(context.Context, string, string) error {
				calls++
				if calls == failure {
					return errFixtureLease
				}
				return nil
			}
			if _, err := store.CollectGeneration(ctx, r, fixtureGeneration, true); !errors.Is(err, errFixtureLease) {
				t.Fatalf("fault accepted: %v", err)
			}
			if before != gcData(t, db) {
				t.Fatal("partial retirement/deletion persisted")
			}
			r.Authorize = func(context.Context, string, string) error { return nil }
			if _, err := store.CollectGeneration(ctx, r, fixtureGeneration, true); err != nil {
				t.Fatalf("retry after rollback: %v", err)
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		ctx, store, db, r, _ := gcStoreFixture(t, false)
		before := gcData(t, db)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		calls := 0
		r.Authorize = func(context.Context, string, string) error {
			calls++
			if calls == 6 {
				cancel()
			}
			return ctx.Err()
		}
		if _, err := store.CollectGeneration(ctx, r, fixtureGeneration, true); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel accepted: %v", err)
		}
		if before != gcData(t, db) {
			t.Fatal("cancel left partial deletion")
		}
	})
}

func TestGCCycleAndFutureSchemaRefuseWithoutMutation(t *testing.T) {
	for _, cycle := range []bool{true, false} {
		t.Run(fmt.Sprint(cycle), func(t *testing.T) {
			ctx, store, db, r, _ := gcStoreFixture(t, cycle)
			if !cycle {
				if _, err := db.Exec(`UPDATE schema_meta SET schema_version=3,min_reader_version=3`); err != nil {
					t.Fatal(err)
				}
			}
			before := gcData(t, db)
			for _, apply := range []bool{false, true} {
				_, err := store.CollectGeneration(ctx, r, fixtureGeneration, apply)
				if cycle && !errors.Is(err, ErrGCIntegrity) || !cycle && !errors.Is(err, ErrSchemaTooNew) {
					t.Fatalf("unsafe collection accepted: %v", err)
				}
				if before != gcData(t, db) {
					t.Fatal("refusal changed database")
				}
			}
		})
	}
}

func TestGCRetentionIncludesOldActiveAndExcludesBuilding(t *testing.T) {
	ctx, store, db, r, _ := gcStoreFixture(t, false)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	const newest = "99999999-9999-4999-8999-999999999999"
	const building = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, id := range []string{newest, building} {
		state := "BUILDING"
		if id == newest {
			state = "READY"
		}
		exec(`INSERT INTO generations(generation_id,profile_id,snapshot_digest,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,manifest_json,created_at,completed_at) SELECT ?,profile_id,snapshot_digest,?,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,manifest_json,'2027-01-01T00:00:00Z','2027-01-01T00:00:00Z' FROM generations WHERE generation_id=?`, id, state, fixtureGeneration)
	}
	r.KeepGenerations = 2
	plan, err := store.PlanGC(ctx, r, 1)
	if err != nil || !reflect.DeepEqual(plan.GenerationIDs, []string{fixtureGeneration}) || plan.More {
		t.Fatalf("retention: %+v %v", plan, err)
	}
	for _, id := range []string{newest, building, gcActive} {
		if _, err := store.CollectGeneration(ctx, r, id, true); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("retained/building accepted: %s %v", id, err)
		}
	}
	if _, err := store.CollectGeneration(ctx, r, fixtureGeneration, true); err != nil {
		t.Fatal(err)
	}
	plan, err = store.PlanGC(ctx, r, 128)
	if err != nil || len(plan.GenerationIDs) != 0 {
		t.Fatalf("post GC retention: %+v %v", plan, err)
	}
	// A non-active FAILED/CANCELLED row can be collected while its
	// terminal state remains unchanged until deletion, without a RETIRED hop.
	for _, state := range []string{"FAILED", "CANCELLED"} {
		exec(`UPDATE generations SET state=?,completed_at='2019-01-01T00:00:00Z' WHERE generation_id=?`, state, building)
		r.KeepGenerations = 1
		if _, err := store.CollectGeneration(ctx, r, building, true); err != nil {
			t.Fatalf("terminal %s: %v", state, err)
		}
		if state == "FAILED" {
			exec(`INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) SELECT ?,profile_id,'BUILDING',owner_job_id,policy_digest,policy_epoch,provider_fingerprint,'2019-01-01T00:00:00Z' FROM generations WHERE generation_id=?`, building, gcActive)
		}
	}
}
