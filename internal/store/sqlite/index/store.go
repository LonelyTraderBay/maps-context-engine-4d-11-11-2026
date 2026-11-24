package index

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// Store borrows caller-owned pools and a lifecycle lease. The caller must
// supply pools for the same authorized file and retain the lease until every
// pool connection and handle is closed. It must not change the pool caps.
// Public transport must wait until the real OS lease is wired.
type Store struct {
	readers       *sql.DB
	image         bool
	writer        *sql.DB
	lease         ports.LifecycleLease
	identity      Identity
	schema        int
	streamsMu     sync.Mutex
	streams       map[string]*providerStageState
	manifestProof atomic.Pointer[generationManifestProof]
}

// Open validates an existing V2 store; it does not initialize or migrate it.
func Open(ctx context.Context, readers, writer *sql.DB, worktreeID string, lease ports.LifecycleLease) (*Store, error) {
	return open(ctx, readers, writer, worktreeID, lease, supportedSchema)
}

// OpenUICompatible reads either the sealed R1 schema or the sealed UI schema.
// An exclusive maintenance owner must migrate before V4 metadata can be used.
func OpenUICompatible(ctx context.Context, readers, writer *sql.DB, worktreeID string, lease ports.LifecycleLease) (*Store, error) {
	return open(ctx, readers, writer, worktreeID, lease, uiAddonSchema)
}

func open(ctx context.Context, readers, writer *sql.DB, worktreeID string, lease ports.LifecycleLease, maximum int) (*Store, error) {
	if ctx == nil || !withinPoolLimit(readers, 4) || !withinPoolLimit(writer, 1) || lease == nil || !validUUID(worktreeID) {
		return nil, ErrInvalidStore
	}
	version, identity, err := readIdentity(ctx, readers, worktreeID, lease, false, maximum)
	if err != nil {
		return nil, err
	}
	return &Store{readers: readers, writer: writer, lease: lease, identity: identity, schema: version}, nil
}

// OpenAuthenticatedImage borrows a receipt-verified immutable SQLite pool.
// Image mode has no writer. The owner retains the lifecycle lease.
func OpenAuthenticatedImage(ctx context.Context, readers *sql.DB, worktreeID string, lease ports.LifecycleLease) (*Store, error) {
	if ctx == nil || !withinPoolLimit(readers, 1) || lease == nil || !validUUID(worktreeID) {
		return nil, ErrInvalidStore
	}
	version, identity, err := readIdentity(ctx, readers, worktreeID, lease, true, supportedSchema)
	if err != nil {
		return nil, err
	}
	return &Store{readers: readers, lease: lease, identity: identity, image: true, schema: version}, nil
}

func (s *Store) readable() bool {
	return s != nil && s.lease != nil && withinPoolLimit(s.readers, 4) &&
		(s.image || withinPoolLimit(s.writer, 1))
}

// SchemaVersion reports the version pinned by Open. Operation entry points
// still validate the live schema and incarnation.
func (s *Store) SchemaVersion() int { return s.schema }

func withinPoolLimit(db *sql.DB, maximum int) bool {
	if db == nil {
		return false
	}
	limit := db.Stats().MaxOpenConnections
	return limit > 0 && limit <= maximum
}

// Identity revalidates the current schema and incarnation under the caller's
// shared lease; an observed replacement never silently becomes this store.
func (s *Store) Identity(ctx context.Context) (Identity, error) {
	if ctx == nil || !s.readable() {
		return Identity{}, ErrInvalidStore
	}
	version, identity, err := readIdentity(ctx, s.readers, s.identity.WorktreeID, s.lease, s.image, s.schema)
	if err != nil {
		return Identity{}, err
	}
	if version != s.schema || identity != s.identity {
		return Identity{}, ErrStoreChanged
	}
	return identity, nil
}

func readIdentity(ctx context.Context, db *sql.DB, worktreeID string, lease ports.LifecycleLease, image bool, maximum int) (_ int, identity Identity, resultErr error) {
	if err := lease.Check(ctx, worktreeID, ports.SharedLease); err != nil {
		return 0, Identity{}, err
	}
	conn, err := borrowConfigured(ctx, db, true)
	if err != nil {
		return 0, Identity{}, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			identity = Identity{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireReadJournal(ctx, conn, image); err != nil {
		return 0, Identity{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, Identity{}, err
	}
	defer tx.Rollback()
	version, identity, err := inspectStoreThrough(ctx, tx, worktreeID, maximum)
	if err != nil {
		return 0, Identity{}, err
	}
	if version == 1 || version == 3 {
		return 0, Identity{}, ErrMigrationRequired
	}
	if version != supportedSchema && version != uiAddonSchema {
		return 0, Identity{}, ErrUnrecognizedStore
	}
	if err := lease.Check(ctx, worktreeID, ports.SharedLease); err != nil {
		return 0, Identity{}, err
	}
	if err := tx.Commit(); err != nil {
		return 0, Identity{}, err
	}
	return version, identity, nil
}

func (s *Store) inspectPinned(ctx context.Context, tx schemaQueryer) (Identity, error) {
	version, identity, err := inspectStoreThrough(ctx, tx, s.identity.WorktreeID, s.schema)
	if err != nil {
		return Identity{}, err
	}
	if version != s.schema || identity != s.identity {
		return Identity{}, ErrStoreChanged
	}
	return identity, nil
}
