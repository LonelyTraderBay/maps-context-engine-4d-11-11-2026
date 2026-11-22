package index

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/index"
)

const fixtureWorktree = "11111111-1111-4111-8111-111111111111"
const fixtureIncarnation = "22222222-2222-4222-8222-222222222222"

var errFixtureLease = errors.New("fixture lease not held")

// This probe is not an OS lock. Actual process coordination belongs to T010.
type leaseProbe struct {
	mu     sync.Mutex
	mode   ports.LeaseMode
	id     string
	calls  int
	failAt int
}

func (l *leaseProbe) Check(ctx context.Context, worktreeID string, mode ports.LeaseMode) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.id != worktreeID || (l.mode != mode && l.mode != ports.ExclusiveLease) || l.failAt == l.calls {
		return errFixtureLease
	}
	return nil
}

func fixtureSQLiteURI(path string) string {
	slashPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func fixtureDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.sqlite")
	dsn := fixtureSQLiteURI(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func fixtureLease(mode ports.LeaseMode) *leaseProbe {
	return &leaseProbe{mode: mode, id: fixtureWorktree}
}

func TestInitializeCreatesV2IdentityAndRequiresLease(t *testing.T) {
	db, _ := fixtureDB(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, db, fixtureWorktree, nil); err == nil {
		t.Fatal("initialization accepted missing lease")
	}
	if _, err := Initialize(ctx, db, fixtureWorktree, fixtureLease(ports.SharedLease)); !errors.Is(err, errFixtureLease) {
		t.Fatalf("shared lease accepted for migration: %v", err)
	}
	exclusive := fixtureLease(ports.ExclusiveLease)
	created, err := Initialize(ctx, db, fixtureWorktree, exclusive)
	if err != nil || created.WorktreeID != fixtureWorktree || !validUUID(created.Incarnation) || created.Incarnation == fixtureIncarnation {
		t.Fatalf("initialization: %+v %v", created, err)
	}
	var version, reader int
	if err := db.QueryRow(`SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&version, &reader); err != nil || version != 2 || reader != 2 {
		t.Fatalf("wrong R1 schema: %d %d %v", version, reader, err)
	}
	second, err := Initialize(ctx, db, fixtureWorktree, fixtureLease(ports.ExclusiveLease))
	if err != nil || second != created {
		t.Fatalf("identity changed on reopen: %+v %v", second, err)
	}
	store, err := Open(ctx, db, db, fixtureWorktree, fixtureLease(ports.SharedLease))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := store.Identity(ctx)
	if err != nil || actual != created {
		t.Fatalf("opened identity: %+v %v", actual, err)
	}
	if _, err := Open(ctx, db, db, "33333333-3333-4333-8333-333333333333", fixtureLease(ports.SharedLease)); err == nil {
		t.Fatal("wrong worktree was accepted")
	}
}

func TestMigrationV1ToV2IsAtomicAndPreservesIdentity(t *testing.T) {
	db, _ := fixtureDB(t)
	if _, err := db.Exec(migration.V1()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO store_identity(singleton,worktree_id,incarnation) VALUES(1,?,?)`, fixtureWorktree, fixtureIncarnation); err != nil {
		t.Fatal(err)
	}
	lease := fixtureLease(ports.ExclusiveLease)
	lease.failAt = 2
	if _, err := Initialize(context.Background(), db, fixtureWorktree, lease); !errors.Is(err, errFixtureLease) {
		t.Fatalf("lost exclusive lease accepted: %v", err)
	}
	var version, modules int
	if err := db.QueryRow(`SELECT schema_version FROM schema_meta WHERE singleton=1`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed migration changed schema version: %d %v", version, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='modules'`).Scan(&modules); err != nil || modules != 0 {
		t.Fatalf("failed migration left modules table: %d %v", modules, err)
	}
	identity, err := Initialize(context.Background(), db, fixtureWorktree, fixtureLease(ports.ExclusiveLease))
	if err != nil || identity.Incarnation != fixtureIncarnation {
		t.Fatalf("migration identity: %+v %v", identity, err)
	}
	if err := db.QueryRow(`SELECT schema_version FROM schema_meta WHERE singleton=1`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("migration did not reach V2: %d %v", version, err)
	}
}

func TestMigrationRollsBackWhenWriterLeaseIsLost(t *testing.T) {
	db, _ := fixtureDB(t)
	if _, err := db.Exec(migration.V1()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO store_identity(singleton,worktree_id,incarnation) VALUES(1,?,?)`, fixtureWorktree, fixtureIncarnation); err != nil {
		t.Fatal(err)
	}
	writer := &writerProbe{failAt: 2}
	if _, err := InitializeWithWriter(context.Background(), db, fixtureWorktree, fixtureLease(ports.ExclusiveLease), writer, fixtureJob); !errors.Is(err, errFixtureLease) {
		t.Fatalf("lost writer lease accepted during migration: %v", err)
	}
	var version, modules int
	if err := db.QueryRow(`SELECT schema_version FROM schema_meta`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed migration changed version: %d %v", version, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='modules'`).Scan(&modules); err != nil || modules != 0 {
		t.Fatalf("failed migration left schema: %d %v", modules, err)
	}
}

func TestInitializeRejectsUnknownAndNewerStoresWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name, setup string
		want        error
	}{
		{"unknown", `CREATE TABLE user_owned(value TEXT); INSERT INTO user_owned VALUES('preserve');`, ErrUnrecognizedStore},
		{"newer", `CREATE TABLE schema_meta(singleton INTEGER PRIMARY KEY,schema_version INTEGER,min_reader_version INTEGER); INSERT INTO schema_meta VALUES(1,3,3); CREATE TABLE user_owned(value TEXT); INSERT INTO user_owned VALUES('preserve');`, ErrSchemaTooNew},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, path := fixtureDB(t)
			if _, err := db.Exec(tc.setup); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			dsn := fixtureSQLiteURI(path)
			db, err = sql.Open("sqlite", dsn)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			defer db.Close()
			if _, err := Initialize(context.Background(), db, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); !errors.Is(err, tc.want) {
				t.Fatalf("unexpected result: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("unrecognized store changed: %v", err)
			}
		})
	}
}

func TestOpenRejectsTamperedSchemaAndChangedIdentity(t *testing.T) {
	db, _ := fixtureDB(t)
	identity, err := Initialize(context.Background(), db, fixtureWorktree, fixtureLease(ports.ExclusiveLease))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), db, db, fixtureWorktree, fixtureLease(ports.SharedLease))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE store_identity SET incarnation=? WHERE singleton=1`, fixtureIncarnation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Identity(context.Background()); !errors.Is(err, ErrStoreChanged) {
		t.Fatalf("changed incarnation accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE store_identity SET incarnation=? WHERE singleton=1`, identity.Incarnation); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE files ADD COLUMN unexpected TEXT`); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), db, db, fixtureWorktree, fixtureLease(ports.SharedLease)); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("tampered schema accepted: %v", err)
	}
}

func TestEveryBorrowedConnectionEnforcesPragmas(t *testing.T) {
	db, _ := fixtureDB(t)
	if _, err := Initialize(context.Background(), db, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	ctx := context.Background()
	connections := make([]*sql.Conn, 0, 4)
	for i := 0; i < 4; i++ {
		conn, err := borrowConfigured(ctx, db, true)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		for _, item := range []struct{ pragma, want string }{{"foreign_keys", "1"}, {"busy_timeout", "5000"}, {"synchronous", "2"}, {"journal_mode", "wal"}, {"query_only", "1"}} {
			var got string
			if err := conn.QueryRowContext(ctx, "PRAGMA "+item.pragma).Scan(&got); err != nil || got != item.want {
				t.Fatalf("connection %d %s=%q: %v", i, item.pragma, got, err)
			}
		}
		if _, err := conn.ExecContext(ctx, `CREATE TABLE forbidden(value TEXT)`); err == nil {
			t.Fatal("reader was allowed to write")
		}
	}
	for _, conn := range connections {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := borrowConfigured(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseConfigured(ctx, conn, false)
	var queryOnly int
	if err := conn.QueryRowContext(ctx, `PRAGMA query_only`).Scan(&queryOnly); err != nil || queryOnly != 0 {
		t.Fatalf("reader pragma leaked to writer: %d %v", queryOnly, err)
	}
}

func TestOpenEnforcesCallerOwnedPoolCaps(t *testing.T) {
	writer, path := fixtureDB(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, writer, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); err != nil {
		t.Fatal(err)
	}
	dsn := fixtureSQLiteURI(path)
	readers, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	readers.SetMaxOpenConns(4)
	store, err := Open(ctx, readers, writer, fixtureWorktree, fixtureLease(ports.SharedLease))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Identity(ctx); err != nil {
		t.Fatal(err)
	}
	readers.SetMaxOpenConns(5)
	if _, err := store.Identity(ctx); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("reader pool expansion accepted: %v", err)
	}
	if _, err := Open(ctx, readers, writer, fixtureWorktree, fixtureLease(ports.SharedLease)); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("too many readers accepted: %v", err)
	}
	readers.SetMaxOpenConns(4)
	writer.SetMaxOpenConns(2)
	if _, err := Open(ctx, readers, writer, fixtureWorktree, fixtureLease(ports.SharedLease)); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("too many writers accepted: %v", err)
	}
	if _, err := Initialize(ctx, writer, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("unbounded maintenance pool accepted: %v", err)
	}
	writer.SetMaxOpenConns(0)
	if _, err := Open(ctx, readers, writer, fixtureWorktree, fixtureLease(ports.SharedLease)); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("unbounded writer pool accepted: %v", err)
	}
}
