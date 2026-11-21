package records

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/records"
	_ "modernc.org/sqlite"
)

var (
	ErrInvalidStore      = errors.New("invalid records store input")
	ErrUnrecognizedStore = errors.New("records schema is unrecognized")
	ErrSchemaTooNew      = errors.New("records schema is too new")
	ErrStoreChanged      = errors.New("records store identity changed")
	ErrPragma            = errors.New("required records SQLite setting is unavailable")
	ErrLimit             = errors.New("records metadata exceeds limit")
	ErrIntegrity         = errors.New("records head metadata is inconsistent")
	ErrCommitUnknown     = errors.New("records initialization outcome is unknown")
	ErrAfterCommit       = errors.New("records initialization committed but finalization failed")
)

const exactIntegerMax int64 = 9007199254740991

type Identity struct {
	WorktreeID  string
	Incarnation string
}

type State struct {
	Identity      Identity
	RecordEpoch   int64
	ApprovedHeads int64
}

// StateAuthorizer verifies actor, worktree and current policy for this store.
// It is a trust boundary and must not acquire a conflicting records writer.
type StateAuthorizer func(context.Context, Identity) error

type Store struct {
	db       *sql.DB
	image    bool
	lease    ports.LifecycleLease
	identity Identity
}

// InitializeWithWriter initializes only an empty, already authorized durable
// database. Existing recognized V1 data is preserved; unknown/newer schemas
// are rejected before changing journal mode. The caller owns the file path,
// exclusive lifecycle, writer lease and pool, and must reconcile uncertain
// commits. This function does not write a record or alter a target worktree.
func InitializeWithWriter(ctx context.Context, db *sql.DB, worktree string, lease ports.LifecycleLease, writer ports.IndexWriterLease, jobID string) (identity Identity, resultErr error) {
	if ctx == nil || db == nil || db.Stats().MaxOpenConnections != 1 || lease == nil || writer == nil || !uuid(worktree) || !uuid(jobID) {
		return Identity{}, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return Identity{}, ErrInvalidStore
	}
	if err := lease.Check(ctx, worktree, ports.ExclusiveLease); err != nil {
		return Identity{}, err
	}
	guard, err := writer.HoldWriter(ctx, worktree, jobID)
	if err != nil {
		return Identity{}, err
	}
	if guard == nil {
		return Identity{}, ErrInvalidStore
	}
	committed := false
	defer func() {
		if err := guard.Close(); err != nil {
			identity = Identity{}
			if committed {
				resultErr = errors.Join(resultErr, ErrAfterCommit, err)
			} else {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	if err := writer.CheckWriter(ctx, worktree, jobID); err != nil {
		return Identity{}, err
	}
	conn, err := borrow(ctx, db, false)
	if err != nil {
		return Identity{}, err
	}
	defer func() {
		if err := release(ctx, conn, false); err != nil {
			identity = Identity{}
			if committed {
				resultErr = errors.Join(resultErr, ErrAfterCommit, err)
			} else {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	version, previous, previousEpoch, err := inspect(ctx, conn, worktree)
	if err != nil {
		return Identity{}, err
	}
	if err := wal(ctx, conn, true); err != nil {
		return Identity{}, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return Identity{}, err
	}
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				identity = Identity{}
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	currentVersion, currentIdentity, currentEpoch, err := inspect(ctx, tx, worktree)
	if err != nil || currentVersion != version || currentIdentity != previous || currentEpoch != previousEpoch {
		return Identity{}, errors.Join(ErrStoreChanged, err)
	}
	if version == 0 {
		incarnation, err := newUUID()
		if err != nil {
			return Identity{}, err
		}
		if _, err := tx.ExecContext(ctx, migration.V1()); err != nil {
			return Identity{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO records_meta(singleton,worktree_id,incarnation,record_epoch) VALUES(1,?,?,0)`, worktree, incarnation); err != nil {
			return Identity{}, err
		}
		identity = Identity{WorktreeID: worktree, Incarnation: incarnation}
	} else {
		identity = previous
	}
	finalVersion, finalIdentity, _, err := inspect(ctx, tx, worktree)
	if err != nil || finalVersion != 1 || finalIdentity != identity {
		return Identity{}, errors.Join(ErrUnrecognizedStore, err)
	}
	if err := lease.Check(ctx, worktree, ports.ExclusiveLease); err != nil {
		return Identity{}, err
	}
	if err := writer.CheckWriter(ctx, worktree, jobID); err != nil {
		return Identity{}, err
	}
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	if err := tx.Commit(); err != nil {
		return Identity{}, errors.Join(ErrCommitUnknown, err)
	}
	committed = true
	return identity, nil
}

// Open verifies a recognized durable store under a shared lifecycle lease. It
// takes no ownership of the pool or lease and never initializes a missing DB.
func Open(ctx context.Context, db *sql.DB, worktree string, lease ports.LifecycleLease) (*Store, error) {
	return open(ctx, db, worktree, lease, false)
}

// OpenAuthenticatedImage borrows a receipt-verified immutable SQLite pool.
// The owner retains the lifecycle lease until the pool closes.
func OpenAuthenticatedImage(ctx context.Context, db *sql.DB, worktree string, lease ports.LifecycleLease) (*Store, error) {
	return open(ctx, db, worktree, lease, true)
}

func open(ctx context.Context, db *sql.DB, worktree string, lease ports.LifecycleLease, image bool) (*Store, error) {
	if ctx == nil || db == nil || db.Stats().MaxOpenConnections != 1 || lease == nil || !uuid(worktree) {
		return nil, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInvalidStore
	}
	store := &Store{db: db, lease: lease, identity: Identity{WorktreeID: worktree}, image: image}
	identity, _, err := store.readIdentity(ctx)
	if err != nil {
		return nil, err
	}
	store.identity = identity
	return store, nil
}

func (s *Store) readIdentity(ctx context.Context) (identity Identity, epoch int64, resultErr error) {
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return Identity{}, 0, err
	}
	conn, err := borrow(ctx, s.db, true)
	if err != nil {
		return Identity{}, 0, err
	}
	defer func() {
		if err := release(ctx, conn, true); err != nil {
			identity, epoch = Identity{}, 0
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := s.requireReadJournal(ctx, conn); err != nil {
		return Identity{}, 0, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Identity{}, 0, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			identity, epoch = Identity{}, 0
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, epoch, err := inspect(ctx, tx, s.identity.WorktreeID)
	if err != nil || version != 1 {
		return Identity{}, 0, errors.Join(ErrUnrecognizedStore, err)
	}
	if s.identity.Incarnation != "" && s.identity != identity {
		return Identity{}, 0, ErrStoreChanged
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return Identity{}, 0, err
	}
	return identity, epoch, ctx.Err()
}

func inspect(ctx context.Context, db schemaQueryer, worktree string) (int, Identity, int64, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*'`).Scan(&count); err != nil {
		return 0, Identity{}, 0, err
	}
	if count == 0 {
		return 0, Identity{}, 0, nil
	}
	var version, reader int
	if err := db.QueryRowContext(ctx, `SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&version, &reader); err != nil {
		return 0, Identity{}, 0, errors.Join(ErrUnrecognizedStore, err)
	}
	if version > 1 || reader > 1 {
		return 0, Identity{}, 0, ErrSchemaTooNew
	}
	if version != 1 || reader != 1 {
		return 0, Identity{}, 0, ErrUnrecognizedStore
	}
	if err := validateSchema(ctx, db); err != nil {
		return 0, Identity{}, 0, errors.Join(ErrUnrecognizedStore, err)
	}
	var identity Identity
	var epoch int64
	if err := db.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(worktree_id AS BLOB))=36 THEN worktree_id END,CASE WHEN length(CAST(incarnation AS BLOB))=36 THEN incarnation END,record_epoch FROM records_meta WHERE singleton=1`).Scan(&identity.WorktreeID, &identity.Incarnation, &epoch); err != nil || !uuid(identity.WorktreeID) || !uuid(identity.Incarnation) || identity.WorktreeID != worktree || epoch < 0 || epoch > exactIntegerMax {
		return 0, Identity{}, 0, ErrStoreChanged
	}
	return 1, identity, epoch, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func uuid(s string) bool {
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
		} else if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}
