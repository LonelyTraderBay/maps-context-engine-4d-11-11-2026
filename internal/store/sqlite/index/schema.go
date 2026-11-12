package index

import (
	"context"
	"database/sql"
	"errors"

	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/index"
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

// The reference is an ephemeral in-memory schema used only for comparison;
// it is closed before return and never opens or owns an index-store pool.
func validateSchema(ctx context.Context, actual schemaQueryer, version int) (resultErr error) {
	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, reference.Close()) }()
	reference.SetMaxOpenConns(1)
	if _, err := reference.ExecContext(ctx, migration.V1()); err != nil {
		return err
	}
	if version == 2 {
		if _, err := reference.ExecContext(ctx, migration.V2()); err != nil {
			return err
		}
	}
	want, err := readSchema(ctx, reference)
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
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' ORDER BY type,name`)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.Kind, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, errors.Join(ErrUnrecognizedStore, err)
		}
		if len(objects) >= 256 || len(object.SQL.String) > 65536 {
			return nil, ErrUnrecognizedStore
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return objects, nil
}
