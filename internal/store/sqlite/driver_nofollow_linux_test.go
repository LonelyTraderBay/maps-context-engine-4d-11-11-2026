//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// This documents the pinned driver's behavior on test-owned files. It is not
// an opener for MCE4D state and must not be used to authorize a real path.
func TestPinnedSQLiteModeRWDoesNotRejectSymlink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target.sqlite"), filepath.Join(dir, "link.sqlite")
	uri := func(path, mode string) string {
		return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=" + mode}).String()
	}
	setup, err := sql.Open("sqlite", uri(target, "rwc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.ExecContext(ctx, `CREATE TABLE fixture (value INTEGER NOT NULL)`); err != nil {
		_ = setup.Close()
		t.Fatal(err)
	}
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", uri(link, "rw"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fixture(value) VALUES(7)`); err != nil {
		_ = db.Close()
		t.Fatalf("pinned driver no longer follows this fixture symlink: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	check, err := sql.Open("sqlite", uri(target, "ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var count int
	if err := check.QueryRowContext(ctx, `SELECT count(*) FROM fixture WHERE value=7`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("fixture target was not reached: %d %v", count, err)
	}
}
