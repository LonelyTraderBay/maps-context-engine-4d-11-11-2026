package catalog

import (
	"context"
	"database/sql"
	"errors"

	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/schemaref"
)

type schemaObject struct {
	Kind  string
	Name  string
	Table string
	SQL   string
}

type schemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

var referenceSchemas schemaref.Cache[schemaObject]

func buildReferenceSchema(ctx context.Context, _ int) (objects []schemaObject, resultErr error) {
	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, reference.Close()) }()
	reference.SetMaxOpenConns(1)
	if _, err = reference.ExecContext(ctx, migration.V1()); err != nil {
		return nil, err
	}
	return readSchema(ctx, reference)
}

// validateSchema reads the actual schema on every call. Only the isolated
// reference generated from the binary's embedded migration is cached.
func validateSchema(ctx context.Context, actual schemaQueryer) error {
	want, err := referenceSchemas.Get(ctx, 1, buildReferenceSchema)
	if err != nil {
		return err
	}
	got, err := readSchema(ctx, actual)
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

func readSchema(ctx context.Context, db schemaQueryer) (objects []schemaObject, resultErr error) {
	// Bound fields in SQLite before the driver materializes them in Go. Sort
	// the bounded values too, so an invalid oversized name is not a sort key.
	rows, err := db.QueryContext(ctx, `SELECT CASE WHEN length(CAST(type AS BLOB))<=64 THEN type END,CASE WHEN length(CAST(name AS BLOB))<=1024 THEN name END,CASE WHEN length(CAST(tbl_name AS BLOB))<=1024 THEN tbl_name END,CASE WHEN length(CAST(sql AS BLOB))<=65536 THEN sql END FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' ORDER BY 1,2 LIMIT 257`)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.Kind, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, errors.Join(ErrUnrecognizedStore, err)
		}
		if len(objects) >= 256 {
			return nil, ErrUnrecognizedStore
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return objects, nil
}
