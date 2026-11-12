package index

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// This fixture verifies the migrated adapter schema, not a public query or
// publication flow. All SQL and source strings are test-owned.
func TestMigratedSchemaEnforcesGenerationFKsAndFTS(t *testing.T) {
	db, _ := fixtureDB(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, db, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); err != nil {
		t.Fatal(err)
	}
	conn, err := borrowConfigured(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseConfigured(ctx, conn, false)
	hash := strings.Repeat("a", 64)
	for _, generation := range []string{"g1", "g2"} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING','fixture-job',?,1,?,'now')`, generation, hash, hash, hash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO content_blobs(content_hash,bytes,byte_length) VALUES(?,X'616263',3)`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO files(generation_id,path,content_hash,kind,status,byte_length) VALUES('g1','a.go',?,'source','indexed',3)`, hash); err != nil {
		t.Fatal(err)
	}
	insertSymbol := `INSERT INTO symbols(generation_id,symbol_id,path,kind,name,qualified_name,scope_key,start_byte,end_byte,signature,exported) VALUES(?,?,?,'function','Alpha','p.Alpha','p',0,3,'func Alpha()',1)`
	if _, err := conn.ExecContext(ctx, insertSymbol, "g1", "s1", "a.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, insertSymbol, "g2", "cross-generation", "a.go"); err == nil {
		t.Fatal("symbol borrowed a file from another generation")
	}
	insertRelation := `INSERT INTO relations(generation_id,relation_id,source_symbol_id,target_symbol_id,kind,evidence_kind,site_path,start_byte,end_byte,reason) VALUES('g2','r1','s1','s1','REFERS_TO','syntax_exact','a.go',0,3,'fixture')`
	if _, err := conn.ExecContext(ctx, insertRelation); err == nil {
		t.Fatal("relation borrowed endpoints from another generation")
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO search_documents(generation_id,item_id,path,kind,name,qualified_name,body) VALUES('g1','s1','a.go','symbol','Alpha','p.Alpha','fixture body')`); err != nil {
		t.Fatal(err)
	}
	var count int
	query := `SELECT count(*) FROM search_fts JOIN search_documents d ON d.rowid=search_fts.rowid WHERE search_fts MATCH ? AND d.generation_id=?`
	if err := conn.QueryRowContext(ctx, query, "Alpha", "g1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("FTS did not index fixture: %d %v", count, err)
	}
	if err := conn.QueryRowContext(ctx, query, "Alpha", "g2").Scan(&count); err != nil || count != 0 {
		t.Fatalf("FTS leaked across generation: %d %v", count, err)
	}
	rows, err := conn.QueryContext(ctx, `EXPLAIN QUERY PLAN `+query, "Alpha", "g1")
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := errorsCloseRows(rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(strings.Join(plan, " ")), "VIRTUAL TABLE INDEX") {
		t.Fatalf("FTS plan did not use virtual table index: %v", plan)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM search_documents WHERE generation_id='g1' AND item_id='s1'`); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, query, "Alpha", "g1").Scan(&count); err != nil || count != 0 {
		t.Fatalf("FTS delete trigger left row: %d %v", count, err)
	}
}

func errorsCloseRows(rows *sql.Rows) error {
	iteration := rows.Err()
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close fixture rows: %w", err)
	}
	return iteration
}
