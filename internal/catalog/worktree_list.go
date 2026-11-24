package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const maxLocalWorktreeRows = 4096

var ErrWorktreeListLimit = errors.New("local worktree list exceeds capacity")

// ListLocalWorktrees reads only root names already approved by the installation.
// Catalog IDs select rows and never grant authority over a root.
func (s *Store) ListLocalWorktrees(ctx context.Context, roots []string, worktree string, authorize func(context.Context) error) ([]model.LocalWorktree, error) {
	if ctx == nil || authorize == nil || len(roots) < 1 || len(roots) > 256 || worktree != "" && !validUUID(worktree) {
		return nil, ErrUnrecognizedStore
	}
	args := make([]any, 0, len(roots)+3)
	for _, root := range roots {
		if len(root) > 16384 || !utf8.ValidString(root) || !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return nil, ErrUnrecognizedStore
		}
		args = append(args, root)
	}
	args = append(args, worktree, worktree, maxLocalWorktreeRows+1)
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	result := []model.LocalWorktree{}
	err := s.read(ctx, func(tx *sql.Tx) error {
		params := strings.Repeat("?,", len(roots)-1) + "?"
		rows, err := tx.QueryContext(ctx, `SELECT
 CASE WHEN length(CAST(w.worktree_id AS BLOB))=36 THEN w.worktree_id END,
 CASE WHEN length(CAST(w.repo_id AS BLOB))=36 THEN w.repo_id END,
 w.canonical_root, w.status, w.binding_revision,
 CASE WHEN p.profile_id IS NULL THEN NULL WHEN length(CAST(p.profile_id AS BLOB))=64 THEN p.profile_id ELSE '' END
FROM worktrees w LEFT JOIN profiles p ON p.worktree_id=w.worktree_id
WHERE w.canonical_root IN (`+params+`) AND w.status!='removed' AND (?='' OR w.worktree_id=?)
ORDER BY w.worktree_id, p.profile_id LIMIT ?`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for i := 0; rows.Next(); i++ {
			if i >= maxLocalWorktreeRows {
				return ErrWorktreeListLimit
			}
			var row model.LocalWorktree
			var profile sql.NullString
			if err := rows.Scan(&row.WorktreeID, &row.RepoID, &row.Root, &row.Status, &row.BindingRevision, &profile); err != nil {
				return err
			}
			if !validUUID(row.WorktreeID) || !validUUID(row.RepoID) || row.BindingRevision < 1 || row.BindingRevision > maxExactRevision ||
				row.Status != "registered" && row.Status != "unavailable" || profile.Valid && !bindingHash(profile.String) {
				return ErrUnrecognizedStore
			}
			last := len(result) - 1
			if last < 0 || result[last].WorktreeID != row.WorktreeID {
				row.Profiles = []string{}
				result = append(result, row)
				last++
			}
			if profile.Valid {
				result[last].Profiles = append(result[last].Profiles, profile.String)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return authorize(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
