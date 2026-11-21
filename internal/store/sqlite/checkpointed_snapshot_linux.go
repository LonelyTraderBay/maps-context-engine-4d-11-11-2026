//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

const checkpointedSnapshotMaxBytes int64 = 64 << 20

// CheckpointedSnapshot is an experimental, read-only view of one checkpointed
// database. It is not a product state opener: acquiring the exclusive lease
// prevents cooperating writers and readers from using that state concurrently.
// The caller owns and authorizes all roots and must keep them open until Close.
type CheckpointedSnapshot struct {
	mu       sync.Mutex
	obs      *StateObservation
	lease    *runtime.Lifecycle
	proof    func(context.Context, [sha256.Size]byte) error
	registry *vfs.FS
	db       *sql.DB
	source   *os.File
	digest   [sha256.Size]byte
	size     int64
	closing  bool
	closed   bool
	closeErr error
}

// SnapshotQuery exposes only queries on the snapshot connection. It and all
// returned rows expire when the enclosing Read callback returns.
type SnapshotQuery struct {
	mu     sync.Mutex
	ctx    context.Context
	conn   *sql.Conn
	rows   []*sql.Rows
	closed bool
}

// OpenCheckpointedSnapshot requires an exclusive lifecycle lease for the exact
// root, no sidecars, and a cooperative absence of other state users. The
// caller's independent proof must attest that the given digest belongs to a
// complete checkpoint; absence of a WAL by itself does not prove that. It
// takes two bounded reads before registering an immutable, memory-only VFS.
// It does not establish that a caller-supplied DB pool or live WAL is safe.
func OpenCheckpointedSnapshot(ctx context.Context, root *paths.Root, protected []*paths.Root, lease *runtime.Lifecycle, scope, name string, maxBytes int64, authorize func(context.Context) error, checkpointProof func(context.Context, [sha256.Size]byte) error) (_ *CheckpointedSnapshot, resultErr error) {
	if ctx == nil || root == nil || lease == nil || checkpointProof == nil || maxBytes < 1 || maxBytes > checkpointedSnapshotMaxBytes {
		return nil, ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lease.CheckRoot(ctx, root, scope, ports.ExclusiveLease) != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrStateUnsafe
	}
	obs, err := ObserveStateFiles(ctx, root, protected, lease, scope, name, maxBytes, authorize)
	if err != nil {
		return nil, err
	}
	s := &CheckpointedSnapshot{obs: obs, lease: lease, proof: checkpointProof}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, s.Close())
		}
	}()
	for _, sidecar := range obs.files[1:] {
		if sidecar.file != nil {
			return nil, ErrStateUnsafe
		}
	}
	// Only this quiesced snapshot opens the main file for reading. Its
	// exclusive lease forbids cooperative SQLite users while that fd is open.
	s.source, err = root.OpenRegular(name)
	if err != nil {
		return nil, errors.Join(ErrStateUnsafe, err)
	}
	info, err := s.source.Stat()
	identity, identityErr := obs.files[0].file.Stat()
	if err != nil || identityErr != nil || stateOwner(s.source, false) != nil || !os.SameFile(info, identity) || info.Size() < 1 || info.Size() > maxBytes {
		return nil, ErrStateUnsafe
	}
	if err = obs.Recheck(ctx); err != nil {
		return nil, err
	}
	s.size = info.Size()
	data := make([]byte, int(s.size))
	first, err := s.hashSource(ctx, data)
	if err != nil {
		return nil, err
	}
	// SQLite's on-disk header uses version 2 for both read and write when the
	// database is in WAL format. This does not prove a live WAL is empty.
	if len(data) < 100 || string(data[:16]) != "SQLite format 3\x00" || data[18] != 2 || data[19] != 2 {
		return nil, ErrStateUnsafe
	}
	s.digest = first
	if err = s.verifySource(ctx); err != nil {
		return nil, err
	}
	nameVFS, registry, err := vfs.New(snapshotFS{data: data})
	if err != nil {
		return nil, err
	}
	s.registry = registry
	values := url.Values{"mode": {"ro"}, "immutable": {"1"}, "vfs": {nameVFS}}
	for _, pragma := range []string{"query_only(1)", "foreign_keys(1)", "busy_timeout(5000)", "synchronous(FULL)", "temp_store(MEMORY)"} {
		values.Add("_pragma", pragma)
	}
	s.db = sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: "file:snapshot.sqlite?" + values.Encode()})
	s.db.SetMaxOpenConns(1)
	s.db.SetMaxIdleConns(1)
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	err = errors.Join(validateSnapshotConnection(ctx, conn), conn.Close())
	if err != nil {
		return nil, err
	}
	if err = s.verifySource(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Read serializes access, denies statements other than SELECT/READ and checks
// source identity, content and authority both before and after the callback.
// The trusted callback must buffer results and publish them only after Read
// succeeds; it must not reenter this snapshot or retain a query or rows.
func (s *CheckpointedSnapshot) Read(ctx context.Context, fn func(*SnapshotQuery) error) error {
	if s == nil || ctx == nil || fn == nil {
		return ErrStateUnsafe
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.closed {
		return ErrOwnerClosed
	}
	if err := s.verifySource(ctx); err != nil {
		return err
	}
	err := s.runQuery(ctx, fn)
	return errors.Join(err, s.verifySource(ctx))
}

func (s *CheckpointedSnapshot) runQuery(ctx context.Context, fn func(*SnapshotQuery) error) (resultErr error) {
	return runSnapshotQuery(ctx, s.db, validateSnapshotConnection, fn)
}

// runSnapshotQuery applies configuration before installing the query-only
// authorizer, including when database/sql replaces a physical connection.
func runSnapshotQuery(ctx context.Context, db *sql.DB, configure func(context.Context, *sql.Conn) error, fn func(*SnapshotQuery) error) (resultErr error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	query := &SnapshotQuery{ctx: ctx, conn: conn}
	defer func() { resultErr = errors.Join(resultErr, query.close(), conn.Close()) }()
	if err = conn.Raw(func(driverConn any) error {
		registerer, ok := driverConn.(modsqlite.AuthorizerRegisterer)
		if !ok {
			return ErrStateUnsupported
		}
		return registerer.RegisterAuthorizer(nil)
	}); err != nil {
		return err
	}
	if err = configure(ctx, conn); err != nil {
		return err
	}
	if err = conn.Raw(func(driverConn any) error {
		registerer, ok := driverConn.(modsqlite.AuthorizerRegisterer)
		if !ok {
			return ErrStateUnsupported
		}
		return registerer.RegisterAuthorizer(func(action modsqlite.AuthorizerActionCode, _, _, database, _ string) modsqlite.AuthorizerReturnCode {
			switch action {
			case modsqlite.AuthSelect, modsqlite.AuthRecursive:
				return modsqlite.AuthorizerOK
			case modsqlite.AuthRead:
				if database == "main" || database == "" {
					return modsqlite.AuthorizerOK
				}
			}
			return modsqlite.AuthorizerDeny
		})
	}); err != nil {
		return err
	}
	return fn(query)
}

// Validate each physical connection, including a replacement opened by the
// pool. Setting a PRAGMA only on the first connection is not sufficient.
func validateSnapshotConnection(ctx context.Context, conn *sql.Conn) error {
	return validateSnapshotConnectionMode(ctx, conn, "delete")
}

func validateSnapshotConnectionMode(ctx context.Context, conn *sql.Conn, expectedJournal string) error {
	var journal string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil || journal != expectedJournal {
		return errors.Join(ErrStateUnsafe, err)
	}
	checks := []struct {
		statement string
		want      int
	}{
		{"PRAGMA query_only", 1}, {"PRAGMA foreign_keys", 1},
		{"PRAGMA busy_timeout", 5000}, {"PRAGMA synchronous", 2},
		{"PRAGMA temp_store", 2},
	}
	for _, check := range checks {
		var got int
		if err := conn.QueryRowContext(ctx, check.statement).Scan(&got); err != nil || got != check.want {
			return errors.Join(ErrStateUnsafe, err)
		}
	}
	var schemaVersion int
	if err := conn.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&schemaVersion); err != nil {
		return err
	}
	return ctx.Err()
}

// Query rejects use after the callback and binds execution to its deadline.
// SQL functions, PRAGMA, ATTACH, writes and transaction statements are denied
// by the SQLite authorizer; this deliberately narrow API is not a core reader.
func (q *SnapshotQuery) Query(statement string, args ...any) (*sql.Rows, error) {
	if q == nil || len(statement) == 0 || len(statement) > 32768 || len(args) > 64 {
		return nil, ErrStateUnsafe
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil, ErrOwnerClosed
	}
	if len(q.rows) >= 64 {
		return nil, ErrStateLimit
	}
	rows, err := q.conn.QueryContext(q.ctx, statement, args...)
	if err == nil {
		q.rows = append(q.rows, rows)
	}
	return rows, err
}

func (q *SnapshotQuery) close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	var err error
	for _, rows := range q.rows {
		err = errors.Join(err, rows.Close())
	}
	q.rows, q.conn = nil, nil
	return err
}

func (s *CheckpointedSnapshot) verifySource(ctx context.Context) error {
	if ctx == nil || s.obs == nil || s.lease == nil || s.proof == nil {
		return ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.lease.CheckRoot(ctx, s.obs.root, s.obs.scope, ports.ExclusiveLease) != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrStateUnsafe
	}
	if s.proof(ctx, s.digest) != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrStateUnsafe
	}
	digest, err := s.hashSource(ctx, nil)
	if err != nil {
		return err
	}
	if digest != s.digest {
		return ErrStateChanged
	}
	if s.proof(ctx, s.digest) != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrStateUnsafe
	}
	return ctx.Err()
}

// hashSource optionally captures the bytes while hashing. Observations are
// repeated around each bounded read; they are not an atomic OS reservation
// against arbitrary processes acting as the same user.
func (s *CheckpointedSnapshot) hashSource(ctx context.Context, capture []byte) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	if err := s.obs.Recheck(ctx); err != nil {
		return zero, err
	}
	file := s.source
	if file == nil {
		return zero, ErrStateUnsafe
	}
	info, err := file.Stat()
	if err != nil || info.Size() != s.size {
		return zero, ErrStateChanged
	}
	if capture != nil && int64(len(capture)) != s.size {
		return zero, ErrStateUnsafe
	}
	h := sha256.New()
	buffer := make([]byte, 256<<10)
	for offset := int64(0); offset < s.size; {
		if err := s.obs.Recheck(ctx); err != nil {
			return zero, err
		}
		length := min(int64(len(buffer)), s.size-offset)
		chunk := buffer[:int(length)]
		if capture != nil {
			chunk = capture[int(offset):int(offset+length)]
		}
		n, readErr := file.ReadAt(chunk, offset)
		if n != len(chunk) || readErr != nil && !errors.Is(readErr, io.EOF) {
			return zero, ErrStateChanged
		}
		_, _ = h.Write(chunk)
		offset += length
	}
	if err = s.obs.Recheck(ctx); err != nil {
		return zero, err
	}
	info, err = file.Stat()
	if err != nil || info.Size() != s.size {
		return zero, ErrStateChanged
	}
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

// Close closes the pool and VFS before releasing its lifecycle attachment. A
// close error retains the attachment because safe release is not established.
func (s *CheckpointedSnapshot) Close() error {
	if s == nil {
		return ErrStateUnsafe
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrOwnerClosed
	}
	if s.closeErr != nil {
		return s.closeErr
	}
	s.closing = true
	if s.db != nil {
		if s.closeErr = s.db.Close(); s.closeErr != nil {
			return s.closeErr
		}
		s.db = nil
	}
	if s.registry != nil {
		if s.closeErr = s.registry.Close(); s.closeErr != nil {
			return s.closeErr
		}
		s.registry = nil
	}
	if s.source != nil {
		if s.closeErr = s.source.Close(); s.closeErr != nil {
			return s.closeErr
		}
		s.source = nil
	}
	if s.obs != nil {
		if s.closeErr = s.obs.Close(); s.closeErr != nil {
			return s.closeErr
		}
		s.obs = nil
	}
	s.closed = true
	return nil
}
