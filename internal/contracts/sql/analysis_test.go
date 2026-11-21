package sql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func fixtureProfile() Profile {
	return Profile{Dialect: "mysql", Version: "8.4", Engine: "InnoDB", SQLMode: "default",
		Environment: "fixture", Namespace: "local.database", Schema: "app"}
}

func sqlCapture(path, kind, input string) Source {
	body := []byte(input)
	hash := sha256.Sum256(body)
	return Source{Kind: kind, Data: body, Ref: model.SourceRef{
		WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("b", 64),
		GenerationID: "22222222-2222-4222-8222-222222222222", Path: path, ContentHash: hex.EncodeToString(hash[:]),
		Range: model.ByteRange{EndByte: int64(len(body))}}}
}

func TestMySQLCapturedLineageKeepsObjectKindsAndSourceRanges(t *testing.T) {
	schema := sqlCapture("db/schema.sql", "schema", `
CREATE TABLE app.customer (id BIGINT NOT NULL, score DECIMAL(12,2) DEFAULT NULL) ENGINE=InnoDB;
CREATE TABLE app.audit (customer_id BIGINT) ENGINE=NDBCLUSTER;
CREATE TABLE app.orders (customer_id BIGINT, CONSTRAINT orders_customer FOREIGN KEY (customer_id)
 REFERENCES app.customer(id)) ENGINE=InnoDB;
`)
	migration := sqlCapture("db/001.sql", "migration", `
ALTER TABLE app.customer ADD COLUMN `+"\x60nhãn\x60"+` VARCHAR(32) NULL;
CREATE VIEW app.customer_view AS SELECT c.id FROM app.customer AS c JOIN app.orders o ON o.customer_id = c.id;
DELIMITER $$
CREATE PROCEDURE app.refresh_customer() BEGIN
  INSERT INTO app.audit(customer_id) SELECT id FROM app.customer;
END$$
CREATE TRIGGER app.log_customer AFTER UPDATE ON app.customer FOR EACH ROW
  INSERT INTO app.audit(customer_id) VALUES (NEW.id)$$
CREATE EVENT app.refresh_daily ON SCHEDULE EVERY 1 DAY DO CALL app.refresh_customer()$$
DELIMITER ;
`)
	profile := fixtureProfile()
	got, err := AnalyzeCaptured(context.Background(), profile, []Source{schema, migration})
	if err != nil {
		t.Fatal(err)
	}
	if got.Coverage.Status != "supported_scope_complete" || len(got.Coverage.Gaps) != 0 ||
		len(got.Batch.Entities) != 9 || len(got.Batch.Fields) != 5 {
		t.Fatalf("analysis: entities=%d fields=%d gaps=%+v", len(got.Batch.Entities), len(got.Batch.Fields), got.Coverage.Gaps)
	}
	kinds := map[string]string{}
	files := map[string][]byte{schema.Ref.Path: schema.Data, migration.Ref.Path: migration.Data}
	for _, item := range got.Batch.Entities {
		kinds[item.Ref.EntityID] = item.Kind
		if item.Declaration.ValidateCapturedBytes(files[item.Declaration.Path]) != nil || item.Plane != "intended" {
			t.Fatalf("invalid declaration: %+v", item)
		}
		if strings.Contains(item.Key.Identifier, "`audit`") && item.Key.Namespace != "local.database.ndb" {
			t.Fatalf("explicit NDB engine lost: %+v", item)
		}
		if item.Kind == "table" && strings.Contains(item.Key.Identifier, "`customer`") && item.Key.Namespace != "local.database.innodb" {
			t.Fatalf("explicit InnoDB engine lost: %+v", item)
		}
	}
	var gotLinks []string
	for _, link := range got.Batch.Links {
		source := link.Source.(*model.EntityRef)
		target := link.Target.(*model.EntityRef)
		ref := link.Evidence.SourceRefs[0]
		if link.ResolutionStatus != "exact_in_supported_scope" || ref.ValidateCapturedBytes(files[ref.Path]) != nil {
			t.Fatalf("invalid evidence: %+v", link)
		}
		if link.Kind != "declares" {
			raw := files[ref.Path][ref.Range.StartByte:ref.Range.EndByte]
			gotLinks = append(gotLinks, kinds[source.EntityID]+"/"+link.Kind+"/"+kinds[target.EntityID]+"/"+string(raw))
		}
	}
	want := []string{
		"event/calls/procedure/app.refresh_customer",
		"migration_change/changes/table/app.customer",
		"procedure/reads/table/app.customer",
		"procedure/writes/table/app.audit",
		"table/references/table/app.customer",
		"trigger/triggers_on/table/app.customer",
		"trigger/writes/table/app.audit",
		"view/reads/table/app.customer",
		"view/reads/table/app.orders",
	}
	slices.Sort(gotLinks)
	if !reflect.DeepEqual(gotLinks, want) {
		t.Fatalf("lineage \ngot  %v\nwant %v", gotLinks, want)
	}
	for _, field := range got.Batch.Fields {
		ref := field.Declaration
		if ref.ValidateCapturedBytes(files[ref.Path]) != nil {
			t.Fatalf("field source: %+v", field)
		}
		switch field.FieldPath {
		case "score":
			if field.Type != "DECIMAL(12,2)" || field.Precision == nil || *field.Precision != 12 || field.Nullable != nil {
				t.Fatalf("declared precision or default treated as nullability: %+v", field)
			}
		case "nhãn":
			if string(files[ref.Path][ref.Range.StartByte:ref.Range.EndByte]) != "`nhãn` VARCHAR(32) NULL" ||
				field.Nullable == nil || !*field.Nullable {
				t.Fatalf("UTF-8 or nullability: %+v", field)
			}
		}
	}
	reordered, err := AnalyzeCaptured(context.Background(), profile, []Source{migration, schema})
	if err != nil || !reflect.DeepEqual(reordered, got) {
		t.Fatalf("map changed with input order: %v", err)
	}
}

func TestMySQLUnresolvedSyntaxDoesNotInventTargets(t *testing.T) {
	source := sqlCapture("db/mixed.sql", "schema", `
CREATE TABLE app.customer (id INT) ENGINE=InnoDB;
CREATE TABLE app.customer (id INT) ENGINE=NDBCLUSTER;
CREATE TABLE app.audit (id INT) ENGINE=NDBCLUSTER;
CREATE TABLE app.reference_example (id INT, FOREIGN KEY (id) REFERENCES app.audit(id)) ENGINE=InnoDB;
PREPARE sample FROM @statement;
SELECT id FROM app.customer;
SELECT id FROM app.missing;
SELECT 'a; b' FROM app.audit;
/*!80400 CREATE TABLE app.hidden (id INT) */;
`)
	got, err := AnalyzeCaptured(context.Background(), fixtureProfile(), []Source{source})
	if err != nil || got.Coverage.Status != "partial" {
		t.Fatalf("analysis coverage: %+v %v", got.Coverage, err)
	}
	codes := map[string]int{}
	for _, gap := range got.Coverage.Gaps {
		codes[gap.Code]++
		if len(gap.SourceRefs) != 1 || gap.SourceRefs[0].ValidateCapturedBytes(source.Data) != nil {
			t.Fatalf("gap has no source: %+v", gap)
		}
	}
	for _, code := range []string{"SQL_DYNAMIC_STATEMENT", "SQL_EXECUTABLE_COMMENT", "SQL_RELATED_OBJECT_AMBIGUOUS",
		"SQL_RELATED_OBJECT_UNDECLARED", "SQL_REFERENCE_ENGINE_UNRESOLVED"} {
		if codes[code] != 1 {
			t.Fatalf("expected gap %s: %v", code, codes)
		}
	}
	if len(got.Batch.Links) != 1 || got.Batch.Links[0].Kind != "reads" {
		t.Fatalf("ambiguous or dynamic SQL created links: %+v", got.Batch.Links)
	}
	for _, item := range got.Batch.Entities {
		if strings.Contains(item.Key.Identifier, "hidden") {
			t.Fatal("version-dependent comment yielded a declaration")
		}
	}
}

func TestMySQLStopsAtUnknownSessionMode(t *testing.T) {
	first := sqlCapture("db/stale-mode.sql", "migration", `
CREATE TABLE app.before_mode (id INT);
SET SESSION sql_mode = 'ANSI_QUOTES';
CREATE TABLE app.possible (id INT);
`)
	versioned := sqlCapture("db/versioned.sql", "schema", `
/*!80400 SET sql_mode = 'ANSI_QUOTES' */;
CREATE TABLE app.after_version_comment (id INT);
`)
	unbalanced := sqlCapture("db/malformed.sql", "schema", "SELECT id FROM app.before_mode WHERE (id = 1;")
	result, err := AnalyzeCaptured(context.Background(), fixtureProfile(), []Source{first, versioned, unbalanced})
	if err != nil || result.Coverage.Status != "partial" || len(result.Batch.Entities) != 2 {
		t.Fatalf("unexpected partial analysis: %+v %v", result, err)
	}
	counts := map[string]int{}
	for _, gap := range result.Coverage.Gaps {
		counts[gap.Code]++
	}
	if counts["SQL_SESSION_STATE_UNRESOLVED"] != 2 || counts["SQL_EXECUTABLE_COMMENT"] != 1 ||
		counts["SQL_STATEMENT_UNBALANCED"] != 1 {
		t.Fatalf("gaps: %+v", counts)
	}
	for _, item := range result.Batch.Entities {
		if strings.Contains(item.Key.Identifier, "possible") || strings.Contains(item.Key.Identifier, "after_version_comment") {
			t.Fatalf("looked past unknown SQL mode: %+v", item)
		}
	}
}

func TestMySQLLiteralUsesPinnedFragmentAndProfile(t *testing.T) {
	const literalSQL = "SELECT id FROM app.customer"
	file := "package fixture\n// ghi chú\nconst text = `" + literalSQL + "`\n"
	literal := sqlCapture("app.go", "literal", file)
	start := strings.Index(file, literalSQL)
	literal.Ref.Range = model.ByteRange{StartByte: int64(start), EndByte: int64(start + len(literalSQL))}
	schema := sqlCapture("db/schema.sql", "schema", "CREATE TABLE app.customer (id INT);")
	profile := fixtureProfile()
	inno, err := AnalyzeCaptured(context.Background(), profile, []Source{literal, schema})
	if err != nil || len(inno.Batch.Links) != 1 || inno.Batch.Links[0].Kind != "reads" {
		t.Fatalf("literal: %+v %v", inno, err)
	}
	ref := inno.Batch.Links[0].Evidence.SourceRefs[0]
	if ref.Path != "app.go" || ref.ValidateCapturedBytes(literal.Data) != nil || file[ref.Range.StartByte:ref.Range.EndByte] != "app.customer" {
		t.Fatalf("fragment offset or content hash: %+v", ref)
	}
	profile.Engine = "NDBCLUSTER"
	ndb, err := AnalyzeCaptured(context.Background(), profile, []Source{literal, schema})
	if err != nil || inno.ProfileDigest == ndb.ProfileDigest || inno.Batch.Entities[1].Ref.EntityID == ndb.Batch.Entities[1].Ref.EntityID ||
		ndb.Batch.Entities[1].Key.Namespace != "local.database.ndb" {
		t.Fatalf("profiles collapsed: %+v %v", ndb, err)
	}
	profile.Dialect = "postgresql"
	if _, err := AnalyzeCaptured(context.Background(), profile, []Source{literal}); !errors.Is(err, ErrProfile) {
		t.Fatalf("unsupported dialect: %v", err)
	}
	literal.Data[0] = '!'
	if _, err := AnalyzeCaptured(context.Background(), fixtureProfile(), []Source{literal}); !errors.Is(err, ErrSource) {
		t.Fatalf("changed bytes: %v", err)
	}
}

func TestMySQLModesAndInputBudgetAreExplicit(t *testing.T) {
	profile := fixtureProfile()
	profile.Version = "8.0.41"
	profile.SQLMode = "ansi_quotes"
	quoted := sqlCapture("db/quoted.sql", "schema", `CREATE TABLE "app"."customer" (id INT); SELECT id FROM "app"."customer";`)
	ansi, err := AnalyzeCaptured(context.Background(), profile, []Source{quoted})
	if err != nil || len(ansi.Batch.Links) != 1 {
		t.Fatalf("ANSI_QUOTES: %+v %v", ansi, err)
	}
	defaultMode, err := AnalyzeCaptured(context.Background(), fixtureProfile(), []Source{quoted})
	if err != nil || len(defaultMode.Batch.Links) != 0 || defaultMode.Coverage.Status != "partial" {
		t.Fatalf("double-quoted identifier inferred under default profile: %+v %v", defaultMode, err)
	}
	schema := sqlCapture("db/schema.sql", "schema", "CREATE TABLE app.customer (id INT);")
	literal := sqlCapture("db/bound.sql", "literal", "SELECT 'x\\' FROM app.customer;")
	profile.SQLMode = "no_backslash_escapes"
	parsed, err := AnalyzeCaptured(context.Background(), profile, []Source{schema, literal})
	if err != nil || len(parsed.Batch.Links) != 1 {
		t.Fatalf("NO_BACKSLASH_ESCAPES: %+v %v", parsed, err)
	}
	large := sqlCapture("db/large.sql", "schema", strings.Repeat(" ", maxSourceBytes+1))
	if value, err := AnalyzeCaptured(context.Background(), fixtureProfile(), []Source{large}); !errors.Is(err, ErrLimit) || len(value.Batch.Entities) != 0 {
		t.Fatalf("limit returned data: %+v %v", value, err)
	}
}
