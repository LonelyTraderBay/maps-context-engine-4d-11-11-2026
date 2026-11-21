package index

import (
	"context"
	"database/sql"
	"errors"

	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/schemaref"
)

type schemaObject struct {
	Kind  string
	Name  string
	Table string
	SQL   sql.NullString
}

type schemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// The cached reference is derived only from migrations embedded in this
// binary. The real store is still read and compared on every validation.
// Errors and canceled builds are not cached; each caller receives a copy.
type schemaReferenceCache struct {
	values schemaref.Cache[schemaObject]
	addon  schemaref.Cache[schemaObject]
}

var referenceSchemas schemaReferenceCache

func (c *schemaReferenceCache) get(ctx context.Context, version int) ([]schemaObject, error) {
	if c == nil || ctx == nil || version < 1 || version > uiAddonSchema {
		return nil, ErrUnrecognizedStore
	}
	if version > supportedSchema {
		return c.addon.Get(ctx, version-supportedSchema, func(ctx context.Context, relative int) ([]schemaObject, error) {
			return buildReferenceSchema(ctx, relative+supportedSchema)
		})
	}
	return c.values.Get(ctx, version, buildReferenceSchema)
}

// The reference is an ephemeral in-memory schema used only for comparison;
// it is closed before return and never opens or owns an index-store pool.
func buildReferenceSchema(ctx context.Context, version int) (objects []schemaObject, resultErr error) {
	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, reference.Close()) }()
	reference.SetMaxOpenConns(1)
	if _, err := reference.ExecContext(ctx, migration.V1()); err != nil {
		return nil, err
	}
	for idx, ddl := range []string{migration.V2(), migration.V3(), migration.V4()} {
		if version >= idx+2 {
			if _, err := reference.ExecContext(ctx, ddl); err != nil {
				return nil, err
			}
		}
	}
	return readSchema(ctx, reference)
}

func validateSchema(ctx context.Context, actual schemaQueryer, version int) error {
	want, err := referenceSchemas.get(ctx, version)
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
	// Bound fields before the driver materializes them; legitimate SQLite
	// internal objects may have NULL SQL, so retain the original length too.
	rows, err := db.QueryContext(ctx, `SELECT CASE WHEN length(CAST(type AS BLOB))<=64 THEN type END,CASE WHEN length(CAST(name AS BLOB))<=1024 THEN name END,CASE WHEN length(CAST(tbl_name AS BLOB))<=1024 THEN tbl_name END,CASE WHEN length(CAST(sql AS BLOB))<=65536 THEN sql END,length(CAST(sql AS BLOB)) FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' ORDER BY 1,2 LIMIT 257`)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var object schemaObject
		var length sql.NullInt64
		if err := rows.Scan(&object.Kind, &object.Name, &object.Table, &object.SQL, &length); err != nil {
			return nil, errors.Join(ErrUnrecognizedStore, err)
		}
		if len(objects) >= 256 || length.Valid && length.Int64 > 65536 {
			return nil, ErrUnrecognizedStore
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return objects, nil
}
