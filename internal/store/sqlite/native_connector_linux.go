//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
)

var errNativeStatement = errors.New("native store statement requires an explicit transaction")

type nativeConnector struct {
	store    *nativeWALStore
	readOnly bool
	driver   *modsqlite.Driver
}

func (c *nativeConnector) Driver() driver.Driver { return c.driver }

func (c *nativeConnector) Connect(ctx context.Context) (connected driver.Conn, resultErr error) {
	a, err := c.store.admit(ctx, false)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, a.close())
		if resultErr != nil && connected != nil {
			resultErr = errors.Join(resultErr, connected.Close())
			connected = nil
		}
	}()
	if c.store.quota != nil {
		if err := c.store.quota.check(ctx, a.receipt.Image, 0, 0); err != nil {
			return nil, err
		}
	}
	observation, err := c.store.domain.Observe(ctx, c.store.lease, c.store.scope, c.store.name, walLogicalImageMaxBytes)
	if err != nil {
		return nil, err
	}
	if err := observation.Close(); err != nil {
		return nil, err
	}
	root, err := c.store.domain.root.CanonicalPath()
	if err != nil {
		return nil, err
	}
	mode := "rw"
	if c.readOnly {
		mode = "ro"
	}
	values := url.Values{"mode": {mode}, "_pragma": {"foreign_keys=ON", "synchronous=FULL", "busy_timeout=5000", "temp_store=MEMORY", "wal_autocheckpoint=0", "cache_spill=OFF"}}
	u := url.URL{Scheme: "file", Path: filepath.Join(root, c.store.name), RawQuery: values.Encode()}
	hold, err := c.store.lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	base, err := c.driver.Open(u.String())
	if err != nil {
		return nil, errors.Join(err, hold.Close())
	}
	if err := c.store.check(ctx); err != nil {
		closeErr := base.Close()
		if closeErr == nil {
			closeErr = hold.Close()
		}
		return nil, errors.Join(err, closeErr)
	}
	return &nativeConn{base: base, store: c.store, readOnly: c.readOnly, hold: hold}, nil
}

func (s *nativeWALStore) pool(readOnly bool, maximum int) *sql.DB {
	db := sql.OpenDB(&nativeConnector{store: s, readOnly: readOnly, driver: &modsqlite.Driver{}})
	db.SetMaxOpenConns(maximum)
	db.SetMaxIdleConns(maximum)
	return db
}

type nativeConn struct {
	base     driver.Conn
	store    *nativeWALStore
	readOnly bool
	active   *nativeTx
	hold     *runtime.LifecycleHold
	closed   bool
	poisoned bool
}

func nativeCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (c *nativeConn) Prepare(query string) (driver.Stmt, error) {
	ctx, cancel := nativeCleanupContext()
	defer cancel()
	return c.PrepareContext(ctx, query)
}

func (c *nativeConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.statement(ctx, query); err != nil {
		return nil, err
	}
	var statement driver.Stmt
	var err error
	if prepare, ok := c.base.(driver.ConnPrepareContext); ok {
		statement, err = prepare.PrepareContext(ctx, query)
	} else {
		statement, err = c.base.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &nativeStmt{base: statement, conn: c, query: query}, nil
}

func (c *nativeConn) Close() error {
	if c.closed {
		return ErrOwnerClosed
	}
	ctx, cancel := nativeCleanupContext()
	defer cancel()
	if c.active != nil {
		// The transaction reports its outcome separately. A successful native
		// close below proves handle release even if rollback/receipt failed.
		_ = c.active.Rollback()
	}
	gate, err := c.store.closeGate(ctx)
	if err != nil {
		return err
	}
	closeErr := c.base.Close()
	if closeErr == nil {
		c.closed = true
		closeErr = c.hold.Close()
	}
	return errors.Join(closeErr, gate.Close())
}

func (c *nativeConn) Begin() (driver.Tx, error) { return nil, errNativeStatement }

func (c *nativeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (_ driver.Tx, resultErr error) {
	if !c.IsValid() {
		return nil, driver.ErrBadConn
	}
	if c.active != nil || c.readOnly && !opts.ReadOnly {
		return nil, errNativeStatement
	}
	a, err := c.store.admit(ctx, !opts.ReadOnly)
	if err != nil {
		return nil, err
	}
	begin, ok := c.base.(driver.ConnBeginTx)
	if !ok {
		return nil, errors.Join(errNativeStatement, a.close())
	}
	base, err := begin.BeginTx(ctx, opts)
	if err != nil {
		// No successful transaction exists. A preparation is still retained if
		// cleanup/capture cannot independently prove the last ACK unchanged.
		if !opts.ReadOnly {
			err = a.finish(ctx, false, err)
		}
		return nil, errors.Join(err, a.close())
	}
	t := &nativeTx{base: base, conn: c, ctx: ctx}
	c.active = t
	if opts.ReadOnly {
		// BEGIN is deferred in SQLite. Force the snapshot to be pinned while
		// the authority gate still excludes a commit or checkpoint/close.
		query, ok := c.base.(driver.QueryerContext)
		if !ok {
			err = errNativeStatement
		} else {
			var rows driver.Rows
			rows, err = query.QueryContext(ctx, "SELECT schema_version FROM schema_meta WHERE singleton=1", nil)
			if err == nil {
				values := make([]driver.Value, len(rows.Columns()))
				err = rows.Next(values)
				err = errors.Join(err, rows.Close())
			}
		}
		closeErr := a.close()
		if err != nil || closeErr != nil {
			c.active = nil
			return nil, errors.Join(err, closeErr, base.Rollback())
		}
	} else {
		t.admission = a
	}
	return t, nil
}

func (c *nativeConn) statement(ctx context.Context, query string) error {
	if !c.IsValid() {
		return driver.ErrBadConn
	}
	if err := c.statementAuthority(ctx, query); err != nil {
		return err
	}
	if c.active != nil || nativeConfigurationPragma(query) {
		return nil
	}
	return errNativeStatement
}

// This closed list changes only the borrowed connection or probes its mode;
// it returns no project data and cannot publish state. The connection already
// owns its protected-root lifecycle. Perform full installation authorization,
// OS boundary and receipt verification at BeginTx. Data statements continue
// to check full authority before execution and when their rows are closed.
func (c *nativeConn) statementAuthority(ctx context.Context, query string) error {
	if nativeConfigurationPragma(query) {
		return ctx.Err()
	}
	return c.store.check(ctx)
}

func (c *nativeConn) IsValid() bool { return !c.closed && !c.poisoned }

// A successful rollback leaves the native connection reusable. Advertising
// that to database/sql keeps a cancelled transaction from asynchronously
// discarding a healthy borrowed connection while its owner is closing the
// pool. Every subsequent transaction still rechecks the store authority.
func (c *nativeConn) ResetSession(context.Context) error {
	if !c.IsValid() || c.active != nil {
		return driver.ErrBadConn
	}
	return nil
}

// Only fixed, connection-local configuration and read-only mode probes may
// run outside a transaction. Never accept journal_mode=..., checkpoint,
// attach, vacuum, extensions or arbitrary SQL at this boundary.
func nativeConfigurationPragma(query string) bool {
	q := strings.ToLower(strings.Join(strings.Fields(query), ""))
	switch q {
	case "pragmaquery_only=off", "pragmaquery_only=on", "pragmaquery_only", "pragmaforeign_keys=on", "pragmaforeign_keys", "pragmabusy_timeout=5000", "pragmabusy_timeout", "pragmasynchronous=full", "pragmasynchronous", "pragmajournal_mode", "pragmatemp_store=memory", "pragmatemp_store", "pragmawal_autocheckpoint=0", "pragmawal_autocheckpoint":
		return true
	case "pragmacache_spill=off", "pragmacache_spill":
		return true
	}
	return false
}

func (c *nativeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.statement(ctx, query); err != nil {
		return nil, err
	}
	exec, ok := c.base.(driver.ExecerContext)
	if !ok {
		return nil, errNativeStatement
	}
	result, err := exec.ExecContext(ctx, query, args)
	return result, errors.Join(err, c.statementAuthority(ctx, query))
}

func (c *nativeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.statement(ctx, query); err != nil {
		return nil, err
	}
	queryer, ok := c.base.(driver.QueryerContext)
	if !ok {
		return nil, errNativeStatement
	}
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return &nativeRows{base: rows, conn: c, ctx: ctx, query: query}, nil
}

type nativeTx struct {
	base      driver.Tx
	conn      *nativeConn
	ctx       context.Context
	admission *nativeAdmission
	done      bool
}

func (t *nativeTx) Commit() error   { return t.finish(true) }
func (t *nativeTx) Rollback() error { return t.finish(false) }

func (t *nativeTx) finish(commit bool) error {
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	var sqlErr error
	var rollbackUnknown bool
	if commit {
		checkErr := t.conn.store.check(t.ctx)
		if checkErr == nil && t.admission != nil && t.conn.store.quota != nil {
			checkErr = t.checkCacheCommit()
		}
		if err := checkErr; err != nil {
			// Always end the underlying SQL transaction before releasing a gate.
			rollbackErr := t.base.Rollback()
			rollbackUnknown = rollbackErr != nil
			sqlErr = errors.Join(err, rollbackErr)
			commit = false
		} else {
			sqlErr = t.base.Commit()
			if sqlErr != nil {
				// SQLite can leave a transaction active after a failed COMMIT.
				// End it before exposing the gate to another native connection.
				rollbackErr := t.base.Rollback()
				rollbackUnknown = rollbackErr != nil
				sqlErr = errors.Join(sqlErr, rollbackErr)
			}
		}
	} else {
		sqlErr = t.base.Rollback()
		rollbackUnknown = sqlErr != nil
	}
	t.conn.active = nil
	if rollbackUnknown {
		t.conn.poisoned = true
	}
	if t.admission == nil {
		return sqlErr
	}
	if rollbackUnknown {
		return errors.Join(errNativeOutcomeUnknown, sqlErr, t.admission.close())
	}
	// An error/cancelled capture never ACKs a commit. A later explicit
	// recoverUnchangedPending may abort only if the prior image is proven.
	return errors.Join(t.admission.finish(t.ctx, commit, sqlErr), t.admission.close())
}

func (t *nativeTx) checkCacheCommit() error {
	values := make([]int64, 3)
	query, ok := t.conn.base.(driver.QueryerContext)
	if !ok {
		return ErrStateUnsafe
	}
	for i, name := range []string{"PRAGMA cache_spill", "PRAGMA page_size", "PRAGMA page_count"} {
		rows, err := query.QueryContext(t.ctx, name, nil)
		if err != nil {
			return err
		}
		cells := make([]driver.Value, len(rows.Columns()))
		err = errors.Join(rows.Next(cells), rows.Close())
		if err != nil || len(cells) != 1 {
			return errors.Join(ErrStateUnsafe, err)
		}
		value, ok := cells[0].(int64)
		if !ok {
			return ErrStateUnsafe
		}
		values[i] = value
	}
	if values[0] != 0 || values[1] != int64(t.admission.receipt.Image.PageSize) || values[2] < 1 {
		return ErrStateUnsafe
	}
	return t.conn.store.quota.check(t.ctx, t.admission.receipt.Image, values[2], walAuthorityRecordSize)
}

type nativeStmt struct {
	base  driver.Stmt
	conn  *nativeConn
	query string
}

func (s *nativeStmt) Close() error                               { return s.base.Close() }
func (s *nativeStmt) NumInput() int                              { return s.base.NumInput() }
func (s *nativeStmt) Exec([]driver.Value) (driver.Result, error) { return nil, errNativeStatement }
func (s *nativeStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, errNativeStatement }
func (s *nativeStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.conn.statement(ctx, s.query); err != nil {
		return nil, err
	}
	exec, ok := s.base.(driver.StmtExecContext)
	if !ok {
		return nil, errNativeStatement
	}
	result, err := exec.ExecContext(ctx, args)
	return result, errors.Join(err, s.conn.statementAuthority(ctx, s.query))
}
func (s *nativeStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.conn.statement(ctx, s.query); err != nil {
		return nil, err
	}
	query, ok := s.base.(driver.StmtQueryContext)
	if !ok {
		return nil, errNativeStatement
	}
	rows, err := query.QueryContext(ctx, args)
	if err != nil {
		return nil, err
	}
	return &nativeRows{base: rows, conn: s.conn, ctx: ctx, query: s.query}, nil
}

type nativeRows struct {
	base  driver.Rows
	conn  *nativeConn
	ctx   context.Context
	query string
}

func (r *nativeRows) Columns() []string { return r.base.Columns() }
func (r *nativeRows) Close() error {
	return errors.Join(r.base.Close(), r.conn.statementAuthority(r.ctx, r.query))
}
func (r *nativeRows) Next(values []driver.Value) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	err := r.base.Next(values)
	if errors.Is(err, io.EOF) {
		if checkErr := r.conn.statementAuthority(r.ctx, r.query); checkErr != nil {
			return checkErr
		}
	}
	return err
}

var _ driver.Connector = (*nativeConnector)(nil)
var _ driver.ConnBeginTx = (*nativeConn)(nil)
var _ driver.ConnPrepareContext = (*nativeConn)(nil)
var _ driver.ExecerContext = (*nativeConn)(nil)
var _ driver.QueryerContext = (*nativeConn)(nil)
var _ driver.Validator = (*nativeConn)(nil)
var _ driver.SessionResetter = (*nativeConn)(nil)
var _ driver.StmtExecContext = (*nativeStmt)(nil)
var _ driver.StmtQueryContext = (*nativeStmt)(nil)
