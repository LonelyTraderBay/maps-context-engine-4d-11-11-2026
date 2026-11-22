package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInitializePersistsRandomIncarnation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	first, err := Initialize(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(first) {
		t.Fatalf("not a random UUIDv4: %q", first)
	}
	second, err := Initialize(ctx, db)
	if err != nil || first != second {
		t.Fatalf("identity changed on reopen: %q %q %v", first, second, err)
	}
	var count, version int
	if err := db.QueryRow(`SELECT count(*),max(schema_version) FROM schema_meta`).Scan(&count, &version); err != nil || count != 1 || version != 1 {
		t.Fatalf("schema meta: %d %d %v", count, version, err)
	}
	var check string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		t.Fatalf("integrity: %q %v", check, err)
	}
}

func TestInitializeRejectsUnknownStoreWithoutModification(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`CREATE TABLE sqliteX(value TEXT); INSERT INTO sqliteX VALUES('owned fixture')`); err != nil {
		t.Fatal(err)
	}
	if value, err := Initialize(context.Background(), db); !errors.Is(err, ErrUnrecognizedStore) || value != "" {
		t.Fatalf("unknown store accepted: %q %v", value, err)
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM sqliteX`).Scan(&value); err != nil || value != "owned fixture" {
		t.Fatalf("unknown data changed: %q %v", value, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='catalog_identity'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unknown store was initialized: %d %v", count, err)
	}
}

func TestInitializeClassifiesUnknownStoreBeforeAttemptingWrites(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`CREATE TABLE sqliteX(value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), db); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("unknown store was not classified before writes: %v", err)
	}
}

func TestInitializeRejectsNewerAndAlteredSchemas(t *testing.T) {
	t.Run("newer", func(t *testing.T) {
		db := testDB(t)
		if _, err := db.Exec(`CREATE TABLE schema_meta(singleton INTEGER PRIMARY KEY,schema_version INTEGER,min_reader_version INTEGER); INSERT INTO schema_meta VALUES(1,2,2)`); err != nil {
			t.Fatal(err)
		}
		if _, err := Initialize(context.Background(), db); !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("newer schema accepted: %v", err)
		}
		var version int
		if err := db.QueryRow(`SELECT schema_version FROM schema_meta`).Scan(&version); err != nil || version != 2 {
			t.Fatalf("newer schema changed: %d %v", version, err)
		}
	})
	t.Run("altered", func(t *testing.T) {
		db := testDB(t)
		if _, err := Initialize(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`ALTER TABLE repositories ADD COLUMN unexpected TEXT`); err != nil {
			t.Fatal(err)
		}
		if _, err := Initialize(context.Background(), db); !errors.Is(err, ErrUnrecognizedStore) {
			t.Fatalf("altered schema accepted: %v", err)
		}
	})
	t.Run("extra-trigger", func(t *testing.T) {
		db := testDB(t)
		if _, err := Initialize(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TRIGGER injected AFTER INSERT ON repositories BEGIN DELETE FROM repositories; END`); err != nil {
			t.Fatal(err)
		}
		if _, err := Initialize(context.Background(), db); !errors.Is(err, ErrUnrecognizedStore) {
			t.Fatalf("extra trigger accepted: %v", err)
		}
	})
}

func TestInitializeRejectsCorruptIncarnation(t *testing.T) {
	db := testDB(t)
	if _, err := Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE catalog_identity SET incarnation='00000000-0000-0000-0000-000000000000' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), db); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("invalid incarnation accepted: %v", err)
	}
}

func TestInitializeConcurrentCallersShareIdentity(t *testing.T) {
	db := testDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const workers = 6
	values := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := Initialize(ctx, db)
			values <- value
			errs <- err
		}()
	}
	wg.Wait()
	close(values)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for value := range values {
		if first == "" {
			first = value
		}
		if value != first {
			t.Fatal("concurrent initializations produced different incarnations")
		}
	}
}

func TestInitializeConcurrentConnectionsNeverLeavePartialCatalog(t *testing.T) {
	file := filepath.Join(t.TempDir(), "catalog.sqlite")
	dsn := fixtureSQLiteURI(file) + "?_pragma=busy_timeout%281000%29"
	const workers = 4
	dbs := make([]*sql.DB, workers)
	for i := range dbs {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		dbs[i] = db
		t.Cleanup(func() { db.Close() })
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	values := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for _, db := range dbs {
		wg.Add(1)
		go func(db *sql.DB) {
			defer wg.Done()
			<-start
			value, err := Initialize(ctx, db)
			values <- value
			errs <- err
		}(db)
	}
	close(start)
	wg.Wait()
	close(values)
	close(errs)
	for err := range errs {
		if err == nil {
			continue
		}
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code()&0xff != 5 {
			t.Fatalf("unexpected initialization failure: %v", err)
		}
	}
	final, err := Initialize(ctx, dbs[0])
	if err != nil || !validUUID(final) {
		t.Fatalf("catalog left partial: %q %v", final, err)
	}
	for value := range values {
		if value != "" && value != final {
			t.Fatal("concurrent connections committed different identities")
		}
	}
	var status string
	if err := dbs[0].QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&status); err != nil || status != "ok" {
		t.Fatalf("catalog integrity: %q %v", status, err)
	}
}
