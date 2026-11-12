package catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var (
	ErrCatalogChanged        = errors.New("catalog incarnation changed")
	ErrRevisionConflict      = errors.New("catalog revision conflict")
	ErrWorktreeUnavailable   = errors.New("worktree is not registered")
	ErrUnsupportedProfile    = errors.New("profile language is not supported in this phase")
	ErrCatalogWriterRequired = errors.New("catalog mutation lease is required")
	ErrCatalogCommitUnknown  = errors.New("catalog commit outcome is unknown; reconcile before retry")
)

// Store owns no database connection or lifecycle lease. Its caller owns both
// and must authorize each administrative operation before invoking it.
type Store struct {
	db          *sql.DB
	incarnation string
	lease       ports.LifecycleLease
	mutation    ports.CatalogMutationLease
	operationID string
}

// Open initializes or validates an already-authorized catalog database.
// A restored or replaced catalog must be opened again under its new incarnation.
func Open(ctx context.Context, db *sql.DB) (*Store, error) {
	incarnation, err := Initialize(ctx, db)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, incarnation: incarnation}, nil
}

func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) (resultErr error) {
	return s.transaction(ctx, true, fn)
}

func (s *Store) read(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.transaction(ctx, false, fn)
}

func (s *Store) transaction(ctx context.Context, writing bool, fn func(*sql.Tx) error) (resultErr error) {
	if s == nil || s.db == nil || ctx == nil || fn == nil {
		return ErrUnrecognizedStore
	}
	if s.lease != nil {
		if err := s.lease.Check(ctx, "catalog", ports.SharedLease); err != nil {
			return err
		}
		if s.db.Stats().MaxOpenConnections != 1 {
			return ErrUnrecognizedStore
		}
		if writing && s.mutation == nil {
			return ErrCatalogWriterRequired
		}
		if writing {
			if err := s.mutation.CheckCatalogMutation(ctx, s.operationID); err != nil {
				return err
			}
			guard, err := s.mutation.HoldCatalogMutation(ctx, s.operationID)
			if err != nil {
				return err
			}
			if guard == nil {
				return ErrCatalogWriterRequired
			}
			defer func() { resultErr = errors.Join(resultErr, guard.Close()) }()
		}
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	configured := false
	defer func() {
		if s.lease == nil {
			resultErr = errors.Join(resultErr, conn.Close())
		} else {
			resultErr = errors.Join(resultErr, closeCatalogConnection(ctx, conn, configured, !writing))
		}
	}()
	if s.lease == nil {
		if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
			return err
		}
		var fk int
		if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
			return errors.Join(ErrUnrecognizedStore, err)
		}
	} else if err = configureCatalogConnection(ctx, conn, true, !writing); err != nil {
		return err
	}
	configured = true
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: !writing})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				resultErr = errors.Join(resultErr, rollbackErr)
			}
		}
	}()
	if err = validateSchema(ctx, tx); err != nil {
		return err
	}
	var version, minReader int
	if err = tx.QueryRowContext(ctx, `SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&version, &minReader); err != nil {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	if version > supportedSchema || minReader > supportedSchema {
		return ErrSchemaTooNew
	}
	if version != supportedSchema || minReader < 1 {
		return ErrUnrecognizedStore
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT incarnation FROM catalog_identity WHERE singleton=1`).Scan(&current); err != nil {
		return errors.Join(ErrInvalidIdentity, err)
	}
	if current != s.incarnation {
		return ErrCatalogChanged
	}
	if err = fn(tx); err != nil {
		return err
	}
	if s.lease != nil {
		if err = s.lease.Check(ctx, "catalog", ports.SharedLease); err != nil {
			return err
		}
		if writing {
			if err = s.mutation.CheckCatalogMutation(ctx, s.operationID); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		if s.lease != nil && writing {
			return errors.Join(ErrCatalogCommitUnknown, err)
		}
		return err
	}
	committed = true
	return nil
}

func requireRegistered(ctx context.Context, tx *sql.Tx, worktreeID string) error {
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM worktrees WHERE worktree_id=?`, worktreeID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorktreeUnavailable
		}
		return err
	}
	if status != "registered" {
		return ErrWorktreeUnavailable
	}
	return nil
}
