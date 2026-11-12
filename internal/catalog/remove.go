package catalog

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrConfirmationRequired = errors.New("explicit confirmation is required")

// Remove marks a registered worktree removed and advances its binding revision.
// It never purges an index or deletes source, policy, profiles, or records.
// The caller must authenticate the administrator, obtain explicit confirmation,
// and hold the catalog lifecycle lease; this is not a public transport route.
func (s *Store) Remove(ctx context.Context, worktreeID string, expectedRevision int64, confirmed bool) (int64, error) {
	if !confirmed {
		return 0, ErrConfirmationRequired
	}
	if !validUUID(worktreeID) || expectedRevision < 1 || expectedRevision >= maxExactRevision {
		return 0, ErrRevisionConflict
	}
	next := expectedRevision + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := s.write(ctx, func(tx *sql.Tx) error {
		var status string
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT status,binding_revision FROM worktrees WHERE worktree_id=?`, worktreeID).Scan(&status, &revision); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrWorktreeUnavailable
			}
			return err
		}
		if status == "removed" {
			return ErrWorktreeUnavailable
		}
		if revision != expectedRevision {
			return ErrRevisionConflict
		}
		result, err := tx.ExecContext(ctx, `UPDATE worktrees SET status='removed',binding_revision=?,updated_at=? WHERE worktree_id=? AND binding_revision=? AND status!='removed'`, next, now, worktreeID, expectedRevision)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrRevisionConflict
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}
