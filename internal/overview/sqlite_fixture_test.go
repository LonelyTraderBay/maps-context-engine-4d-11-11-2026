package overview

import (
	"database/sql"
	"reflect"
	"testing"

	migrations "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/index"
	_ "modernc.org/sqlite"
)

// This disposable in-memory database checks the real frozen DDL against the
// projection. It does not exercise production publication or query adapters.
func TestOverviewAgainstSQLiteSchemaFixture(t *testing.T) {
	ctx, input := overviewFixture(t, 3001)
	report, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, migrations.V1()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, migrations.V2()); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	manifest := input.Snapshot.Manifest()
	_, err = tx.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING','fixture',?,1,?,'2026-11-12T00:00:00Z')`, generation, manifest.ProfileID, manifest.PolicyDigest, manifest.ProviderDigest)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range input.Packages {
		p := pkg.Fact
		if _, err = tx.ExecContext(ctx, `INSERT INTO packages(generation_id,package_key,import_path,module_key,variant,input_digest,semantic_status) VALUES(?,?,?,?,?,?,?)`, generation, p.PackageKey, p.ImportPath, p.ModuleKey, p.Variant, p.InputDigest, p.SemanticStatus); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range manifest.Files {
		file, ok := input.Snapshot.File(entry.Path)
		if !ok {
			t.Fatal("fixture missing capture")
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO content_blobs(content_hash,bytes,byte_length) VALUES(?,?,?)`, file.ContentHash(), file.Bytes(), file.Len()); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO files(generation_id,path,content_hash,kind,status,byte_length) VALUES(?,?,?,'go','indexed',?)`, generation, entry.Path, file.ContentHash(), file.Len()); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range summary.Modules {
		var parent any
		if m.ParentID != "" {
			parent = m.ParentID
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO modules(generation_id,module_id,parent_module_id,label,kind,plane,source_path,start_byte,end_byte) VALUES(?,?,?,?,'module',?,?,?,?)`, generation, m.Ref.ModuleID, parent, m.ImportPath, m.Plane, m.Source.Path, m.Source.Range.StartByte, m.Source.Range.EndByte); err != nil {
			t.Fatal(err)
		}
		for _, p := range m.Packages {
			if _, err = tx.ExecContext(ctx, `INSERT INTO module_members(generation_id,module_id,package_key) VALUES(?,?,?)`, generation, m.Ref.ModuleID, p.Fact.PackageKey); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range input.Symbols {
		if _, err = tx.ExecContext(ctx, `INSERT INTO symbols(generation_id,symbol_id,path,package_key,kind,name,qualified_name,scope_key,start_byte,end_byte,signature,exported) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, generation, s.SymbolID, *s.Path, *s.PackageKey, s.Kind, s.Name, s.QualifiedName, s.ScopeKey, s.Range.StartByte, s.Range.EndByte, s.Signature, s.Exported); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range input.Relations {
		if _, err = tx.ExecContext(ctx, `INSERT INTO relations(generation_id,relation_id,source_symbol_id,target_symbol_id,kind,evidence_kind,site_path,start_byte,end_byte,reason,unresolved_label) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, generation, r.RelationID, r.SourceSymbolID, *r.TargetSymbolID, r.Kind, r.EvidenceKind, r.SitePath, r.Range.StartByte, r.Range.EndByte, r.Reason, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE generations SET state='READY_PARTIAL',snapshot_digest=?,manifest_json='{}',completed_at='2026-11-12T00:00:01Z' WHERE generation_id=?`, manifest.SnapshotDigest, generation); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE modules SET label='changed' WHERE generation_id=?`, generation); err == nil {
		t.Fatal("DDL allowed ready generation module mutation")
	}
	query := `SELECT r.relation_id FROM relations r JOIN symbols s ON s.generation_id=r.generation_id AND s.symbol_id=r.source_symbol_id JOIN packages sp ON sp.generation_id=s.generation_id AND sp.package_key=s.package_key JOIN symbols d ON d.generation_id=r.generation_id AND d.symbol_id=r.target_symbol_id JOIN packages dp ON dp.generation_id=d.generation_id AND dp.package_key=d.package_key WHERE r.generation_id=? AND sp.module_key=? AND dp.module_key=? AND r.kind=? AND r.evidence_kind=? ORDER BY r.relation_id`
	for _, group := range summary.Groups {
		rows, err := db.QueryContext(ctx, query, generation, group.SourceModuleID, group.TargetModuleID, group.Kind, group.EvidenceKind)
		if err != nil {
			t.Fatal(err)
		}
		var expected []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			expected = append(expected, id)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err = rows.Close(); err != nil {
			t.Fatal(err)
		}
		var got []string
		after := ""
		for {
			page, err := report.Expand(ctx, group.Expand, after, 37)
			if err != nil {
				t.Fatal(err)
			}
			for _, site := range page.Sites {
				got = append(got, site.Relation.RelationID)
			}
			if page.NextAfter == "" {
				break
			}
			after = page.NextAfter
		}
		if len(expected) != group.Count || !reflect.DeepEqual(got, expected) {
			t.Fatalf("SQLite and expansion disagree: got %d expected %d", len(got), len(expected))
		}
	}
}
