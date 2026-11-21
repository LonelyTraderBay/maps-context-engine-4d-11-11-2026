package records

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const fixtureWorktree = "11111111-1111-4111-8111-111111111111"
const fixtureJob = "22222222-2222-4222-8222-222222222222"

var fixtureDenied = errors.New("fixture authorization denied")

type fixtureLease struct{ calls, failAt int }

func (l *fixtureLease) Check(ctx context.Context, worktree string, _ ports.LeaseMode) error {
	l.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if worktree != fixtureWorktree || l.calls == l.failAt {
		return fixtureDenied
	}
	return nil
}

type fixtureWriter struct{ calls, failAt int }
type fixtureGuard struct{}

func (fixtureGuard) Close() error { return nil }
func (w *fixtureWriter) HoldWriter(ctx context.Context, worktree, job string) (ports.LeaseGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if worktree != fixtureWorktree || job != fixtureJob {
		return nil, fixtureDenied
	}
	return fixtureGuard{}, nil
}
func (w *fixtureWriter) CheckWriter(ctx context.Context, worktree, job string) error {
	w.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if worktree != fixtureWorktree || job != fixtureJob || w.calls == w.failAt {
		return fixtureDenied
	}
	return nil
}

func fixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.sqlite")
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func fixtureContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func initialized(t *testing.T) (context.Context, *sql.DB, *Store, *fixtureLease, Identity) {
	t.Helper()
	ctx, db, lease := fixtureContext(t), fixtureDB(t), &fixtureLease{}
	identity, err := InitializeWithWriter(ctx, db, fixtureWorktree, lease, &fixtureWriter{}, fixtureJob)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, db, fixtureWorktree, lease)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, db, store, lease, identity
}

func allowed(context.Context, Identity) error { return nil }

func TestRecordsIdentityInitializationAndRead(t *testing.T) {
	ctx, db, store, _, identity := initialized(t)
	if !uuid(identity.Incarnation) || identity.WorktreeID != fixtureWorktree {
		t.Fatalf("invalid identity: %+v", identity)
	}
	state, err := store.ReadState(ctx, 10, allowed)
	if err != nil || state.Identity != identity || state.RecordEpoch != 0 || state.ApprovedHeads != 0 {
		t.Fatalf("empty state: %+v %v", state, err)
	}
	again, err := InitializeWithWriter(ctx, db, fixtureWorktree, &fixtureLease{}, &fixtureWriter{}, fixtureJob)
	if err != nil || again != identity {
		t.Fatalf("reinitialize changed identity: %+v %v", again, err)
	}
	_, _, _, _, other := initialized(t)
	if other.Incarnation == identity.Incarnation {
		t.Fatal("independent stores share an incarnation")
	}
}

func TestRecordsRejectUnknownAndNewSchemaBeforeJournalChange(t *testing.T) {
	ctx := fixtureContext(t)
	db := fixtureDB(t)
	if _, err := db.Exec(`CREATE TABLE other(x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeWithWriter(ctx, db, fixtureWorktree, &fixtureLease{}, &fixtureWriter{}, fixtureJob); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("unknown store accepted: %v", err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "delete" {
		t.Fatalf("journal changed on unknown store: %q %v", mode, err)
	}
	ctx, db, _, lease, _ := initialized(t)
	if _, err := db.Exec(`UPDATE schema_meta SET schema_version=3,min_reader_version=3`); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, db, fixtureWorktree, lease); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("new schema accepted: %v", err)
	}
}

func TestRecordsInitializationRollsBackOnLostWriter(t *testing.T) {
	ctx, db := fixtureContext(t), fixtureDB(t)
	if _, err := InitializeWithWriter(ctx, db, fixtureWorktree, &fixtureLease{}, &fixtureWriter{failAt: 2}, fixtureJob); !errors.Is(err, fixtureDenied) {
		t.Fatalf("lost writer accepted: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed initialization left schema: %d %v", count, err)
	}
}

func TestRecordsStateChecksCurrentHeadsAndRevocation(t *testing.T) {
	ctx, db, store, lease, identity := initialized(t)
	for _, row := range []struct{ id, status string }{{"first", "approved"}, {"second", "proposed"}} {
		_, err := db.Exec(`INSERT INTO record_versions(record_id,version,record_kind,declared_status,payload_json,source_refs_json,actor_id,reason,payload_hash,created_at) VALUES(?,1,'decision',?,'{}','[]','fixture','fixture',?,'now')`, row.id, row.status, strings.Repeat("a", 64))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO record_heads(record_id,version) VALUES(?,1)`, row.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE records_meta SET record_epoch=2`); err != nil {
		t.Fatal(err)
	}
	state, err := store.ReadState(ctx, 2, allowed)
	if err != nil || state.Identity != identity || state.RecordEpoch != 2 || state.ApprovedHeads != 1 {
		t.Fatalf("wrong head state: %+v %v", state, err)
	}
	if state, err := store.ReadState(ctx, 1, allowed); !errors.Is(err, ErrLimit) || state != (State{}) {
		t.Fatalf("limit did not fail closed: %+v %v", state, err)
	}
	calls := 0
	if state, err := store.ReadState(ctx, 2, func(context.Context, Identity) error {
		calls++
		if calls == 2 {
			return fixtureDenied
		}
		return nil
	}); !errors.Is(err, fixtureDenied) || state != (State{}) {
		t.Fatalf("late denial leaked state: %+v %v", state, err)
	}
	lease.failAt = lease.calls + 2
	if state, err := store.ReadState(ctx, 2, allowed); !errors.Is(err, fixtureDenied) || state != (State{}) {
		t.Fatalf("lease loss leaked state: %+v %v", state, err)
	}
	lease.failAt = 0
	if _, err := db.Exec(`UPDATE records_meta SET record_epoch=0`); err != nil {
		t.Fatal(err)
	}
	if state, err := store.ReadState(ctx, 2, allowed); !errors.Is(err, ErrIntegrity) || state != (State{}) {
		t.Fatalf("inconsistent epoch accepted: %+v %v", state, err)
	}
	if _, err := db.Exec(`UPDATE records_meta SET incarnation='33333333-3333-4333-8333-333333333333'`); err != nil {
		t.Fatal(err)
	}
	if state, err := store.ReadState(ctx, 2, allowed); !errors.Is(err, ErrStoreChanged) || state != (State{}) {
		t.Fatalf("replaced identity accepted: %+v %v", state, err)
	}
}
