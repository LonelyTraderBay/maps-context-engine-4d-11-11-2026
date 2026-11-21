//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	modsqlite "modernc.org/sqlite"
)

// This fixture exercises database/sql's automatic rollback with the real
// native transaction wrapper. It uses an in-memory SQLite database because
// it tests connection lifetime, independently of the protected filesystem.
type cancelledReadConnector struct{ driver *modsqlite.Driver }

func (c cancelledReadConnector) Driver() driver.Driver { return c.driver }

func (c cancelledReadConnector) Connect(context.Context) (driver.Conn, error) {
	base, err := c.driver.Open(":memory:")
	if err != nil {
		return nil, err
	}
	return &cancelledReadConn{&nativeConn{base: base, readOnly: true}}, nil
}

type cancelledReadConn struct{ *nativeConn }

func (c *cancelledReadConn) Close() error { return c.base.Close() }

func (c *cancelledReadConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	base, err := c.base.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	tx := &nativeTx{base: base, conn: c.nativeConn, ctx: ctx}
	c.active = tx
	return tx, nil
}

func TestNativeCancelledReadRetainsHealthyConnection(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	pool := sql.OpenDB(cancelledReadConnector{driver: &modsqlite.Driver{}})
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	borrow := func() (*sql.Conn, *nativeConn) {
		t.Helper()
		conn, err := pool.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var native *nativeConn
		if err := conn.Raw(func(driverConn any) error {
			native = driverConn.(*cancelledReadConn).nativeConn
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return conn, native
	}
	conn, initial := borrow()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, err := pool.BeginTx(request, &sql.TxOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	cancel()
	// Borrowing the only slot waits for database/sql's asynchronous rollback.
	conn, after := borrow()
	defer conn.Close()
	if after != initial {
		t.Fatal("cancelling a read discarded a healthy native connection")
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}
