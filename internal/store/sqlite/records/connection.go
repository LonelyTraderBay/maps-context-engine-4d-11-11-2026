package records

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

func borrow(ctx context.Context, db *sql.DB, readOnly bool) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*sql.Conn, error) { return nil, errors.Join(err, discard(conn)) }
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
	queryOnly := 0
	if readOnly {
		queryOnly = 1
	}
	for _, check := range []struct {
		query string
		want  int
	}{{"PRAGMA foreign_keys", 1}, {"PRAGMA busy_timeout", 5000}, {"PRAGMA synchronous", 2}, {"PRAGMA query_only", queryOnly}} {
		var value int
		if err := conn.QueryRowContext(ctx, check.query).Scan(&value); err != nil || value != check.want {
			return fail(errors.Join(ErrPragma, err))
		}
	}
	return conn, nil
}

func release(ctx context.Context, conn *sql.Conn, readOnly bool) error {
	if readOnly {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanup, "PRAGMA query_only=OFF"); err != nil {
			return errors.Join(err, discard(conn))
		}
	}
	return conn.Close()
}

func discard(conn *sql.Conn) error {
	err := conn.Raw(func(any) error { return driver.ErrBadConn })
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return nil
	}
	return err
}

func (s *Store) requireReadJournal(ctx context.Context, conn *sql.Conn) error {
	if s.image {
		return ctx.Err()
	}
	return wal(ctx, conn, false)
}

func wal(ctx context.Context, conn *sql.Conn, enable bool) error {
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
