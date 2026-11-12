package contract

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func storageDB(t *testing.T, migrations ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "fixture.sqlite"))+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, name := range migrations {
		b, err := os.ReadFile(filepath.Join("..", "..", "internal", "store", "sqlite", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	return db
}
func storageExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
func storageReject(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err == nil {
		t.Fatalf("unexpected success: %s", q)
	}
}
func storageCount(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestStorageMigrationSequence(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		base, addon               []string
		baseVersion, addonVersion int
		addonTable                string
	}{
		{"catalog", []string{"catalog/001.sql"}, nil, 1, 0, ""},
		{"index", []string{"index/001.sql", "index/002.sql"}, []string{"index/003.sql", "index/004.sql"}, 2, 4, "ui_build_maps"},
		{"records", []string{"records/001.sql"}, []string{"records/002.sql"}, 1, 2, "evidence_artifacts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := storageDB(t, tc.base...)
			if got := storageCount(t, db, "SELECT schema_version FROM schema_meta"); got != tc.baseVersion {
				t.Fatalf("base schema version %d", got)
			}
			if tc.addonTable != "" && storageCount(t, db, "SELECT count(*) FROM sqlite_master WHERE name=?", tc.addonTable) != 0 {
				t.Fatal("addon already present in base")
			}
			for _, name := range tc.addon {
				b, err := os.ReadFile(filepath.Join("..", "..", "internal", "store", "sqlite", "migrations", name))
				if err != nil {
					t.Fatal(err)
				}
				storageExec(t, db, string(b))
			}
			if tc.addonVersion != 0 && storageCount(t, db, "SELECT schema_version FROM schema_meta") != tc.addonVersion {
				t.Fatal("addon schema version")
			}
			rows, err := db.Query("PRAGMA foreign_key_check")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if rows.Next() {
				t.Fatal("foreign key violations")
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			var check string
			if err = db.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
				t.Fatalf("integrity: %s %v", check, err)
			}
		})
	}
}
func TestStoragePooledConnectionPragmas(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "pool.sqlite"))
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	ctx := context.Background()
	var connections []*sql.Conn
	for i := 0; i < 4; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
		defer c.Close()
		for _, p := range []struct{ name, want string }{{"foreign_keys", "1"}, {"journal_mode", "wal"}, {"synchronous", "2"}, {"busy_timeout", "5000"}} {
			var v string
			if err = c.QueryRowContext(ctx, "PRAGMA "+p.name).Scan(&v); err != nil || !strings.EqualFold(v, p.want) {
				t.Fatalf("conn %d %s=%q: %v", i, p.name, v, err)
			}
		}
	}
	if len(connections) != 4 {
		t.Fatal("expected four simultaneous pooled connections")
	}
}
func TestStoragePublicationAndFTS(t *testing.T) {
	db := storageDB(t, "index/001.sql", "index/002.sql")
	h := strings.Repeat("a", 64)
	p := strings.Repeat("p", 64)
	for _, g := range []string{"old", "new"} {
		storageExec(t, db, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING','job',?,1,?,'now')`, g, p, h, h)
	}
	storageReject(t, db, `INSERT INTO active_generations VALUES(?,?)`, p, "new")
	for _, g := range []string{"old", "new"} {
		storageExec(t, db, `INSERT INTO search_documents(generation_id,item_id,path,kind,name,qualified_name,body) VALUES(?,?,'x','symbol','x','x',?)`, g, g, "unique"+g)
	}
	storageExec(t, db, `UPDATE generations SET state='READY',snapshot_digest=?,manifest_json='{}',completed_at='now' WHERE generation_id='old'`, h)
	storageExec(t, db, `INSERT INTO active_generations VALUES(?,?)`, p, "old")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	res, err := tx.Exec(`UPDATE generations SET state='READY',snapshot_digest=?,manifest_json='{}',completed_at='now' WHERE generation_id='new' AND state='BUILDING' AND owner_job_id=?`, h, "wrong")
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 0 {
		t.Fatalf("wrong-owner CAS rows %d: %v", n, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	res, err = tx.Exec(`UPDATE generations SET state='READY',snapshot_digest=?,manifest_json='{}',completed_at='now' WHERE generation_id='new' AND state='BUILDING' AND owner_job_id=?`, h, "job")
	if err != nil {
		t.Fatal(err)
	}
	n, err = res.RowsAffected()
	if err != nil || n != 1 {
		t.Fatalf("CAS rows %d: %v", n, err)
	}
	if _, err = tx.Exec(`UPDATE active_generations SET generation_id='new' WHERE profile_id=?`, p); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if storageCount(t, db, `SELECT count(*) FROM active_generations WHERE generation_id='old'`) != 1 {
		t.Fatal("rollback lost active pointer")
	}
	storageReject(t, db, `UPDATE generations SET state='RETIRED' WHERE generation_id='old'`)
	storageReject(t, db, `INSERT INTO search_documents(generation_id,item_id,path,kind,name,qualified_name,body) VALUES('old','late','x','symbol','x','x','x')`)
	query := `SELECT count(*) FROM search_fts JOIN search_documents d ON d.rowid=search_fts.rowid WHERE search_fts MATCH ? AND d.generation_id=?`
	if storageCount(t, db, query, "uniqueold", "old") != 1 || storageCount(t, db, query, "uniqueold", "new") != 0 {
		t.Fatal("FTS isolation")
	}
	storageExec(t, db, `UPDATE search_documents SET body='replacement' WHERE generation_id='new'`)
	if storageCount(t, db, query, "uniquenew", "new") != 0 || storageCount(t, db, query, "replacement", "new") != 1 {
		t.Fatal("FTS update trigger")
	}
	storageExec(t, db, `DELETE FROM search_documents WHERE generation_id='new'`)
	if storageCount(t, db, query, "replacement", "new") != 0 {
		t.Fatal("FTS delete trigger")
	}
}

func TestStorageCompositeFKAndResolverBindings(t *testing.T) {
	db := storageDB(t, "index/001.sql", "index/002.sql", "index/003.sql", "index/004.sql")
	h := strings.Repeat("a", 64)
	p := strings.Repeat("p", 64)
	for _, g := range []string{"g1", "g2"} {
		storageExec(t, db, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING','job',?,1,?,'now')`, g, p, h, h)
	}
	storageExec(t, db, `INSERT INTO symbols(generation_id,symbol_id,kind,name,qualified_name,scope_key,signature,exported) VALUES('g1','only-g1','external','s','s','scope','',0)`)
	storageExec(t, db, `INSERT INTO files(generation_id,path,kind,status,reason_code) VALUES('g2','file','go','missing','missing')`)
	storageReject(t, db, `INSERT INTO relations(generation_id,relation_id,source_symbol_id,kind,evidence_kind,site_path,start_byte,end_byte,reason,unresolved_label) VALUES('g2','r','only-g1','ref','unresolved','file',0,0,'missing','unknown')`)
	storageExec(t, db, `INSERT INTO ui_build_maps VALUES(?,1,?,'v1',?,?,?,'test',?,'{}','now')`, h, p, h, h, h, h)
	insert := `INSERT INTO ui_build_bindings VALUES(?,?,?,?,?,?,?,'now')`
	storageExec(t, db, insert, strings.Repeat("1", 64), h, p, "g1", h, strings.Repeat("2", 64), h)
	storageExec(t, db, insert, strings.Repeat("3", 64), h, p, "g1", h, strings.Repeat("4", 64), h)
	storageReject(t, db, insert, strings.Repeat("5", 64), h, p, "g1", h, strings.Repeat("2", 64), h)
	storageReject(t, db, `UPDATE ui_build_bindings SET evidence_digest=? WHERE binding_id=?`, strings.Repeat("6", 64), strings.Repeat("1", 64))
	if storageCount(t, db, `SELECT count(*) FROM ui_build_bindings`) != 2 {
		t.Fatal("resolver bindings lost")
	}
}

func TestStorageRecordAndPermissionImmutability(t *testing.T) {
	db := storageDB(t, "records/001.sql")
	h := strings.Repeat("a", 64)
	storageExec(t, db, `INSERT INTO records_meta VALUES(1,'worktree','11111111-1111-1111-1111-111111111111',0)`)
	storageExec(t, db, `INSERT INTO record_versions VALUES('r',1,'decision','proposed','{}','[]','actor','reason',?,'now')`, h)
	storageExec(t, db, `INSERT INTO record_heads VALUES('r',1)`)
	storageReject(t, db, `UPDATE record_versions SET payload_json='{"changed":true}' WHERE record_id='r'`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO record_versions VALUES('r',2,'decision','approved','{}','[]','actor','reason',?,'now')`, h); err != nil {
		t.Fatal(err)
	}
	result, err := tx.Exec(`UPDATE record_heads SET version=2 WHERE record_id='r' AND version=1`)
	if err != nil {
		t.Fatal(err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		t.Fatalf("head CAS %d %v", n, err)
	}
	if _, err = tx.Exec(`UPDATE records_meta SET record_epoch=record_epoch+1`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO mutation_receipts VALUES('mutation','actor',?,'r',2,'now')`, h); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if storageCount(t, db, `SELECT version FROM record_heads WHERE record_id='r'`) != 1 || storageCount(t, db, `SELECT record_epoch FROM records_meta`) != 0 || storageCount(t, db, `SELECT count(*) FROM mutation_receipts`) != 0 || storageCount(t, db, `SELECT count(*) FROM record_versions`) != 1 {
		t.Fatal("record transaction did not rollback")
	}
	catalog := storageDB(t, "catalog/001.sql")
	storageExec(t, catalog, `INSERT INTO preview_permission_versions VALUES('profile',1,'approved','{}',?,'actor','reason','now')`, h)
	storageExec(t, catalog, `INSERT INTO preview_permission_heads VALUES('profile',1)`)
	storageReject(t, catalog, `UPDATE preview_permission_versions SET declared_status='revoked' WHERE profile_id='profile'`)
	storageReject(t, catalog, `UPDATE preview_permission_heads SET version=2 WHERE profile_id='profile'`)
}

func TestStorageRecordCASAndReceiptConstraints(t *testing.T) {
	db := storageDB(t, "records/001.sql")
	h := strings.Repeat("a", 64)
	storageExec(t, db, `INSERT INTO records_meta VALUES(1,'w','11111111-1111-1111-1111-111111111111',0)`)
	storageExec(t, db, `INSERT INTO record_versions VALUES('r',1,'decision','proposed','{}','[]','actor','reason',?,'now')`, h)
	storageExec(t, db, `INSERT INTO record_heads VALUES('r',1)`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO record_versions VALUES('r',2,'decision','approved','{}','[]','actor','reason',?,'now')`, h); err != nil {
		t.Fatal(err)
	}
	res, err := tx.Exec(`UPDATE record_heads SET version=2 WHERE record_id='r' AND version=1`)
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		t.Fatalf("first CAS %d %v", n, err)
	}
	if _, err = tx.Exec(`UPDATE records_meta SET record_epoch=record_epoch+1`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO mutation_receipts VALUES('m','actor',?,'r',2,'now')`, h); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	res, err = db.Exec(`UPDATE record_heads SET version=2 WHERE record_id='r' AND version=1`)
	if err != nil {
		t.Fatal(err)
	}
	n, err = res.RowsAffected()
	if err != nil || n != 0 {
		t.Fatalf("stale CAS %d %v", n, err)
	}
	storageReject(t, db, `INSERT INTO mutation_receipts VALUES('m','other',?,'r',2,'now')`, h)
	var actor, request string
	var version int
	if err = db.QueryRow(`SELECT principal_id,request_hash,result_version FROM mutation_receipts WHERE client_mutation_id='m'`).Scan(&actor, &request, &version); err != nil {
		t.Fatal(err)
	}
	if actor != "actor" || request != h || version != 2 || storageCount(t, db, `SELECT record_epoch FROM records_meta`) != 1 || storageCount(t, db, `SELECT count(*) FROM record_versions`) != 2 {
		t.Fatal("receipt or version state changed")
	}
}
func TestStorageIncarnationRequiredAndTransactionStable(t *testing.T) {
	for _, tc := range []struct {
		migration, insert, query string
		args                     []any
	}{
		{"catalog/001.sql", `INSERT INTO catalog_identity(singleton) VALUES(1)`, `SELECT incarnation FROM catalog_identity`, nil},
		{"index/001.sql", `INSERT INTO store_identity(singleton,worktree_id) VALUES(1,'w')`, `SELECT incarnation FROM store_identity`, nil},
		{"records/001.sql", `INSERT INTO records_meta(singleton,worktree_id) VALUES(1,'w')`, `SELECT incarnation FROM records_meta`, nil},
	} {
		t.Run(tc.migration, func(t *testing.T) {
			db := storageDB(t, tc.migration)
			storageReject(t, db, tc.insert, tc.args...)
			id := "11111111-1111-1111-1111-111111111111"
			switch tc.migration {
			case "catalog/001.sql":
				storageExec(t, db, `INSERT INTO catalog_identity VALUES(1,?)`, id)
			case "index/001.sql":
				storageExec(t, db, `INSERT INTO store_identity VALUES(1,'w',?)`, id)
			case "records/001.sql":
				storageExec(t, db, `INSERT INTO records_meta VALUES(1,'w',?,0)`, id)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`UPDATE schema_meta SET schema_version=schema_version WHERE singleton=1`); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var got string
			if err = db.QueryRow(tc.query).Scan(&got); err != nil || got != id {
				t.Fatalf("incarnation %q: %v", got, err)
			}
		})
	}
}

func TestStorageReaderQueryOnly(t *testing.T) {
	db := storageDB(t, "catalog/001.sql")
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), `PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = conn.QueryRowContext(context.Background(), `SELECT schema_version FROM schema_meta`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("read %d %v", version, err)
	}
	if _, err = conn.ExecContext(context.Background(), `CREATE TABLE reader_must_not_write (id INTEGER)`); err == nil {
		t.Fatal("query_only accepted write")
	}
}
func TestStorageSuccessfulPublishAndProfileGuard(t *testing.T) {
	db := storageDB(t, "index/001.sql", "index/002.sql")
	h := strings.Repeat("a", 64)
	p := strings.Repeat("p", 64)
	other := strings.Repeat("q", 64)
	storageExec(t, db, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES('g',?,'BUILDING','job',?,1,?,'now')`, p, h, h)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	result, err := tx.Exec(`UPDATE generations SET state='READY_PARTIAL',snapshot_digest=?,manifest_json='{}',completed_at='now' WHERE generation_id='g' AND state='BUILDING' AND owner_job_id='job'`, h)
	if err != nil {
		t.Fatal(err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		t.Fatalf("publish CAS %d %v", n, err)
	}
	if _, err = tx.Exec(`INSERT INTO active_generations VALUES(?,'g')`, p); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if storageCount(t, db, `SELECT count(*) FROM active_generations a JOIN generations g ON g.generation_id=a.generation_id WHERE a.profile_id=? AND g.state='READY_PARTIAL'`, p) != 1 {
		t.Fatal("publish did not commit")
	}
	storageReject(t, db, `UPDATE active_generations SET profile_id=? WHERE generation_id='g'`, other)
	storageReject(t, db, `DELETE FROM generations WHERE generation_id='g'`)
}

func TestStorageConcurrentExpectedVersion(t *testing.T) {
	db := storageDB(t, "records/001.sql")
	db.SetMaxOpenConns(2)
	h := strings.Repeat("a", 64)
	storageExec(t, db, `INSERT INTO records_meta VALUES(1,'w','11111111-1111-1111-1111-111111111111',0)`)
	storageExec(t, db, `INSERT INTO record_versions VALUES('r',1,'decision','proposed','{}','[]','actor','reason',?,'now')`, h)
	storageExec(t, db, `INSERT INTO record_heads VALUES('r',1)`)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, mutation := range []string{"m1", "m2"} {
		go func(mutation string) {
			<-start
			tx, err := db.Begin()
			if err != nil {
				results <- err
				return
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`INSERT INTO record_versions VALUES('r',2,'decision','approved','{}','[]','actor','reason',?,'now')`, h); err != nil {
				results <- err
				return
			}
			res, err := tx.Exec(`UPDATE record_heads SET version=2 WHERE record_id='r' AND version=1`)
			if err != nil {
				results <- err
				return
			}
			n, err := res.RowsAffected()
			if err != nil || n != 1 {
				results <- fmt.Errorf("stale head CAS: %d %v", n, err)
				return
			}
			if _, err = tx.Exec(`UPDATE records_meta SET record_epoch=record_epoch+1`); err != nil {
				results <- err
				return
			}
			if _, err = tx.Exec(`INSERT INTO mutation_receipts VALUES(?,'actor',?,'r',2,'now')`, mutation, h); err != nil {
				results <- err
				return
			}
			results <- tx.Commit()
		}(mutation)
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			success++
		}
	}
	if success != 1 || storageCount(t, db, `SELECT version FROM record_heads WHERE record_id='r'`) != 2 || storageCount(t, db, `SELECT record_epoch FROM records_meta`) != 1 || storageCount(t, db, `SELECT count(*) FROM record_versions`) != 2 || storageCount(t, db, `SELECT count(*) FROM mutation_receipts`) != 1 {
		t.Fatal("concurrent expected version violated")
	}
}

func TestStorageMapBindingImmutableKeys(t *testing.T) {
	db := storageDB(t, "index/001.sql", "index/002.sql", "index/003.sql", "index/004.sql")
	h := strings.Repeat("a", 64)
	p := strings.Repeat("p", 64)
	bid := strings.Repeat("b", 64)
	anchor := strings.Repeat("c", 64)
	storageExec(t, db, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES('g',?,'BUILDING','job',?,1,?,'now')`, p, h, h)
	storageExec(t, db, `INSERT INTO ui_build_maps VALUES(?,1,?,'v1',?,?,?,'test',?,'{}','now')`, h, p, h, h, h, h)
	storageExec(t, db, `INSERT INTO ui_source_anchors VALUES(?,?,'element_site','f',?,0,1,NULL)`, h, anchor, h)
	storageExec(t, db, `INSERT INTO ui_build_bindings VALUES(?,?,?,'g',?,?,?,'now')`, bid, h, p, h, h, h)
	storageReject(t, db, `INSERT INTO ui_build_maps VALUES(?,1,?,'v2',?,?,?,'test',?,'{}','now')`, h, p, h, h, h, h)
	storageReject(t, db, `UPDATE ui_build_maps SET metadata_json='{"changed":true}' WHERE build_map_id=?`, h)
	storageReject(t, db, `UPDATE ui_source_anchors SET end_byte=2 WHERE build_map_id=? AND anchor_id=?`, h, anchor)
	storageReject(t, db, `INSERT INTO ui_source_anchors VALUES(?,?,'element_site','other',?,0,1,NULL)`, h, strings.Repeat("d", 64), h)
	storageReject(t, db, `INSERT INTO ui_build_bindings VALUES(?,?,?,'g',?,?,?,'now')`, bid, h, p, h, h, strings.Repeat("d", 64))
	if storageCount(t, db, `SELECT count(*) FROM ui_build_maps WHERE build_map_id=? AND provider_version='v1'`, h) != 1 || storageCount(t, db, `SELECT count(*) FROM ui_source_anchors WHERE build_map_id=? AND end_byte=1`, h) != 1 || storageCount(t, db, `SELECT count(*) FROM ui_build_bindings WHERE binding_id=? AND evidence_digest=?`, bid, h) != 1 {
		t.Fatal("immutable metadata changed")
	}
}
