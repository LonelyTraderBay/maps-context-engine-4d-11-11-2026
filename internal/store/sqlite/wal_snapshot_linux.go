//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

// WALSnapshot is an experimental read-only recovery view. Its exclusive
// lifecycle lease blocks cooperating database users throughout its lifetime;
// it is not a concurrent product state opener or a persistent recovery.
type WALSnapshot struct {
	mu       sync.Mutex
	obs      *StateObservation
	lease    *runtime.Lifecycle
	proof    func(context.Context, WALSnapshotPair) error
	registry *vfs.FS
	db       *sql.DB
	main     *os.File
	wal      *os.File
	pair     WALSnapshotPair
	closing  bool
	closed   bool
	closeErr error
}

// OpenWALSnapshot requires an exclusive lifecycle lease, an existing WAL,
// no rollback journal, and a trusted independent proof of the exact committed
// main/WAL pair. A stale SHM file is observed but never read or modified.
// The proof must bind store incarnation, digests, last commit frame and its
// database page count; it must not merely recompute them from these files. The
// caller owns the roots and authority, must not reenter callbacks, and must
// buffer results until Read succeeds. This function never writes or repairs
// the source.
func OpenWALSnapshot(ctx context.Context, root *paths.Root, protected []*paths.Root, lease *runtime.Lifecycle, scope, name string, maxBytes int64, authorize func(context.Context) error, proof func(context.Context, WALSnapshotPair) error) (_ *WALSnapshot, resultErr error) {
	if ctx == nil || root == nil || lease == nil || proof == nil || maxBytes < 1 || maxBytes > checkpointedSnapshotMaxBytes {
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
	s := &WALSnapshot{obs: obs, lease: lease, proof: proof}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, s.Close())
		}
	}()
	if len(obs.files) != 4 || obs.files[0].file == nil || obs.files[1].file == nil || obs.files[3].file != nil {
		return nil, ErrStateUnsafe
	}
	s.main, s.pair.MainSize, err = openWALSnapshotSource(root, obs.files[0])
	if err != nil {
		return nil, err
	}
	s.wal, s.pair.WALSize, err = openWALSnapshotSource(root, obs.files[1])
	if err != nil {
		return nil, err
	}
	if s.pair.MainSize < 100 || s.pair.WALSize < 32 || s.pair.MainSize > maxBytes || s.pair.WALSize > maxBytes-s.pair.MainSize {
		return nil, ErrStateLimit
	}
	mainBytes := make([]byte, int(s.pair.MainSize))
	walBytes := make([]byte, int(s.pair.WALSize))
	s.pair, err = s.hashSources(ctx, mainBytes, walBytes)
	if err != nil {
		return nil, err
	}
	s.pair.WALCommitFrame, s.pair.WALCommitPages, err = walCommittedBoundary(ctx, mainBytes, walBytes)
	if err != nil {
		return nil, err
	}
	if err = s.verifySource(ctx); err != nil {
		return nil, err
	}
	nameVFS, registry, err := vfs.New(walSnapshotFS{main: mainBytes, wal: walBytes})
	if err != nil {
		return nil, err
	}
	s.registry = registry
	values := url.Values{"mode": {"ro"}, "vfs": {nameVFS}}
	s.db = sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: "file:snapshot.sqlite?" + values.Encode()})
	s.db.SetMaxOpenConns(1)
	s.db.SetMaxIdleConns(1)
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	err = configureWALSnapshotConnection(ctx, conn)
	if err == nil {
		var check string
		err = conn.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check)
		if err == nil && check != "ok" {
			err = ErrStateUnsafe
		}
	}
	err = errors.Join(err, conn.Close())
	if err != nil {
		return nil, err
	}
	if err = s.verifySource(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func openWALSnapshotSource(root *paths.Root, observed observedStateFile) (*os.File, int64, error) {
	if root == nil || observed.file == nil {
		return nil, 0, ErrStateUnsafe
	}
	file, err := root.OpenRegular(observed.name)
	if err != nil {
		return nil, 0, errors.Join(ErrStateUnsafe, err)
	}
	info, err := file.Stat()
	identity, identityErr := observed.file.Stat()
	if err != nil || identityErr != nil || stateOwner(file, false) != nil || !os.SameFile(info, identity) {
		return nil, 0, errors.Join(ErrStateUnsafe, file.Close())
	}
	return file, info.Size(), nil
}

func configureWALSnapshotConnection(ctx context.Context, conn *sql.Conn) error {
	// The pinned read-only VFS has no shared-memory methods. SQLite must enter
	// exclusive mode before its first database access to read a WAL snapshot.
	var locking string
	if err := conn.QueryRowContext(ctx, "PRAGMA locking_mode=EXCLUSIVE").Scan(&locking); err != nil || locking != "exclusive" {
		return errors.Join(ErrStateUnsafe, err)
	}
	for _, statement := range []string{
		"PRAGMA query_only=ON", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=FULL", "PRAGMA temp_store=MEMORY",
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return validateSnapshotConnectionMode(ctx, conn, "wal")
}

// Read exposes only read queries and revalidates the pair and proof before
// and after the callback. It cannot prevent uncooperative same-user changes.
func (s *WALSnapshot) Read(ctx context.Context, fn func(*SnapshotQuery) error) error {
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
	err := runSnapshotQuery(ctx, s.db, configureWALSnapshotConnection, fn)
	return errors.Join(err, s.verifySource(ctx))
}

func (s *WALSnapshot) verifySource(ctx context.Context) error {
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
	if s.proof(ctx, s.pair) != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrStateUnsafe
	}
	actual, err := s.hashSources(ctx, nil, nil)
	if err != nil {
		return err
	}
	if actual != s.pair {
		return ErrStateChanged
	}
	if s.proof(ctx, s.pair) != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrStateUnsafe
	}
	return ctx.Err()
}

func (s *WALSnapshot) hashSources(ctx context.Context, mainBytes, walBytes []byte) (WALSnapshotPair, error) {
	var zero WALSnapshotPair
	if s.obs == nil || s.main == nil || s.wal == nil {
		return zero, ErrStateUnsafe
	}
	if err := s.obs.Recheck(ctx); err != nil {
		return zero, err
	}
	actual := WALSnapshotPair{
		MainSize: s.pair.MainSize, WALSize: s.pair.WALSize,
		WALCommitFrame: s.pair.WALCommitFrame, WALCommitPages: s.pair.WALCommitPages,
	}
	for _, item := range []struct {
		file    *os.File
		size    int64
		capture []byte
		digest  *[sha256.Size]byte
	}{
		{s.main, s.pair.MainSize, mainBytes, &actual.MainSHA256},
		{s.wal, s.pair.WALSize, walBytes, &actual.WALSHA256},
	} {
		info, err := item.file.Stat()
		if err != nil || info.Size() != item.size {
			return zero, ErrStateChanged
		}
		if item.capture != nil && int64(len(item.capture)) != item.size {
			return zero, ErrStateUnsafe
		}
		hash := sha256.New()
		buffer := make([]byte, 256<<10)
		for offset := int64(0); offset < item.size; {
			if err := s.obs.Recheck(ctx); err != nil {
				return zero, err
			}
			length := min(int64(len(buffer)), item.size-offset)
			chunk := buffer[:int(length)]
			if item.capture != nil {
				chunk = item.capture[int(offset):int(offset+length)]
			}
			n, readErr := item.file.ReadAt(chunk, offset)
			if n != len(chunk) || readErr != nil && !errors.Is(readErr, io.EOF) {
				return zero, ErrStateChanged
			}
			_, _ = hash.Write(chunk)
			offset += length
		}
		info, err = item.file.Stat()
		if err != nil || info.Size() != item.size {
			return zero, ErrStateChanged
		}
		copy(item.digest[:], hash.Sum(nil))
	}
	if err := s.obs.Recheck(ctx); err != nil {
		return zero, err
	}
	return actual, nil
}

// Close closes the connection and VFS before its source descriptors and
// lifecycle attachment. An error retains the attachment for safe handling.
func (s *WALSnapshot) Close() error {
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
	for _, file := range []**os.File{&s.wal, &s.main} {
		if *file != nil {
			if s.closeErr = (*file).Close(); s.closeErr != nil {
				return s.closeErr
			}
			*file = nil
		}
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

type walSnapshotFS struct{ main, wal []byte }

func (f walSnapshotFS) Open(name string) (fs.File, error) {
	switch name {
	case "snapshot.sqlite":
		return &snapshotFile{Reader: bytes.NewReader(f.main), name: name}, nil
	case "snapshot.sqlite-wal":
		return &snapshotFile{Reader: bytes.NewReader(f.wal), name: name}, nil
	default:
		return nil, os.ErrNotExist
	}
}
