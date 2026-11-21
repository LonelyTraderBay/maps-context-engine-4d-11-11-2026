package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"

	modsqlite "modernc.org/sqlite"
)

const backupImageMaxBytes int64 = 8 << 30

type backupImage struct {
	SQLiteVersion string
	Pages         int64
	PageSize      int64
	Bytes         int64
}

// copyPinnedSQLiteBackup is a private driver primitive, not an opener or an
// authorization boundary. The offline owner must supply the verified source
// connection under exclusive lifecycle, a fresh approved destination URI and
// explicit capacity budget. It must close handles, verify schema/identity and
// fsync/hash the resulting file before publishing a backup manifest.
//
// No source main/WAL descriptors are opened here. Step checks cancellation at
// 64-page boundaries; the driver's individual calls have no context argument.
func copyPinnedSQLiteBackup(ctx context.Context, conn *sql.Conn, destinationURI string, maxBytes int64) (out backupImage, resultErr error) {
	if ctx == nil || conn == nil || destinationURI == "" || maxBytes < 1 || maxBytes > backupImageMaxBytes {
		return out, ErrStateUnsafe
	}
	if _, ok := ctx.Deadline(); !ok {
		return out, ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, err)
		}
		if resultErr != nil {
			out = backupImage{}
		}
	}()
	for _, probe := range []struct {
		query string
		value any
	}{
		{"PRAGMA page_count", &out.Pages}, {"PRAGMA page_size", &out.PageSize}, {"SELECT sqlite_version()", &out.SQLiteVersion},
	} {
		if err := tx.QueryRowContext(ctx, probe.query).Scan(probe.value); err != nil {
			return out, err
		}
	}
	if out.Pages < 1 || out.PageSize < 512 || out.PageSize > 65536 || out.PageSize&(out.PageSize-1) != 0 || out.Pages > maxBytes/out.PageSize || out.SQLiteVersion == "" {
		return out, ErrStateLimit
	}
	out.Bytes = out.Pages * out.PageSize
	err = conn.Raw(func(raw any) (innerErr error) {
		source, ok := raw.(interface {
			NewBackup(string) (*modsqlite.Backup, error)
		})
		if !ok {
			return ErrStateUnsupported
		}
		backup, err := source.NewBackup(destinationURI)
		if err != nil {
			return err
		}
		if backup == nil {
			return ErrStateUnsafe
		}
		active := true
		defer func() {
			if active {
				innerErr = errors.Join(innerErr, backup.Finish())
			}
		}()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(64)
			if err != nil {
				return err
			}
			if int64(backup.PageCount()) != out.Pages {
				return ErrStateChanged
			}
			if !more {
				break
			}
		}
		destination, err := backup.Commit()
		active = false
		if err != nil {
			return err
		}
		if destination == nil {
			return ErrStateUnsafe
		}
		defer func() { innerErr = errors.Join(innerErr, destination.Close()) }()
		// A completed artifact must be a standalone main file. This changes only
		// the fresh destination, never the source's WAL/checkpoint configuration.
		query, ok := destination.(driver.QueryerContext)
		if !ok {
			return ErrStateUnsupported
		}
		rows, err := query.QueryContext(ctx, "PRAGMA journal_mode=DELETE", nil)
		if err != nil {
			return err
		}
		defer func() { innerErr = errors.Join(innerErr, rows.Close()) }()
		if len(rows.Columns()) != 1 {
			return ErrStateUnsafe
		}
		cells := make([]driver.Value, 1)
		if err := rows.Next(cells); err != nil {
			return err
		}
		if cells[0] != "delete" {
			return ErrStateUnsafe
		}
		if err := rows.Next(cells); err != io.EOF {
			return errors.Join(ErrStateUnsafe, err)
		}
		return ctx.Err()
	})
	return out, err
}
