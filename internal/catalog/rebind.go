package catalog

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

type rebindState struct {
	repoID         string
	commonPath     string
	commonIdentity string
	status         string
	revision       int64
	policyJSON     string
	policyDigest   string
	policyEpoch    int64
}

// RepoLinkImpact is an exact worktree revision and status presented to the
// administrator for a shared-repository identity link. Removed worktrees are
// retained unchanged; other affected worktrees become unavailable until they
// are separately revalidated or rebound.
type RepoLinkImpact struct {
	WorktreeID      string
	BindingRevision int64
	Status          string
}

const maxRepoLinkImpacts = 1000

// PreviewRepoLink reads the target revision and the complete bounded set of
// potentially affected worktrees. The caller must authorize access before
// presenting this metadata, and must confirm and recheck it before mutation.
func (s *Store) PreviewRepoLink(ctx context.Context, worktreeID string) (int64, []RepoLinkImpact, error) {
	if !validUUID(worktreeID) {
		return 0, nil, ErrWorktreeUnavailable
	}
	var revision int64
	var impacts []RepoLinkImpact
	err := s.write(ctx, func(tx *sql.Tx) error {
		state, err := readRebindState(ctx, tx, worktreeID)
		if err != nil {
			return err
		}
		if state.status == "removed" {
			return ErrWorktreeUnavailable
		}
		revision = state.revision
		impacts, err = readLinkedWorktrees(ctx, tx, state.repoID, worktreeID)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	return revision, impacts, nil
}

// Rebind explicitly updates the root for an existing worktree. A different
// Git common-directory identity gets its own repo ID, unless that identity is
// already registered. A conflicting path with different identity is refused;
// it is never silently remapped. The caller must authorize the administrator
// and roots, hold the lifecycle lease, and obtain confirmation. No target file
// is changed by this operation.
func (s *Store) Rebind(ctx context.Context, worktreeID string, expectedRevision int64, confirmed bool, worktree, common *paths.Root, linkedRelative string) (int64, error) {
	return s.rebind(ctx, worktreeID, expectedRevision, confirmed, false, nil, worktree, common, linkedRelative)
}

// RebindWithExplicitRepoLink additionally confirms that a new common-dir
// identity at the same path may be linked to this existing repo ID. A separate
// confirmation is required because rebind alone does not imply a repo link.
// It refuses the link if any other worktree refers to that repo, including a
// removed worktree, so their identity is never silently changed. The caller
// retains the same authorization and lifecycle responsibilities as Rebind.
func (s *Store) RebindWithExplicitRepoLink(ctx context.Context, worktreeID string, expectedRevision int64, confirmed, confirmedRepoLink bool, worktree, common *paths.Root, linkedRelative string) (int64, error) {
	if !confirmedRepoLink {
		return 0, ErrConfirmationRequired
	}
	return s.rebind(ctx, worktreeID, expectedRevision, confirmed, true, nil, worktree, common, linkedRelative)
}

// RebindWithSharedRepoLink requires a separate explicit link confirmation and
// the complete, authorized list of other worktrees and their current revisions
// and statuses. It atomically makes live affected worktrees unavailable and
// advances their binding revisions. It does not delete their data or sources.
// The caller must authorize every affected worktree and hold the lifecycle
// lease. This is an internal use case, not a transport or implicit permission.
func (s *Store) RebindWithSharedRepoLink(ctx context.Context, worktreeID string, expectedRevision int64, confirmed, confirmedRepoLink bool, impacts []RepoLinkImpact, worktree, common *paths.Root, linkedRelative string) (int64, error) {
	if !confirmedRepoLink {
		return 0, ErrConfirmationRequired
	}
	if len(impacts) > maxRepoLinkImpacts {
		return 0, ErrRootRebindRequired
	}
	return s.rebind(ctx, worktreeID, expectedRevision, confirmed, true, impacts, worktree, common, linkedRelative)
}

func (s *Store) rebind(ctx context.Context, worktreeID string, expectedRevision int64, confirmed, confirmedRepoLink bool, impacts []RepoLinkImpact, worktree, common *paths.Root, linkedRelative string) (int64, error) {
	if !confirmed {
		return 0, ErrConfirmationRequired
	}
	if !validUUID(worktreeID) || expectedRevision < 1 || expectedRevision >= maxExactRevision {
		return 0, ErrRevisionConflict
	}
	if ctx == nil || worktree == nil || common == nil {
		return 0, ErrInvalidGitMetadata
	}
	var snapshot rebindState
	if err := s.write(ctx, func(tx *sql.Tx) error {
		state, err := readRebindState(ctx, tx, worktreeID)
		if err != nil {
			return err
		}
		if state.status == "removed" {
			return ErrWorktreeUnavailable
		}
		if state.revision != expectedRevision {
			return ErrRevisionConflict
		}
		snapshot = state
		return nil
	}); err != nil {
		return 0, err
	}
	policy, err := model.DecodePolicyConfigJSON([]byte(snapshot.policyJSON), maxAdminBytes)
	if err != nil {
		return 0, errors.Join(ErrUnrecognizedStore, err)
	}
	digest, err := model.PolicyDigest(policy, maxAdminBytes)
	if err != nil || digest != snapshot.policyDigest {
		return 0, errors.Join(ErrUnrecognizedStore, err)
	}
	worktreePath, worktreeIdentity, err := directoryIdentity(worktree)
	if err != nil {
		return 0, err
	}
	commonPath, commonIdentity, err := directoryIdentity(common)
	if err != nil {
		return 0, err
	}
	allowed, err := policyContainsRoot(policy.AllowedRoots, worktree, worktreePath)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, ErrRootNotAllowed
	}
	if err = verifyCommonGitDirectory(common); err != nil {
		return 0, errors.Join(ErrInvalidGitMetadata, err)
	}
	if err = verifyRegistrationBinding(worktree, common, linkedRelative, worktreePath, worktreeIdentity, commonPath, commonIdentity); err != nil {
		return 0, err
	}
	newRepoCandidate, err := newUUID()
	if err != nil {
		return 0, err
	}
	next := expectedRevision + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readRebindState(ctx, tx, worktreeID)
		if err != nil {
			return err
		}
		if current != snapshot {
			return ErrRevisionConflict
		}
		var other string
		err = tx.QueryRowContext(ctx, `SELECT worktree_id FROM worktrees WHERE worktree_id!=? AND (canonical_root=? OR filesystem_identity=?) LIMIT 1`, worktreeID, worktreePath, worktreeIdentity).Scan(&other)
		if err == nil {
			return ErrRegistrationExists
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		targetRepoID := snapshot.repoID
		impactsApplied := false
		if commonIdentity == snapshot.commonIdentity && commonPath != snapshot.commonPath {
			result, err := tx.ExecContext(ctx, `UPDATE repositories SET common_dir=? WHERE repo_id=? AND common_dir=? AND filesystem_identity=?`, commonPath, snapshot.repoID, snapshot.commonPath, commonIdentity)
			if err != nil {
				return err
			}
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				return errors.Join(ErrRevisionConflict, err)
			}
		} else if commonIdentity != snapshot.commonIdentity {
			var existingPath string
			err := tx.QueryRowContext(ctx, `SELECT repo_id,common_dir FROM repositories WHERE filesystem_identity=?`, commonIdentity).Scan(&targetRepoID, &existingPath)
			if errors.Is(err, sql.ErrNoRows) {
				var existingID string
				err = tx.QueryRowContext(ctx, `SELECT repo_id FROM repositories WHERE common_dir=?`, commonPath).Scan(&existingID)
				if err == nil {
					if !confirmedRepoLink || existingID != snapshot.repoID || commonPath != snapshot.commonPath {
						return ErrRootRebindRequired
					}
					if err = invalidateLinkedWorktrees(ctx, tx, snapshot.repoID, worktreeID, impacts, now); err != nil {
						return err
					}
					impactsApplied = true
					updated, err := tx.ExecContext(ctx, `UPDATE repositories SET filesystem_identity=? WHERE repo_id=? AND common_dir=? AND filesystem_identity=?`, commonIdentity, snapshot.repoID, snapshot.commonPath, snapshot.commonIdentity)
					if err != nil {
						return err
					}
					if n, err := updated.RowsAffected(); err != nil || n != 1 {
						return errors.Join(ErrRevisionConflict, err)
					}
					targetRepoID = snapshot.repoID
				} else if errors.Is(err, sql.ErrNoRows) {
					targetRepoID = newRepoCandidate
					if _, err = tx.ExecContext(ctx, `INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,?,?,?)`, targetRepoID, commonPath, commonIdentity, now); err != nil {
						return err
					}
				} else {
					return err
				}
			} else if err != nil {
				return err
			} else if existingPath != commonPath {
				return ErrRootRebindRequired
			}
		}
		if len(impacts) != 0 && !impactsApplied {
			return ErrRootRebindRequired
		}
		result, err := tx.ExecContext(ctx, `UPDATE worktrees SET repo_id=?,canonical_root=?,filesystem_identity=?,binding_revision=?,status='registered',updated_at=? WHERE worktree_id=? AND binding_revision=? AND status!='removed'`, targetRepoID, worktreePath, worktreeIdentity, next, now, worktreeID, expectedRevision)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return errors.Join(ErrRevisionConflict, err)
		}
		return verifyRegistrationBinding(worktree, common, linkedRelative, worktreePath, worktreeIdentity, commonPath, commonIdentity)
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}

func invalidateLinkedWorktrees(ctx context.Context, tx *sql.Tx, repoID, worktreeID string, impacts []RepoLinkImpact, now string) error {
	if len(impacts) > maxRepoLinkImpacts {
		return ErrRootRebindRequired
	}
	expected := make(map[string]RepoLinkImpact, len(impacts))
	for _, item := range impacts {
		if !validUUID(item.WorktreeID) || item.WorktreeID == worktreeID || item.BindingRevision < 1 || item.BindingRevision >= maxExactRevision || (item.Status != "registered" && item.Status != "unavailable" && item.Status != "removed") {
			return ErrRevisionConflict
		}
		if _, exists := expected[item.WorktreeID]; exists {
			return ErrRevisionConflict
		}
		expected[item.WorktreeID] = item
	}
	actual, err := readLinkedWorktrees(ctx, tx, repoID, worktreeID)
	if err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return ErrRootRebindRequired
	}
	for _, item := range actual {
		if want, ok := expected[item.WorktreeID]; !ok || item != want {
			return ErrRevisionConflict
		}
		if item.Status == "removed" {
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE worktrees SET status='unavailable',binding_revision=?,updated_at=? WHERE worktree_id=? AND repo_id=? AND status=? AND binding_revision=?`, item.BindingRevision+1, now, item.WorktreeID, repoID, item.Status, item.BindingRevision)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return errors.Join(ErrRevisionConflict, err)
		}
	}
	return nil
}

func readLinkedWorktrees(ctx context.Context, tx *sql.Tx, repoID, worktreeID string) ([]RepoLinkImpact, error) {
	rows, err := tx.QueryContext(ctx, `SELECT worktree_id,binding_revision,status FROM worktrees WHERE repo_id=? AND worktree_id!=? ORDER BY worktree_id LIMIT ?`, repoID, worktreeID, maxRepoLinkImpacts+1)
	if err != nil {
		return nil, err
	}
	actual := make([]RepoLinkImpact, 0)
	for rows.Next() {
		var item RepoLinkImpact
		if err := rows.Scan(&item.WorktreeID, &item.BindingRevision, &item.Status); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		if !validUUID(item.WorktreeID) || item.BindingRevision < 1 || item.BindingRevision >= maxExactRevision || (item.Status != "registered" && item.Status != "unavailable" && item.Status != "removed") {
			return nil, errors.Join(ErrUnrecognizedStore, rows.Close())
		}
		actual = append(actual, item)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	if len(actual) > maxRepoLinkImpacts {
		return nil, ErrRootRebindRequired
	}
	return actual, nil
}

func readRebindState(ctx context.Context, tx *sql.Tx, worktreeID string) (rebindState, error) {
	var state rebindState
	err := tx.QueryRowContext(ctx, `SELECT w.repo_id,r.common_dir,r.filesystem_identity,w.status,w.binding_revision,p.config_json,h.policy_digest,h.policy_epoch
		FROM worktrees w JOIN repositories r ON r.repo_id=w.repo_id
		JOIN worktree_policy_heads h ON h.worktree_id=w.worktree_id
		JOIN policies p ON p.worktree_id=h.worktree_id AND p.policy_digest=h.policy_digest
		WHERE w.worktree_id=?`, worktreeID).Scan(&state.repoID, &state.commonPath, &state.commonIdentity, &state.status, &state.revision, &state.policyJSON, &state.policyDigest, &state.policyEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrWorktreeUnavailable
	}
	return state, err
}
