package index

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

// borrowConfigured configures and verifies each borrowed connection. The
// caller owns the pool; this adapter never opens a second pool for the store.
func borrowConfigured(ctx context.Context, db *sql.DB, readOnly bool) (*sql.Conn, error) {
	if ctx == nil || db == nil {
		return nil, ErrInvalidStore
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*sql.Conn, error) {
		return nil, errors.Join(err, discardConnection(conn))
	}
	for _, statement := range []string{"PRAGMA query_only=OFF", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=FULL"} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fail(err)
		}
	}
	if readOnly {
		if _, err := conn.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
			return fail(err)
		}
	}
	for _, check := range []struct {
		query string
		want  int
	}{{"PRAGMA foreign_keys", 1}, {"PRAGMA busy_timeout", 5000}, {"PRAGMA synchronous", 2}, {"PRAGMA query_only", boolInt(readOnly)}} {
		var got int
		if err := conn.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			return fail(errors.Join(ErrPragma, err))
		}
	}
	return conn, nil
}

func releaseConfigured(ctx context.Context, conn *sql.Conn, readOnly bool) error {
	if conn == nil || ctx == nil {
		return ErrInvalidStore
	}
	if readOnly {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanupCtx, "PRAGMA query_only=OFF"); err != nil {
			return errors.Join(err, discardConnection(conn))
		}
	}
	return conn.Close()
}

// A connection whose configuration could not be restored must not return to
// the caller's pool. database/sql marks it bad when Raw returns ErrBadConn.
func discardConnection(conn *sql.Conn) error {
	err := conn.Raw(func(any) error { return driver.ErrBadConn })
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return nil
	}
	return err
}

func requireReadJournal(ctx context.Context, conn *sql.Conn, image bool) error {
	if image {
		// The filesystem owner verified the WAL-mode header and receipt
		// before supplying this immutable VFS image with no live sidecars.
		return ctx.Err()
	}
	return requireWAL(ctx, conn, false)
}

func requireWAL(ctx context.Context, conn *sql.Conn, enable bool) error {
	query := "PRAGMA journal_mode"
	if enable {
		query = "PRAGMA journal_mode=WAL"
	}
	var mode string
	if err := conn.QueryRowContext(ctx, query).Scan(&mode); err != nil || mode != "wal" {
		return errors.Join(ErrPragma, err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
