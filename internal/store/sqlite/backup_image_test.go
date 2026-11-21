package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	modsqlite "modernc.org/sqlite"
)

// Only a test driver hook: commit another WAL writer immediately before the
// real NewBackup call. Production has no fault hook or caller-supplied driver.
type backupHookDriver struct{ before func() error }
type backupHookConn struct {
	driver.Conn
	before func() error
}

var backupHookSequence atomic.Uint64

func (d backupHookDriver) Open(name string) (driver.Conn, error) {
	c, err := (&modsqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &backupHookConn{c, d.before}, nil
}
func (c *backupHookConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}
func (c *backupHookConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *backupHookConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *backupHookConn) NewBackup(uri string) (*modsqlite.Backup, error) {
	if c.before != nil {
		if err := c.before(); err != nil {
			return nil, err
		}
	}
	return c.Conn.(interface {
		NewBackup(string) (*modsqlite.Backup, error)
	}).NewBackup(uri)
}

func backupFixtureFileURL(path string) url.URL {
	slashPath := filepath.ToSlash(path)
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	return url.URL{Scheme: "file", Path: slashPath}
}

func backupFixture(t *testing.T, hook bool) (context.Context, *sql.DB, *sql.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	dir := t.TempDir()
	source := filepath.Join(dir, "source.sqlite")
	u := backupFixtureFileURL(source)
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE payload(version INTEGER NOT NULL,body TEXT NOT NULL) STRICT"} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO payload VALUES(1,?)`, strings.Repeat("bounded synthetic data\n", 3000)); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(source + "-wal"); err != nil || info.Size() <= 32 {
		t.Fatalf("fixture requires nonempty WAL: %v", err)
	}
	pool := db
	if hook {
		name := fmt.Sprintf("mce4d-backup-hook-%d", backupHookSequence.Add(1))
		sql.Register(name, backupHookDriver{before: func() error { _, err := db.ExecContext(ctx, `UPDATE payload SET version=2`); return err }})
		pool, err = sql.Open(name, u.String())
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = pool.Close() })
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return ctx, db, conn, filepath.Join(dir, "backup.sqlite")
}

func backupDestination(t *testing.T, path string) string {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	u := backupFixtureFileURL(path)
	u.RawQuery = url.Values{"mode": {"rw"}, "_pragma": {"synchronous=FULL", "busy_timeout=5000"}}.Encode()
	return u.String()
}

func TestPinnedSQLiteBackupContainsSnapshotBeforeConcurrentWALCommit(t *testing.T) {
	ctx, source, conn, path := backupFixture(t, true)
	uri := backupDestination(t, path)
	image, err := copyPinnedSQLiteBackup(ctx, conn, uri, 1<<20)
	if err != nil || image.Pages < 1 || image.Bytes != image.Pages*image.PageSize || image.SQLiteVersion == "" {
		t.Fatalf("backup: %+v %v", image, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != image.Bytes {
		t.Fatalf("standalone backup size: %v %v", info, err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("backup requires a sidecar: %s %v", suffix, err)
		}
	}
	// Source's actual second writer committed, while backup retains the pinned
	// first version. Read standalone bytes without a WAL or recovery sidecar.
	var current int
	if err := conn.QueryRowContext(ctx, `SELECT version FROM payload`).Scan(&current); err != nil || current != 2 {
		t.Fatalf("concurrent writer did not commit: %d %v", current, err)
	}
	u := backupFixtureFileURL(path)
	u.RawQuery = "mode=ro&immutable=1"
	backup, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	backup.SetMaxOpenConns(1)
	var version int
	var body, integrity, mode string
	if err := backup.QueryRowContext(ctx, `SELECT version,body FROM payload`).Scan(&version, &body); err != nil || version != 1 || body != strings.Repeat("bounded synthetic data\n", 3000) {
		t.Fatalf("snapshot mismatch: version=%d %v", version, err)
	}
	if err := backup.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("backup integrity: %s %v", integrity, err)
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("source WAL mode changed: %s %v", mode, err)
	}
	_ = source // Original pool remains open throughout; this is not a quiescent-pair/owner proof.
}

func TestPinnedSQLiteBackupLimitsAndErrorsPreserveSource(t *testing.T) {
	for _, name := range []string{"limit", "cancel", "destination"} {
		t.Run(name, func(t *testing.T) {
			ctx, _, conn, path := backupFixture(t, false)
			uri := backupDestination(t, path)
			limit := int64(1 << 20)
			want := error(nil)
			switch name {
			case "limit":
				limit = 512
				want = ErrStateLimit
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "destination":
				missing := backupFixtureFileURL(filepath.Join(path, "missing.sqlite"))
				uri = missing.String()
			}
			image, err := copyPinnedSQLiteBackup(ctx, conn, uri, limit)
			if err == nil || want != nil && !errors.Is(err, want) || image != (backupImage{}) {
				t.Fatalf("unsafe backup result: %+v %v", image, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Size() != 0 {
				t.Fatalf("failure wrote destination: %v %v", info, err)
			}
			check, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var version int
			if err := conn.QueryRowContext(check, `SELECT version FROM payload`).Scan(&version); err != nil || version != 1 {
				t.Fatalf("failure changed source: %d %v", version, err)
			}
		})
	}
}
