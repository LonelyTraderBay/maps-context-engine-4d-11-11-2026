package contract

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// gcFixture demonstrates DB-03 ordering; production GC and locking belong to later tasks.
func gcFixture(db *sql.DB, failAfter int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stages := []string{
		`UPDATE generations SET state='RETIRED' WHERE generation_id='old'`,
		`DELETE FROM ui_build_bindings WHERE source_generation_id='old'`,
		`DELETE FROM entity_links WHERE generation_id='old'; DELETE FROM state_transitions WHERE generation_id='old'; DELETE FROM contract_fields WHERE generation_id='old'`,
		`DELETE FROM relations WHERE generation_id='old'`,
		`DELETE FROM module_members WHERE generation_id='old'`,
		`DELETE FROM search_documents WHERE generation_id='old'; DELETE FROM diagnostics WHERE generation_id='old'; DELETE FROM capability_results WHERE generation_id='old'`,
		`DELETE FROM symbols WHERE generation_id='old'`,
		`DELETE FROM domain_entities WHERE generation_id='old'`,
		`MODULES`,
		`DELETE FROM files WHERE generation_id='old'`,
		`DELETE FROM packages WHERE generation_id='old'`,
		`DELETE FROM generations WHERE generation_id='old'`,
		`DELETE FROM content_blobs WHERE NOT EXISTS (SELECT 1 FROM files WHERE files.content_hash=content_blobs.content_hash)`,
	}
	for i, q := range stages {
		if q == "MODULES" {
			for {
				var count int
				if err = tx.QueryRow(`SELECT count(*) FROM modules WHERE generation_id='old'`).Scan(&count); err != nil {
					return err
				}
				if count == 0 {
					break
				}
				result, e := tx.Exec(`DELETE FROM modules WHERE generation_id='old' AND module_id IN (SELECT m.module_id FROM modules m WHERE m.generation_id='old' AND NOT EXISTS (SELECT 1 FROM modules child WHERE child.generation_id=m.generation_id AND child.parent_module_id=m.module_id) ORDER BY m.module_id LIMIT 256)`)
				if e != nil {
					return e
				}
				n, e := result.RowsAffected()
				if e != nil {
					return e
				}
				if n == 0 {
					return fmt.Errorf("cycle")
				}
			}
		} else if _, err = tx.Exec(q); err != nil {
			return err
		}
		if i == failAfter {
			return fmt.Errorf("injected failure at %d", i)
		}
	}
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	if rows.Next() {
		rows.Close()
		return fmt.Errorf("foreign key violation")
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	return tx.Commit()
}
func seedGC(t *testing.T, db *sql.DB, cycle bool) {
	t.Helper()
	h := strings.Repeat("a", 64)
	p := strings.Repeat("p", 64)
	storageExec(t, db, `INSERT INTO content_blobs VALUES(?,x'61',1)`, h)
	for _, g := range []string{"old", "active"} {
		storageExec(t, db, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING','job',?,1,?,'now')`, g, p, h, h)
		storageExec(t, db, `INSERT INTO packages VALUES(?,'pkg','import','module','production',?,'complete_in_profile')`, g, h)
		storageExec(t, db, `INSERT INTO files(generation_id,path,content_hash,kind,status,byte_length,package_key) VALUES(?,'f',?,'go','indexed',1,'pkg')`, g, h)
		storageExec(t, db, `INSERT INTO search_documents(generation_id,item_id,path,kind,name,qualified_name,body) VALUES(?,'doc','f','symbol','n','n','oldphrase')`, g)
	}
	for _, pair := range [][2]string{{"root", ""}, {"middle", "root"}, {"leaf", "middle"}} {
		var parent any
		if pair[1] != "" {
			parent = pair[1]
		}
		storageExec(t, db, `INSERT INTO modules VALUES('old',?,?,'m','module','intended','f',0,1)`, pair[0], parent)
	}
	if cycle {
		storageExec(t, db, `UPDATE modules SET parent_module_id='leaf' WHERE generation_id='old' AND module_id='root'`)
	}
	storageExec(t, db, `INSERT INTO module_members VALUES('old','leaf','pkg')`)
	storageExec(t, db, `INSERT INTO domain_entities VALUES('old','entity','contract','key','intended','{}','[]')`)
	storageExec(t, db, `INSERT INTO symbols VALUES('old','symbol','f','pkg','function','n','n','scope',0,1,'',0)`)
	storageExec(t, db, `INSERT INTO relations VALUES('old','rel','symbol','symbol','ref','syntax_exact','f',0,1,'proof',NULL)`)
	storageExec(t, db, `INSERT INTO entity_links VALUES('old','link','symbol',NULL,NULL,'entity','ref','{}','exact_in_supported_scope')`)
	storageExec(t, db, `INSERT INTO contract_fields VALUES('old','entity','field','input','{}')`)
	storageExec(t, db, `INSERT INTO state_transitions VALUES('old','entity','transition','entity','entity',NULL,'[]','{}')`)
	storageExec(t, db, `INSERT INTO ui_build_maps VALUES(?,1,?,'v1',?,?,?,'test',?,'{}','now')`, h, p, h, h, h, h)
	storageExec(t, db, `INSERT INTO ui_source_anchors VALUES(?,?,'element_site','f',?,0,1,NULL)`, h, strings.Repeat("c", 64), h)
	storageExec(t, db, `INSERT INTO ui_build_bindings VALUES(?,?,?,'old',?,?,?,'now')`, strings.Repeat("b", 64), h, p, h, h, h)
	storageExec(t, db, `INSERT INTO ui_anchor_links VALUES(?,?,?,?,'{}','symbol','exact_in_supported_scope','{}')`, strings.Repeat("b", 64), h, "link", strings.Repeat("c", 64))
	storageExec(t, db, `UPDATE generations SET state='READY',snapshot_digest=?,manifest_json='{}',completed_at='now'`, h)
	storageExec(t, db, `INSERT INTO active_generations VALUES(?,'active')`, p)
}
func TestStorageGCTransactionalStages(t *testing.T) {
	db := storageDB(t, "index/001.sql", "index/002.sql", "index/003.sql", "index/004.sql")
	seedGC(t, db, false)
	for i := 0; i < 13; i++ {
		if err := gcFixture(db, i); err == nil {
			t.Fatalf("failure stage %d not injected", i)
		}
		if storageCount(t, db, `SELECT count(*) FROM generations WHERE generation_id='old' AND state='READY'`) != 1 || storageCount(t, db, `SELECT count(*) FROM modules WHERE generation_id='old'`) != 3 || storageCount(t, db, `SELECT count(*) FROM ui_build_bindings WHERE source_generation_id='old'`) != 1 {
			t.Fatalf("stage %d did not roll back", i)
		}
	}
	if err := gcFixture(db, -1); err != nil {
		t.Fatal(err)
	}
	if storageCount(t, db, `SELECT count(*) FROM generations WHERE generation_id='old'`) != 0 || storageCount(t, db, `SELECT count(*) FROM active_generations WHERE generation_id='active'`) != 1 || storageCount(t, db, `SELECT count(*) FROM content_blobs`) != 1 {
		t.Fatal("GC lost retained generation or shared blob")
	}
	if storageCount(t, db, `SELECT count(*) FROM search_fts JOIN search_documents d ON d.rowid=search_fts.rowid WHERE search_fts MATCH 'oldphrase' AND d.generation_id='active'`) != 1 {
		t.Fatal("retained search missing")
	}
}
func TestStorageGCCycleRollsBack(t *testing.T) {
	db := storageDB(t, "index/001.sql", "index/002.sql", "index/003.sql", "index/004.sql")
	seedGC(t, db, true)
	if err := gcFixture(db, -1); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle failure: %v", err)
	}
	if storageCount(t, db, `SELECT count(*) FROM generations WHERE generation_id='old' AND state='READY'`) != 1 || storageCount(t, db, `SELECT count(*) FROM modules WHERE generation_id='old'`) != 3 || storageCount(t, db, `SELECT count(*) FROM ui_build_bindings`) != 1 {
		t.Fatal("cycle did not roll back")
	}
}

// Cycle validation and report-only SQL are contract fixtures, not the product GC.
func TestStorageCyclePrecheckAndReportOnly(t *testing.T) {
	db := storageDB(t, "index/001.sql", "index/002.sql")
	h := strings.Repeat("a", 64)
	p := strings.Repeat("p", 64)
	storageExec(t, db, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES('g',?,'BUILDING','job',?,1,?,'now')`, p, h, h)
	storageExec(t, db, `INSERT INTO modules VALUES('g','root',NULL,'m','module','intended',NULL,NULL,NULL)`)
	storageExec(t, db, `INSERT INTO modules VALUES('g','leaf','root','m','module','intended',NULL,NULL,NULL)`)
	cycleQuery := `WITH RECURSIVE ancestors(start,id,parent) AS (SELECT module_id,module_id,parent_module_id FROM modules WHERE generation_id='g' UNION SELECT a.start,m.module_id,m.parent_module_id FROM ancestors a JOIN modules m ON m.generation_id='g' AND m.module_id=a.parent) SELECT EXISTS(SELECT 1 FROM ancestors WHERE start=parent)`
	if storageCount(t, db, cycleQuery) != 0 {
		t.Fatal("acyclic modules reported as cycle")
	}
	storageExec(t, db, `UPDATE modules SET parent_module_id='leaf' WHERE generation_id='g' AND module_id='root'`)
	if storageCount(t, db, cycleQuery) != 1 {
		t.Fatal("cycle not detected")
	}
	// A report is a read transaction and must not retire or delete anything.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(`SELECT count(*) FROM modules WHERE generation_id='g'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if count != 2 || storageCount(t, db, `SELECT count(*) FROM modules WHERE generation_id='g'`) != 2 || storageCount(t, db, `SELECT count(*) FROM generations WHERE generation_id='g' AND state='BUILDING'`) != 1 {
		t.Fatal("report mutated fixture")
	}
}
