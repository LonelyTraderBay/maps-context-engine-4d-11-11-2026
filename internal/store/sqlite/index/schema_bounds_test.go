package index

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func TestSchemaReaderRejectsOversizedMetadataAndExcessObjects(t *testing.T) {
	for _, scenario := range []string{"sql", "name", "count"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := fixtureDB(t)
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

func TestIndexRejectsOversizedCorruptIdentity(t *testing.T) {
	for _, field := range []string{"worktree_id", "incarnation"} {
		t.Run(field, func(t *testing.T) {
			db, _ := fixtureDB(t)
			ctx := context.Background()
			if _, err := Initialize(ctx, db, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE store_identity SET `+field+`=?`, strings.Repeat("x", 70000)); err != nil {
				t.Fatal(err)
			}
			if _, err := Initialize(ctx, db, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("initialize accepted corrupt identity: %v", err)
			}
			if _, err := Open(ctx, db, db, fixtureWorktree, fixtureLease(ports.SharedLease)); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("open accepted corrupt identity: %v", err)
			}
		})
	}
}
