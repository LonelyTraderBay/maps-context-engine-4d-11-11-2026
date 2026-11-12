// Package catalog implements catalog use cases against the frozen schema. A
// caller supplies an already authorized SQLite database; this package does not
// discover databases, open target repositories, or manage lifecycle leases.
package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/catalog"
	_ "modernc.org/sqlite"
)

var (
	ErrUnrecognizedStore = errors.New("catalog schema is unrecognized")
	ErrSchemaTooNew      = errors.New("catalog schema is too new")
	ErrInvalidIdentity   = errors.New("catalog incarnation is invalid")
)

const supportedSchema = 1

// Initialize creates the frozen catalog schema and a random incarnation only
// for an empty database, or validates the version and existing incarnation.
// The schema and identity are committed together. On an existing but unknown
// database it refuses to write. The caller owns database path authorization,
// connection lifecycle, backup, and cross-process maintenance coordination.
func Initialize(ctx context.Context, db *sql.DB) (incarnation string, resultErr error) {
	return initialize(ctx, db, nil, nil, "")
}

// InitializeLeased requires exclusive maintenance ownership. The caller must
// retain the OS lease and all handles until the database is closed.
func InitializeLeased(ctx context.Context, db *sql.DB, lease ports.LifecycleLease, mutation ports.CatalogMutationLease, operationID string) (incarnation string, resultErr error) {
	if ctx == nil || db == nil || db.Stats().MaxOpenConnections != 1 || lease == nil || mutation == nil || !validUUID(operationID) {
		return "", ErrUnrecognizedStore
	}
	if err := lease.Check(ctx, "catalog", ports.ExclusiveLease); err != nil {
		return "", err
	}
	if err := mutation.CheckCatalogMutation(ctx, operationID); err != nil {
		return "", err
	}
	guard, err := mutation.HoldCatalogMutation(ctx, operationID)
	if err != nil {
		return "", err
	}
	if guard == nil {
		return "", ErrCatalogWriterRequired
	}
	defer func() { resultErr = errors.Join(resultErr, guard.Close()) }()
	return initialize(ctx, db, lease, mutation, operationID)
}

func initialize(ctx context.Context, db *sql.DB, lease ports.LifecycleLease, mutation ports.CatalogMutationLease, operationID string) (incarnation string, resultErr error) {
	if ctx == nil || db == nil {
		return "", ErrUnrecognizedStore
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", err
	}
	configured := false
	defer func() {
		var closeErr error
		if lease == nil {
			closeErr = conn.Close()
		} else {
			closeErr = closeCatalogConnection(ctx, conn, configured, false)
		}
		if closeErr != nil {
			incarnation = ""
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	if lease == nil {
		if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
			return "", err
		}
		var fk int
		if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
			return "", errors.Join(ErrUnrecognizedStore, err)
		}
	} else if err = configureCatalogConnection(ctx, conn, false, false); err != nil {
		return "", err
	}
	configured = true
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				incarnation = ""
				resultErr = errors.Join(resultErr, rollbackErr)
			}
		}
	}()
	var objects int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*'`).Scan(&objects); err != nil {
		return "", err
	}
	if objects == 0 {
		incarnation, err := newUUID()
		if err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, migration.V1()); err != nil {
			return "", err
		}
		if err = validateSchema(ctx, tx); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog_identity(singleton,incarnation) VALUES(1,?)`, incarnation); err != nil {
			return "", err
		}
		if lease != nil {
			if err = lease.Check(ctx, "catalog", ports.ExclusiveLease); err != nil {
				return "", err
			}
			if err = mutation.CheckCatalogMutation(ctx, operationID); err != nil {
				return "", err
			}
		}
		if err = tx.Commit(); err != nil {
			if lease != nil {
				return "", errors.Join(ErrCatalogCommitUnknown, err)
			}
			return "", err
		}
		committed = true
		return incarnation, nil
	}
	var metaType string
	if err = tx.QueryRowContext(ctx, `SELECT type FROM sqlite_schema WHERE name='schema_meta'`).Scan(&metaType); err != nil || metaType != "table" {
		return "", errors.Join(ErrUnrecognizedStore, err)
	}
	var version, minReader int
	if err = tx.QueryRowContext(ctx, `SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&version, &minReader); err != nil {
		return "", errors.Join(ErrUnrecognizedStore, err)
	}
	if version > supportedSchema || minReader > supportedSchema {
		return "", ErrSchemaTooNew
	}
	if version != supportedSchema || minReader < 1 {
		return "", ErrUnrecognizedStore
	}
	if err = validateSchema(ctx, tx); err != nil {
		return "", err
	}
	if err = tx.QueryRowContext(ctx, `SELECT incarnation FROM catalog_identity WHERE singleton=1`).Scan(&incarnation); err != nil {
		return "", errors.Join(ErrInvalidIdentity, err)
	}
	if !validUUID(incarnation) {
		return "", ErrInvalidIdentity
	}
	if lease != nil {
		if err = lease.Check(ctx, "catalog", ports.ExclusiveLease); err != nil {
			return "", err
		}
		if err = mutation.CheckCatalogMutation(ctx, operationID); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(); err != nil {
		if lease != nil {
			return "", errors.Join(ErrCatalogCommitUnknown, err)
		}
		return "", err
	}
	committed = true
	return incarnation, nil
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
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	if value == "00000000-0000-0000-0000-000000000000" {
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
