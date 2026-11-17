package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
)

// ReadWorktreeRoot observes a registered root for an already authenticated
// administrator, without guessing an analysis profile. It grants no access.
func (s *Store) ReadWorktreeRoot(ctx context.Context, worktree string, authorize func(context.Context) error) (root string, resultErr error) {
	if s == nil || s.lease == nil || ctx == nil || authorize == nil || !validUUID(worktree) {
		return "", ErrUnrecognizedStore
	}
	if err := authorize(ctx); err != nil {
		return "", err
	}
	err := s.read(ctx, func(tx *sql.Tx) error {
		if err := requireRegistered(ctx, tx, worktree); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(canonical_root AS BLOB)) BETWEEN 1 AND 16384 THEN canonical_root END FROM worktrees WHERE worktree_id=?`, worktree).Scan(&root); err != nil {
			return err
		}
		if !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return ErrUnrecognizedStore
		}
		return authorize(ctx)
	})
	if err != nil {
		return "", errors.Join(err, ctx.Err())
	}
	return root, nil
}
