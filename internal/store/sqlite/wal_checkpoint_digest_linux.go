//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"

	modsqlite "modernc.org/sqlite"
)

const checkpointImageMaxBytes int64 = 16 << 20

// checkpointImageDigest describes an experimental SQLite online-backup image.
// A separate authority must bind this value to the latest acknowledged commit,
// store incarnation and sequence. It is not itself an authenticated receipt.
type checkpointImageDigest struct {
	SQLiteVersion string
	PageSize      int64
	ImageSize     int64
	SHA256        [sha256.Size]byte
}

// captureCheckpointImage reads a bounded, pinned SQLite snapshot and hashes a
// backup made into memory. The caller owns the connection and must ensure it
// is a securely opened, authorized store with the appropriate lifecycle lease;
// no other goroutine may use this connection until the function returns. This
// helper neither opens a state path nor persists or authenticates a receipt.
// Different SQLite versions, legitimate writes or other storage changes can
// change the image digest. The byte cap is not a hard process-memory limit.
// Backup/Serialize calls do not accept a context: cancellation is checked
// between steps but cannot interrupt a blocked driver call. This is not an
// opener or a product recovery protocol.
func captureCheckpointImage(ctx context.Context, conn *sql.Conn, maxBytes int64) (out checkpointImageDigest, resultErr error) {
	if ctx == nil || conn == nil || maxBytes < 1 || maxBytes > checkpointImageMaxBytes {
		return out, ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				resultErr = errors.Join(resultErr, rollbackErr)
			}
		}
		if resultErr != nil {
			out = checkpointImageDigest{}
		}
	}()
	var pages, pageSize int64
	if err = tx.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&out.SQLiteVersion); err != nil {
		return out, err
	}
	if pages < 1 || pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 || pages > maxBytes/pageSize || out.SQLiteVersion == "" {
		return out, ErrStateLimit
	}
	out.PageSize, out.ImageSize = pageSize, pages*pageSize
	if err = backupPinnedCheckpointImage(ctx, conn, &out, maxBytes); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	committed = true
	return out, nil
}

// backupPinnedCheckpointImage must run on the same exclusively owned
// connection while its caller's read transaction and page metadata remain
// pinned. The capture helper above establishes those preconditions.
func backupPinnedCheckpointImage(ctx context.Context, conn *sql.Conn, out *checkpointImageDigest, maxBytes int64) error {
	if ctx == nil || conn == nil || out == nil || maxBytes < 1 || maxBytes > checkpointImageMaxBytes || out.ImageSize < 1 || out.ImageSize > maxBytes || out.PageSize < 512 || out.PageSize > 65536 || out.PageSize&(out.PageSize-1) != 0 || out.ImageSize%out.PageSize != 0 || out.SQLiteVersion == "" {
		return ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return conn.Raw(func(raw any) (innerErr error) {
		source, ok := raw.(interface {
			NewBackup(string) (*modsqlite.Backup, error)
		})
		if !ok {
			return ErrStateUnsafe
		}
		backup, err := source.NewBackup(":memory:")
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
			if err = ctx.Err(); err != nil {
				return err
			}
			more, stepErr := backup.Step(64)
			if stepErr != nil {
				return stepErr
			}
			if !more {
				break
			}
		}
		destination, err := backup.Commit()
		active = false // Commit finishes the backup even when it returns an error.
		if err != nil {
			return err
		}
		if destination == nil {
			return ErrStateUnsafe
		}
		defer func() { innerErr = errors.Join(innerErr, destination.Close()) }()
		serial, ok := destination.(interface{ Serialize() ([]byte, error) })
		if !ok {
			return ErrStateUnsafe
		}
		image, err := serial.Serialize()
		if err != nil {
			return err
		}
		if int64(len(image)) != out.ImageSize || int64(len(image)) > maxBytes {
			return ErrStateChanged
		}
		out.SHA256 = sha256.Sum256(image)
		return ctx.Err()
	})
}
