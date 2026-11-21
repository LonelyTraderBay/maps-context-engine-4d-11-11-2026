package index

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestV3DomainMigrationTransaction(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "index.sqlite"))+
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	reject := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.Exec(statement, args...); err == nil {
			t.Fatal("accepted invalid domain write")
		}
	}
	count := func(statement string) int {
		t.Helper()
		var got int
		if err := db.QueryRow(statement).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	exec(V1())
	exec(V2())
	hash := strings.Repeat("a", 64)
	for _, gen := range []string{"g1", "g2"} {
		exec(`INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at)
			VALUES(?,?,'BUILDING','job',?,1,?,'now')`, gen, hash, hash, hash)
	}
	exec(`INSERT INTO symbols(generation_id,symbol_id,kind,name,qualified_name,scope_key,signature,exported) VALUES
		('g1','from','external','from','from','scope','',0),('g2','from','external','from','from','scope','',0)`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(V3()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if count("SELECT schema_version FROM schema_meta") != 2 ||
		count("SELECT count(*) FROM sqlite_master WHERE name='domain_entities'") != 0 {
		t.Fatal("rollback left addon schema active")
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(V3()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if count("SELECT schema_version FROM schema_meta") != 3 ||
		count("SELECT min_reader_version FROM schema_meta") != 3 ||
		count("SELECT count(*) FROM symbols WHERE symbol_id='from'") != 2 {
		t.Fatal("migration lost the reader fence or prior generation data")
	}
	entity := `INSERT INTO domain_entities VALUES(?,?,'contract','key','intended','{}','[]')`
	exec(entity, "g1", "to")
	link := `INSERT INTO entity_links VALUES(?,?,?,?,?,?,'uses','{}','exact_in_supported_scope')`
	exec(link, "g1", "good", "from", nil, nil, "to")
	reject(link, "g1", "both", "from", "to", nil, "to")
	reject(link, "g1", "neither", nil, nil, nil, "to")
	reject(link, "g2", "other", "from", nil, nil, "to")
	field := `INSERT INTO contract_fields VALUES(?,'to','amount',?,'{}')`
	exec(field, "g1", "input")
	exec(field, "g1", "output")
	reject(field, "g1", "input")
	reject(field, "g2", "input")
	exec(`UPDATE generations SET state='READY',snapshot_digest=?,manifest_json='{}',completed_at='now' WHERE generation_id='g1'`, hash)
	reject(`UPDATE domain_entities SET qualified_key='replacement' WHERE generation_id='g1'`)
	reject(`DELETE FROM entity_links WHERE generation_id='g1'`)
	reject(field, "g1", "stored")
	if count("SELECT count(*) FROM pragma_foreign_key_check") != 0 {
		t.Fatal("foreign keys failed")
	}
}
