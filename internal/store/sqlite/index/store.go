package index

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// Store borrows caller-owned pools and a lifecycle lease. The caller must
// supply pools for the same authorized file and retain the lease until every
// pool connection and handle is closed. It must not change the pool caps.
// Public transport must wait until the real OS lease is wired.
type Store struct {
	readers   *sql.DB
	image     bool
	writer    *sql.DB
	lease     ports.LifecycleLease
	identity  Identity
	streamsMu sync.Mutex
	streams   map[string]*providerStageState
}

// Open validates an existing V2 store; it does not initialize or migrate it.
func Open(ctx context.Context, readers, writer *sql.DB, worktreeID string, lease ports.LifecycleLease) (*Store, error) {
	if ctx == nil || !withinPoolLimit(readers, 4) || !withinPoolLimit(writer, 1) || lease == nil || !validUUID(worktreeID) {
		return nil, ErrInvalidStore
	}
	identity, err := readIdentity(ctx, readers, worktreeID, lease, false)
	if err != nil {
		return nil, err
	}
	return &Store{readers: readers, writer: writer, lease: lease, identity: identity}, nil
}

// OpenAuthenticatedImage borrows a receipt-verified immutable SQLite pool.
// Image mode has no writer. The owner retains the lifecycle lease.
func OpenAuthenticatedImage(ctx context.Context, readers *sql.DB, worktreeID string, lease ports.LifecycleLease) (*Store, error) {
	if ctx == nil || !withinPoolLimit(readers, 1) || lease == nil || !validUUID(worktreeID) {
		return nil, ErrInvalidStore
	}
	identity, err := readIdentity(ctx, readers, worktreeID, lease, true)
	if err != nil {
		return nil, err
	}
	return &Store{readers: readers, lease: lease, identity: identity, image: true}, nil
}

func (s *Store) readable() bool {
	return s != nil && s.lease != nil && withinPoolLimit(s.readers, 4) &&
		(s.image || withinPoolLimit(s.writer, 1))
}

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
	identity, err := readIdentity(ctx, s.readers, s.identity.WorktreeID, s.lease, s.image)
	if err != nil {
		return Identity{}, err
	}
	if identity != s.identity {
		return Identity{}, ErrStoreChanged
	}
	return identity, nil
}

func readIdentity(ctx context.Context, db *sql.DB, worktreeID string, lease ports.LifecycleLease, image bool) (identity Identity, resultErr error) {
	if err := lease.Check(ctx, worktreeID, ports.SharedLease); err != nil {
		return Identity{}, err
	}
	conn, err := borrowConfigured(ctx, db, true)
	if err != nil {
		return Identity{}, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			identity = Identity{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireReadJournal(ctx, conn, image); err != nil {
		return Identity{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Identity{}, err
	}
	defer tx.Rollback()
	version, identity, err := inspectStore(ctx, tx, worktreeID)
	if err != nil {
		return Identity{}, err
	}
	if version == 1 {
		return Identity{}, ErrMigrationRequired
	}
	if version != supportedSchema {
		return Identity{}, ErrUnrecognizedStore
	}
	if err := lease.Check(ctx, worktreeID, ports.SharedLease); err != nil {
		return Identity{}, err
	}
	if err := tx.Commit(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}
