// Package index implements the R1 index-store foundation against the frozen
// SQLite schema. It does not open paths, authorize actors, own the caller's
// pool, or implement the OS lifecycle locks required before public access.
package index

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/index"
	_ "modernc.org/sqlite"
)

var (
	ErrInvalidStore      = errors.New("invalid index store input")
	ErrUnrecognizedStore = errors.New("index schema is unrecognized")
	ErrSchemaTooNew      = errors.New("index schema is too new")
	ErrMigrationRequired = errors.New("index migration requires exclusive maintenance")
	ErrInvalidIdentity   = errors.New("index store identity is invalid")
	ErrWrongWorktree     = errors.New("index belongs to a different worktree")
	ErrStoreChanged      = errors.New("index incarnation changed")
	ErrPragma            = errors.New("required SQLite setting is unavailable")
)

const supportedSchema = 2

type Identity struct {
	WorktreeID  string
	Incarnation string
}

// Initialize creates V1 then V2 atomically for an empty authorized database,
// or migrates a recognized V1 under an exclusive lifecycle lease. The caller
// owns path authorization, the pool, quiescence and any required backup. A
// failed commit may have an unknown outcome and must be reconciled by reading
// the store, never by blindly replaying the operation.
func Initialize(ctx context.Context, db *sql.DB, worktreeID string, lease ports.LifecycleLease) (identity Identity, resultErr error) {
	return initialize(ctx, db, worktreeID, lease, nil, "")
}

// InitializeWithWriter adds the required worktree writer check to exclusive
// lifecycle initialization. Product wiring must use this entry point under a
// maintenance pool owner; Initialize remains the isolated foundation entry.
func InitializeWithWriter(ctx context.Context, db *sql.DB, worktreeID string, lease ports.LifecycleLease, writer ports.IndexWriterLease, jobID string) (identity Identity, resultErr error) {
	if ctx == nil || writer == nil || !validUUID(jobID) {
		return Identity{}, ErrInvalidStore
	}
	guard, err := writer.HoldWriter(ctx, worktreeID, jobID)
	if err != nil {
		return Identity{}, err
	}
	if guard == nil {
		return Identity{}, ErrInvalidStore
	}
	defer func() { resultErr = errors.Join(resultErr, guard.Close()) }()
	return initialize(ctx, db, worktreeID, lease, writer, jobID)
}

func initialize(ctx context.Context, db *sql.DB, worktreeID string, lease ports.LifecycleLease, writer ports.IndexWriterLease, jobID string) (identity Identity, resultErr error) {
	if ctx == nil || !withinPoolLimit(db, 1) || lease == nil || !validUUID(worktreeID) {
		return Identity{}, ErrInvalidStore
	}
	if err := lease.Check(ctx, worktreeID, ports.ExclusiveLease); err != nil {
		return Identity{}, err
	}
	if writer != nil {
		if err := writer.CheckWriter(ctx, worktreeID, jobID); err != nil {
			return Identity{}, err
		}
	}
	conn, err := borrowConfigured(ctx, db, false)
	if err != nil {
		return Identity{}, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, false); err != nil {
			identity = Identity{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, previous, err := inspectStore(ctx, conn, worktreeID)
	if err != nil {
		return Identity{}, err
	}
	// Never alter journal mode on an unknown or too-new store.
	if err := requireWAL(ctx, conn, true); err != nil {
		return Identity{}, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return Identity{}, err
	}
	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				identity = Identity{}
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	currentVersion, currentIdentity, err := inspectStore(ctx, tx, worktreeID)
	if err != nil {
		return Identity{}, err
	}
	if currentVersion != version || currentIdentity != previous {
		return Identity{}, ErrStoreChanged
	}
	switch version {
	case 0:
		incarnation, err := newUUID()
		if err != nil {
			return Identity{}, err
		}
		if _, err := tx.ExecContext(ctx, migration.V1()); err != nil {
			return Identity{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO store_identity(singleton,worktree_id,incarnation) VALUES(1,?,?)`, worktreeID, incarnation); err != nil {
			return Identity{}, err
		}
		identity = Identity{WorktreeID: worktreeID, Incarnation: incarnation}
		if _, err := tx.ExecContext(ctx, migration.V2()); err != nil {
			return Identity{}, err
		}
	case 1:
		identity = previous
		if _, err := tx.ExecContext(ctx, migration.V2()); err != nil {
			return Identity{}, err
		}
	case 2:
		identity = previous
	default:
		return Identity{}, ErrUnrecognizedStore
	}
	finalVersion, finalIdentity, err := inspectStore(ctx, tx, worktreeID)
	if err != nil || finalVersion != supportedSchema || finalIdentity != identity {
		return Identity{}, errors.Join(ErrUnrecognizedStore, err)
	}
	if err := lease.Check(ctx, worktreeID, ports.ExclusiveLease); err != nil {
		return Identity{}, err
	}
	if writer != nil {
		if err := writer.CheckWriter(ctx, worktreeID, jobID); err != nil {
			return Identity{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Identity{}, errors.Join(ErrCommitUnknown, err)
	}
	committed = true
	return identity, nil
}

func inspectStore(ctx context.Context, db schemaQueryer, expectedWorktree string) (int, Identity, error) {
	var objects int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*'`).Scan(&objects); err != nil {
		return 0, Identity{}, err
	}
	if objects == 0 {
		return 0, Identity{}, nil
	}
	var kind string
	if err := db.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(type AS BLOB))<=64 THEN type END FROM sqlite_schema WHERE name='schema_meta'`).Scan(&kind); err != nil || kind != "table" {
		return 0, Identity{}, errors.Join(ErrUnrecognizedStore, err)
	}
	var version, minReader int
	if err := db.QueryRowContext(ctx, `SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&version, &minReader); err != nil {
		return 0, Identity{}, errors.Join(ErrUnrecognizedStore, err)
	}
	if version > supportedSchema || minReader > supportedSchema {
		return 0, Identity{}, ErrSchemaTooNew
	}
	if version < 1 || minReader != version {
		return 0, Identity{}, ErrUnrecognizedStore
	}
	if err := validateSchema(ctx, db, version); err != nil {
		return 0, Identity{}, err
	}
	var identity Identity
	if err := db.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(worktree_id AS BLOB))=36 THEN worktree_id END,CASE WHEN length(CAST(incarnation AS BLOB))=36 THEN incarnation END FROM store_identity WHERE singleton=1`).Scan(&identity.WorktreeID, &identity.Incarnation); err != nil {
		return 0, Identity{}, errors.Join(ErrInvalidIdentity, err)
	}
	if !validUUID(identity.WorktreeID) || !validUUID(identity.Incarnation) {
		return 0, Identity{}, ErrInvalidIdentity
	}
	if identity.WorktreeID != expectedWorktree {
		return 0, Identity{}, ErrWrongWorktree
	}
	return version, identity, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
