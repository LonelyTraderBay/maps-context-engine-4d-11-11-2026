package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

// ProbeMemory checks the linked SQLite and FTS5 without opening a state or
// target database. All statements are fixed and run only in an in-memory DB.
func ProbeMemory(ctx context.Context) (version string, fts bool, resultErr error) {
	if ctx == nil {
		return "", false, errors.New("SQLite probe unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return "", false, err
	}
	db.SetMaxOpenConns(1)
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	if err := db.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&version); err != nil {
		return "", false, err
	}
	var found int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_module_list WHERE name='fts5'`).Scan(&found); err != nil {
		return "", false, err
	}
	if found == 0 {
		return version, false, nil
	}
	if _, err := db.ExecContext(ctx, `CREATE VIRTUAL TABLE fts_probe USING fts5(body)`); err != nil {
		return "", false, err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fts_probe(body) VALUES('mcefixture')`); err != nil {
		return "", false, err
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fts_probe WHERE fts_probe MATCH 'mcefixture'`).Scan(&count); err != nil || count != 1 {
		return "", false, errors.Join(errors.New("SQLite FTS probe failed"), err)
	}
	return version, true, nil
}
