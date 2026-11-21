// Package records provides the R1 identity and read-only authority foundation
// for the durable records store. Record mutations and history belong to T047.
package records

import (
	"context"
	"database/sql"
	"errors"

	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/records"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/schemaref"
)

type schemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type schemaObject struct {
	Kind, Name, Table string
	SQL               sql.NullString
}

var referenceSchemas schemaref.Cache[schemaObject]

func buildReferenceSchema(ctx context.Context, version int) (objects []schemaObject, resultErr error) {
	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	reference.SetMaxOpenConns(1)
	defer func() { resultErr = errors.Join(resultErr, reference.Close()) }()
	if _, err := reference.ExecContext(ctx, migration.V1()); err != nil {
		return nil, err
	}
	if version == 2 {
		if _, err := reference.ExecContext(ctx, migration.V2()); err != nil {
			return nil, err
		}
	}
	return schemaObjects(ctx, reference)
}

// Cached expected DDL never replaces the fresh observation of the real store.
func validateSchema(ctx context.Context, actual schemaQueryer, version int) error {
	want, err := referenceSchemas.Get(ctx, version, buildReferenceSchema)
	if err != nil {
		return err
	}
	got, err := schemaObjects(ctx, actual)
	if err != nil {
		return err
	}
	if len(want) != len(got) {
		return ErrUnrecognizedStore
	}
	for i := range want {
		if want[i] != got[i] {
			return ErrUnrecognizedStore
		}
	}
	return nil
}

func schemaObjects(ctx context.Context, db schemaQueryer) (objects []schemaObject, resultErr error) {
	// The CASE expressions reject oversized corrupt metadata before a driver
	// materializes it. A malformed or unexpected schema is never migrated.
	rows, err := db.QueryContext(ctx, `SELECT CASE WHEN length(CAST(type AS BLOB))<=64 THEN type END,CASE WHEN length(CAST(name AS BLOB))<=1024 THEN name END,CASE WHEN length(CAST(tbl_name AS BLOB))<=1024 THEN tbl_name END,CASE WHEN length(CAST(sql AS BLOB))<=65536 THEN sql END,length(CAST(sql AS BLOB)) FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' ORDER BY type,name LIMIT 257`)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var item schemaObject
		var length sql.NullInt64
		if err := rows.Scan(&item.Kind, &item.Name, &item.Table, &item.SQL, &length); err != nil || length.Valid && length.Int64 > 65536 || len(objects) >= 256 {
			return nil, ErrUnrecognizedStore
		}
		objects = append(objects, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return objects, nil
}
