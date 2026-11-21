package catalog

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"
)

var ErrBackupLimit = errors.New("catalog backup metadata limit exceeded")

// BackupRevisions contains identifiers/epochs only, never policy bodies,
// credentials or source. It is metadata for an offline backup, not an access
// grant, a new wire operation, or a promise of cross-worktree atomicity.
type BackupRevisions struct {
	CatalogIncarnation string                   `json:"catalog_incarnation"`
	Worktrees          []BackupWorktreeRevision `json:"worktrees"`
}
type BackupWorktreeRevision struct {
	WorktreeID      string                  `json:"worktree_id"`
	Status          string                  `json:"status"`
	BindingRevision int64                   `json:"binding_revision"`
	PolicyDigest    *string                 `json:"policy_digest"`
	PolicyEpoch     *int64                  `json:"policy_epoch"`
	Profiles        []BackupProfileRevision `json:"profiles"`
}
type BackupProfileRevision struct {
	ProfileName string `json:"profile_name"`
	ProfileID   string `json:"profile_id"`
	Revision    int64  `json:"revision"`
}

// ReadBackupRevisions observes one registered worktree, or the complete
// catalog when worktree is empty, in one bounded read transaction. Caller
// authorization covers the selected scope and is rechecked before return.
func (s *Store) ReadBackupRevisions(ctx context.Context, worktree string, authorize func(context.Context) error) (out BackupRevisions, resultErr error) {
	if s == nil || s.lease == nil || ctx == nil || authorize == nil || worktree != "" && !validUUID(worktree) {
		return out, ErrUnrecognizedStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return out, ErrUnrecognizedStore
	}
	if err := authorize(ctx); err != nil {
		return out, err
	}
	out = BackupRevisions{CatalogIncarnation: s.incarnation, Worktrees: []BackupWorktreeRevision{}}
	err := s.read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(w.worktree_id AS BLOB))=36 THEN w.worktree_id END,w.status,w.binding_revision,p.policy_digest,p.policy_epoch FROM worktrees w LEFT JOIN worktree_policy_heads p ON p.worktree_id=w.worktree_id WHERE (?='' OR w.worktree_id=?) ORDER BY w.worktree_id LIMIT 4097`, worktree, worktree)
		if err != nil {
			return err
		}
		positions := map[string]int{}
		for rows.Next() {
			var entry BackupWorktreeRevision
			var digest sql.NullString
			var epoch sql.NullInt64
			if err := rows.Scan(&entry.WorktreeID, &entry.Status, &entry.BindingRevision, &digest, &epoch); err != nil {
				_ = rows.Close()
				return err
			}
			if len(out.Worktrees) >= 4096 {
				_ = rows.Close()
				return ErrBackupLimit
			}
			if !validUUID(entry.WorktreeID) || entry.BindingRevision < 1 || entry.BindingRevision > maxExactRevision || entry.Status != "registered" && entry.Status != "unavailable" && entry.Status != "removed" || digest.Valid != epoch.Valid || digest.Valid && (!bindingHash(digest.String) || epoch.Int64 < 1 || epoch.Int64 > maxExactRevision) {
				_ = rows.Close()
				return ErrUnrecognizedStore
			}
			if worktree != "" && entry.Status != "registered" {
				_ = rows.Close()
				return ErrWorktreeUnavailable
			}
			if digest.Valid {
				entry.PolicyDigest = &digest.String
				entry.PolicyEpoch = &epoch.Int64
			}
			entry.Profiles = []BackupProfileRevision{}
			positions[entry.WorktreeID] = len(out.Worktrees)
			out.Worktrees = append(out.Worktrees, entry)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if worktree != "" && len(out.Worktrees) != 1 {
			return ErrWorktreeUnavailable
		}
		rows, err = tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(worktree_id AS BLOB))=36 THEN worktree_id END,CASE WHEN length(CAST(profile_name AS BLOB)) BETWEEN 1 AND 512 THEN profile_name END,profile_id,revision FROM profile_heads WHERE (?='' OR worktree_id=?) ORDER BY worktree_id,profile_name LIMIT 16385`, worktree, worktree)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var id string
			var entry BackupProfileRevision
			if err := rows.Scan(&id, &entry.ProfileName, &entry.ProfileID, &entry.Revision); err != nil {
				_ = rows.Close()
				return err
			}
			count++
			if count > 16384 {
				_ = rows.Close()
				return ErrBackupLimit
			}
			position, ok := positions[id]
			if !ok || !utf8.ValidString(entry.ProfileName) || utf8.RuneCountInString(entry.ProfileName) > 128 || !bindingHash(entry.ProfileID) || entry.Revision < 1 || entry.Revision > maxExactRevision {
				_ = rows.Close()
				return ErrUnrecognizedStore
			}
			out.Worktrees[position].Profiles = append(out.Worktrees[position].Profiles, entry)
		}
		return errors.Join(rows.Err(), rows.Close(), authorize(ctx))
	})
	if err != nil {
		return BackupRevisions{}, err
	}
	if !out.ValidFor(worktree) {
		return BackupRevisions{}, ErrUnrecognizedStore
	}
	return out, nil
}

func (b BackupRevisions) ValidFor(worktree string) bool {
	if !validUUID(b.CatalogIncarnation) || b.Worktrees == nil || len(b.Worktrees) > 4096 || worktree != "" && (len(b.Worktrees) != 1 || b.Worktrees[0].WorktreeID != worktree) {
		return false
	}
	previous := ""
	profiles := 0
	for _, entry := range b.Worktrees {
		if !validUUID(entry.WorktreeID) || entry.WorktreeID <= previous || entry.BindingRevision < 1 || entry.BindingRevision > maxExactRevision || entry.Status != "registered" && entry.Status != "unavailable" && entry.Status != "removed" || worktree != "" && entry.Status != "registered" || (entry.PolicyDigest == nil) != (entry.PolicyEpoch == nil) || entry.Profiles == nil {
			return false
		}
		if entry.PolicyDigest != nil && (!bindingHash(*entry.PolicyDigest) || *entry.PolicyEpoch < 1 || *entry.PolicyEpoch > maxExactRevision) {
			return false
		}
		previous = entry.WorktreeID
		lastName := ""
		for _, head := range entry.Profiles {
			profiles++
			if profiles > 16384 || !utf8.ValidString(head.ProfileName) || utf8.RuneCountInString(head.ProfileName) < 1 || utf8.RuneCountInString(head.ProfileName) > 128 || head.ProfileName <= lastName || !bindingHash(head.ProfileID) || head.Revision < 1 || head.Revision > maxExactRevision {
				return false
			}
			lastName = head.ProfileName
		}
	}
	return true
}
