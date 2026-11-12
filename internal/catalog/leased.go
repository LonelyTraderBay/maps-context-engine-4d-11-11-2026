package catalog

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// OpenExistingLeased validates an existing catalog under a shared lifecycle
// lease. It cannot create or migrate a schema. The caller owns the single
// bounded pool, keeps the lease attached until all handles close, and provides
// path and principal authorization.
func OpenExistingLeased(ctx context.Context, db *sql.DB, lease ports.LifecycleLease) (store *Store, resultErr error) {
	if ctx == nil || db == nil || lease == nil || db.Stats().MaxOpenConnections != 1 {
		return nil, ErrUnrecognizedStore
	}
	if err := lease.Check(ctx, "catalog", ports.SharedLease); err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	configured := false
	defer func() {
		closeErr := closeCatalogConnection(ctx, conn, configured, true)
		if closeErr != nil {
			store = nil
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	if err := configureCatalogConnection(ctx, conn, true, true); err != nil {
		return nil, err
	}
	configured = true
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var version, minReader int
	if err := tx.QueryRowContext(ctx, `SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&version, &minReader); err != nil {
		return nil, errors.Join(ErrUnrecognizedStore, err)
	}
	if version > supportedSchema || minReader > supportedSchema {
		return nil, ErrSchemaTooNew
	}
	if version != supportedSchema || minReader < 1 {
		return nil, ErrUnrecognizedStore
	}
	if err := validateSchema(ctx, tx); err != nil {
		return nil, err
	}
	var incarnation string
	if err := tx.QueryRowContext(ctx, `SELECT incarnation FROM catalog_identity WHERE singleton=1`).Scan(&incarnation); err != nil || !validUUID(incarnation) {
		return nil, errors.Join(ErrInvalidIdentity, err)
	}
	if err := lease.Check(ctx, "catalog", ports.SharedLease); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Store{db: db, incarnation: incarnation, lease: lease}, nil
}

func closeCatalogConnection(ctx context.Context, conn *sql.Conn, configured, readOnly bool) error {
	var closeErr error
	if configured && readOnly {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		_, closeErr = conn.ExecContext(cleanup, "PRAGMA query_only=OFF")
		cancel()
	}
	if closeErr != nil || !configured {
		rawErr := conn.Raw(func(any) error { return driver.ErrBadConn })
		if !errors.Is(rawErr, driver.ErrBadConn) && !errors.Is(rawErr, sql.ErrConnDone) {
			closeErr = errors.Join(closeErr, rawErr)
		}
		return closeErr
	}
	return conn.Close()
}

// WithMutation binds a short-lived catalog mutation lock to a store view.
// The original view remains unbound and cannot perform transactional calls.
func (s *Store) WithMutation(ctx context.Context, mutation ports.CatalogMutationLease, operationID string) (*Store, error) {
	if s == nil || s.lease == nil || ctx == nil || mutation == nil || !validUUID(operationID) {
		return nil, ErrCatalogWriterRequired
	}
	if err := s.lease.Check(ctx, "catalog", ports.SharedLease); err != nil {
		return nil, err
	}
	if err := mutation.CheckCatalogMutation(ctx, operationID); err != nil {
		return nil, err
	}
	return &Store{db: s.db, incarnation: s.incarnation, lease: s.lease, mutation: mutation, operationID: operationID}, nil
}

func configureCatalogConnection(ctx context.Context, conn *sql.Conn, requireWAL, readOnly bool) error {
	for _, statement := range []string{"PRAGMA query_only=OFF", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=FULL"} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if requireWAL {
		var mode string
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
			return errors.Join(ErrUnrecognizedStore, err)
		}
	}
	if readOnly {
		if _, err := conn.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
			return err
		}
	}
	for _, check := range []struct {
		query string
		want  int
	}{
		{"PRAGMA foreign_keys", 1}, {"PRAGMA busy_timeout", 5000}, {"PRAGMA synchronous", 2},
	} {
		var actual int
		if err := conn.QueryRowContext(ctx, check.query).Scan(&actual); err != nil || actual != check.want {
			return errors.Join(ErrUnrecognizedStore, err)
		}
	}
	var queryOnly int
	if err := conn.QueryRowContext(ctx, "PRAGMA query_only").Scan(&queryOnly); err != nil || (queryOnly == 1) != readOnly {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	return nil
}
