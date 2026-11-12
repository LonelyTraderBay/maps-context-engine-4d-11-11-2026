package catalog

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const catalogOperation = "22222222-2222-4222-8222-222222222222"

var errFixtureRevoked = errors.New("fixture lock revoked")

type lifecycleFixture struct {
	mode          ports.LeaseMode
	calls, failAt int
}

func (l *lifecycleFixture) Check(ctx context.Context, scope string, required ports.LeaseMode) error {
	l.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if scope != "catalog" || required == ports.ExclusiveLease && l.mode != ports.ExclusiveLease || l.calls == l.failAt {
		return errFixtureRevoked
	}
	return nil
}

type mutationFixture struct{ calls, failAt int }

func (m *mutationFixture) CheckCatalogMutation(ctx context.Context, operation string) error {
	m.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if operation != catalogOperation || m.calls == m.failAt {
		return errFixtureRevoked
	}
	return nil
}
func (m *mutationFixture) HoldCatalogMutation(ctx context.Context, operation string) (ports.LeaseGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if operation != catalogOperation {
		return nil, errFixtureRevoked
	}
	return catalogFixtureGuard{}, nil
}

type catalogFixtureGuard struct{}

func (catalogFixtureGuard) Close() error { return nil }

func leasedCatalogDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLeasedCatalogRollsBackOnLostAuthority(t *testing.T) {
	for _, tc := range []string{"writer", "lifecycle"} {
		t.Run(tc, func(t *testing.T) {
			db := leasedCatalogDB(t)
			ctx := context.Background()
			if _, err := Initialize(ctx, db); err != nil {
				t.Fatal(err)
			}
			var mode string
			if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
				t.Fatalf("WAL: %q %v", mode, err)
			}
			lease := &lifecycleFixture{mode: ports.SharedLease}
			mutation := &mutationFixture{}
			base, err := OpenExistingLeased(ctx, db, lease)
			if err != nil {
				t.Fatal(err)
			}
			bound, err := base.WithMutation(ctx, mutation, catalogOperation)
			if err != nil {
				t.Fatal(err)
			}
			if tc == "writer" {
				mutation.failAt = mutation.calls + 2
			} else {
				lease.failAt = lease.calls + 2
			}
			err = bound.write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO repo_groups(group_id,name,status,revision,created_at,updated_at) VALUES(?,?,'active',1,'now','now')`, catalogOperation, "fixture")
				return err
			})
			if !errors.Is(err, errFixtureRevoked) {
				t.Fatalf("lost authority accepted: %v", err)
			}
			var count, queryOnly int
			if err := db.QueryRow(`SELECT count(*) FROM repo_groups`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed transaction persisted: %d %v", count, err)
			}
			if err := db.QueryRow(`PRAGMA query_only`).Scan(&queryOnly); err != nil || queryOnly != 0 {
				t.Fatalf("query_only leaked: %d %v", queryOnly, err)
			}
		})
	}
}

func TestLeasedCatalogInitializationRollsBackOnLeaseLoss(t *testing.T) {
	db := leasedCatalogDB(t)
	lease := &lifecycleFixture{mode: ports.ExclusiveLease, failAt: 2}
	if _, err := InitializeLeased(context.Background(), db, lease, &mutationFixture{}, catalogOperation); !errors.Is(err, errFixtureRevoked) {
		t.Fatalf("lost exclusive lease accepted: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed initialization left schema: %d %v", count, err)
	}
}

func TestOpenExistingLeasedNeverInitializes(t *testing.T) {
	db := leasedCatalogDB(t)
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL: %q %v", mode, err)
	}
	if _, err := OpenExistingLeased(context.Background(), db, &lifecycleFixture{mode: ports.SharedLease}); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("empty catalog was opened: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read-only open created schema: %d %v", count, err)
	}
}
