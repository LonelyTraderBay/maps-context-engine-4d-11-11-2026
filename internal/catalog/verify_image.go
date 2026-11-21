package catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// VerifyReadOnlyImage validates an already authenticated immutable image. The
// caller owns capture/receipt/path authority and its lifecycle lease.
// This function cannot initialize/migrate a schema or create a writable Store.
func VerifyReadOnlyImage(ctx context.Context, db *sql.DB) (string, error) {
	if ctx == nil || db == nil || db.Stats().MaxOpenConnections != 1 {
		return "", ErrUnrecognizedStore
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var readOnly int
	if err := conn.QueryRowContext(ctx, "PRAGMA query_only").Scan(&readOnly); err != nil || readOnly != 1 {
		return "", ErrUnrecognizedStore
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := validateSchema(ctx, tx); err != nil {
		return "", err
	}
	var version, minReader int
	if err := tx.QueryRowContext(ctx, "SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1").Scan(&version, &minReader); err != nil || version != supportedSchema || minReader < 1 || minReader > supportedSchema {
		return "", ErrUnrecognizedStore
	}
	var incarnation string
	if err := tx.QueryRowContext(ctx, "SELECT CASE WHEN length(CAST(incarnation AS BLOB))=36 THEN incarnation END FROM catalog_identity WHERE singleton=1").Scan(&incarnation); err != nil || !validUUID(incarnation) {
		return "", ErrInvalidIdentity
	}
	var integrity string
	if err := tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return "", ErrUnrecognizedStore
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return "", err
	}
	invalid := rows.Next()
	err = errors.Join(rows.Err(), rows.Close())
	if invalid || err != nil {
		return "", ErrUnrecognizedStore
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return incarnation, ctx.Err()
}

// OpenAuthenticatedImage opens a read-only view of an already authenticated
// checkpoint image. Its owner must verify the physical WAL image and head,
// supply an immutable pool, and retain the shared lifecycle lease.
func OpenAuthenticatedImage(ctx context.Context, db *sql.DB, lease ports.LifecycleLease) (*Store, error) {
	if ctx == nil || lease == nil {
		return nil, ErrUnrecognizedStore
	}
	if err := lease.Check(ctx, "catalog", ports.SharedLease); err != nil {
		return nil, err
	}
	incarnation, err := VerifyReadOnlyImage(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := lease.Check(ctx, "catalog", ports.SharedLease); err != nil {
		return nil, err
	}
	return &Store{db: db, incarnation: incarnation, lease: lease, image: true}, nil
}
