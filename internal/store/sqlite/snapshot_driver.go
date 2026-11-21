//go:build linux || windows

package sqlite

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"io/fs"
	"os"
	"time"

	modsqlite "modernc.org/sqlite"
)

type snapshotFS struct{ data []byte }

// A private driver avoids inheriting process-global SQL functions and hooks.
// The upstream SQLite VFS registry remains process-global and is closed with
// the snapshot after its pool has closed.
type snapshotConnector struct {
	driver *modsqlite.Driver
	dsn    string
}

func (c *snapshotConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	return conn, nil
}

func (c *snapshotConnector) Driver() driver.Driver { return c.driver }

func (f snapshotFS) Open(name string) (fs.File, error) {
	if name != "snapshot.sqlite" {
		return nil, os.ErrNotExist
	}
	return &snapshotFile{Reader: bytes.NewReader(f.data), name: name}, nil
}

type snapshotFile struct {
	*bytes.Reader
	name string
}

func (f *snapshotFile) Close() error { return nil }
func (f *snapshotFile) Stat() (fs.FileInfo, error) {
	return snapshotInfo{name: f.name, size: f.Size()}, nil
}

type snapshotInfo struct {
	name string
	size int64
}

func (i snapshotInfo) Name() string     { return i.name }
func (i snapshotInfo) Size() int64      { return i.size }
func (snapshotInfo) Mode() fs.FileMode  { return 0400 }
func (snapshotInfo) ModTime() time.Time { return time.Time{} }
func (snapshotInfo) IsDir() bool        { return false }
func (snapshotInfo) Sys() any           { return nil }
