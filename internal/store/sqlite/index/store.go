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
	identity, err := readIdentity(ctx, readers, worktreeID, lease)
	if err != nil {
		return nil, err
	}
	return &Store{readers: readers, writer: writer, lease: lease, identity: identity}, nil
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
	if s == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || s.lease == nil || ctx == nil {
		return Identity{}, ErrInvalidStore
	}
	identity, err := readIdentity(ctx, s.readers, s.identity.WorktreeID, s.lease)
	if err != nil {
		return Identity{}, err
	}
	if identity != s.identity {
		return Identity{}, ErrStoreChanged
	}
	return identity, nil
}

func readIdentity(ctx context.Context, db *sql.DB, worktreeID string, lease ports.LifecycleLease) (identity Identity, resultErr error) {
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
	if err := requireWAL(ctx, conn, false); err != nil {
		return Identity{}, err
	}
	version, identity, err := inspectStore(ctx, conn, worktreeID)
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
	return identity, nil
}
