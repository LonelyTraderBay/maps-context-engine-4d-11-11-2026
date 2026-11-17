package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func TestSchemaReaderRejectsOversizedMetadataAndExcessObjects(t *testing.T) {
	for _, scenario := range []string{"sql", "name", "count"} {
		t.Run(scenario, func(t *testing.T) {
			db := testDB(t)
			switch scenario {
			case "sql":
				_, err := db.Exec(`CREATE TABLE example(x INTEGER /*` + strings.Repeat("x", 70000) + `*/)`)
				if err != nil {
					t.Fatal(err)
				}
			case "name":
				_, err := db.Exec(`CREATE TABLE "` + strings.Repeat("x", 1100) + `" (x INTEGER)`)
				if err != nil {
					t.Fatal(err)
				}
			case "count":
				for i := 0; i < 257; i++ {
					if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE t%03d(x INTEGER)`, i)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if objects, err := readSchema(context.Background(), db); !errors.Is(err, ErrUnrecognizedStore) || objects != nil {
				t.Fatalf("invalid metadata accepted: objects=%d error=%v", len(objects), err)
			}
		})
	}
}

func TestCachedReferenceStillRejectsChangedCatalogSchema(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(ctx, db); err != nil {
		t.Fatalf("valid schema: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE unauthorized_fixture (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(ctx, db); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("cached reference accepted changed catalog: %v", err)
	}
}

func TestCatalogRejectsOversizedCorruptIdentity(t *testing.T) {
	db := leasedCatalogDB(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL: %q %v", mode, err)
	}
	store, err := OpenExistingLeased(ctx, db, &lifecycleFixture{mode: ports.SharedLease})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE catalog_identity SET incarnation=?`, strings.Repeat("x", 70000)); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(ctx, db); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("initialize accepted corrupt identity: %v", err)
	}
	if _, err := OpenExistingLeased(ctx, db, &lifecycleFixture{mode: ports.SharedLease}); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("open accepted corrupt identity: %v", err)
	}
	if err := store.read(ctx, func(_ *sql.Tx) error { return nil }); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("read accepted corrupt identity: %v", err)
	}
}
