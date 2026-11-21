package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrProfileUnavailable = errors.New("profile is not available for worktree")

// Binding contains a short catalog observation, not a filesystem proof. Before
// using paths the caller must open and verify the registered root identities
// and enforce the observed policy. A broader operation must postcheck these
// revisions and the catalog incarnation before serializing a response.
type Binding struct {
	CatalogIncarnation string
	WorktreeID         string
	RepoID             string
	CanonicalRoot      string
	RootIdentity       string
	CommonDirectory    string
	CommonIdentity     string
	BindingRevision    int64
	PolicyDigest       string
	PolicyEpoch        int64
	Policy             model.PolicyConfig
	ProfileID          string
	Profile            *model.GoProfile
}

// BindingAuthorizer verifies actor and scope; it must not acquire a conflicting
// catalog writer. This is a trust boundary, not an implicit access grant.
type BindingAuthorizer func(context.Context, string, string) error

// ReadBinding reads the explicit immutable profile and the current policy and
// root binding under a leased catalog transaction. An explicit profile ID may
// also be referenced by a group; it is not silently substituted by a head.
func (s *Store) ReadBinding(ctx context.Context, worktreeID, profileID string, authorize BindingAuthorizer) (Binding, error) {
	if s == nil || s.lease == nil || ctx == nil || authorize == nil || !validUUID(worktreeID) || !bindingHash(profileID) {
		return Binding{}, ErrUnrecognizedStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return Binding{}, ErrUnrecognizedStore
	}
	if err := authorize(ctx, worktreeID, profileID); err != nil {
		return Binding{}, err
	}
	var result Binding
	err := s.read(ctx, func(tx *sql.Tx) error {
		return readBindingRow(ctx, tx, s.incarnation, worktreeID, profileID, authorize, &result)
	})
	if err != nil {
		return Binding{}, err
	}
	return result, nil
}

func readBindingRow(ctx context.Context, tx *sql.Tx, incarnation, worktreeID, profileID string, authorize BindingAuthorizer, result *Binding) error {
	if err := requireRegistered(ctx, tx, worktreeID); err != nil {
		return err
	}
	var policyJSON, profileJSON, toolchain, provider string
	// CASE prevents oversized corrupt values from being materialized in Go.
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(w.repo_id AS BLOB))=36 THEN w.repo_id END,CASE WHEN length(CAST(w.canonical_root AS BLOB)) BETWEEN 1 AND 16384 THEN w.canonical_root END,CASE WHEN length(CAST(w.filesystem_identity AS BLOB)) BETWEEN 1 AND 4096 THEN w.filesystem_identity END,CASE WHEN length(CAST(r.common_dir AS BLOB)) BETWEEN 1 AND 16384 THEN r.common_dir END,CASE WHEN length(CAST(r.filesystem_identity AS BLOB)) BETWEEN 1 AND 4096 THEN r.filesystem_identity END,w.binding_revision,CASE WHEN length(CAST(h.policy_digest AS BLOB))=64 THEN h.policy_digest END,h.policy_epoch,CASE WHEN length(CAST(p.config_json AS BLOB)) BETWEEN 1 AND 1048576 THEN p.config_json END FROM worktrees AS w JOIN repositories AS r ON r.repo_id=w.repo_id JOIN worktree_policy_heads AS h ON h.worktree_id=w.worktree_id JOIN policies AS p ON p.worktree_id=h.worktree_id AND p.policy_digest=h.policy_digest WHERE w.worktree_id=?`, worktreeID).Scan(&result.RepoID, &result.CanonicalRoot, &result.RootIdentity, &result.CommonDirectory, &result.CommonIdentity, &result.BindingRevision, &result.PolicyDigest, &result.PolicyEpoch, &policyJSON)
	if err != nil {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	err = tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(config_json AS BLOB)) BETWEEN 1 AND 1048576 THEN config_json END,CASE WHEN length(CAST(toolchain_digest AS BLOB))=64 THEN toolchain_digest END,CASE WHEN length(CAST(provider_digest AS BLOB))=64 THEN provider_digest END FROM profiles WHERE worktree_id=? AND profile_id=?`, worktreeID, profileID).Scan(&profileJSON, &toolchain, &provider)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrProfileUnavailable
	}
	if err != nil {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	if !validUUID(result.RepoID) || !bindingHash(result.PolicyDigest) || !filepath.IsAbs(result.CanonicalRoot) || !filepath.IsAbs(result.CommonDirectory) || !utf8.ValidString(result.CanonicalRoot) || !utf8.ValidString(result.CommonDirectory) || !utf8.ValidString(result.RootIdentity) || !utf8.ValidString(result.CommonIdentity) || result.BindingRevision < 1 || result.BindingRevision > maxExactRevision || result.PolicyEpoch < 1 || result.PolicyEpoch > maxExactRevision {
		return ErrUnrecognizedStore
	}
	result.Policy, err = model.DecodePolicyConfigJSON([]byte(policyJSON), maxAdminBytes)
	if err != nil {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	policyDigest, err := model.PolicyDigest(result.Policy, maxAdminBytes)
	if err != nil || policyDigest != result.PolicyDigest {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	profile, err := model.DecodeEffectiveProfileJSON([]byte(profileJSON), maxAdminBytes)
	if err != nil {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	result.Profile, _ = profile.(*model.GoProfile)
	if result.Profile == nil {
		return ErrUnsupportedProfile
	}
	digest, err := model.ProfileDigest(result.Profile, maxAdminBytes)
	if err != nil || digest != profileID || result.Profile.ToolchainDigest != toolchain || result.Profile.ProviderDigest != provider {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	if err := authorize(ctx, worktreeID, profileID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result.CatalogIncarnation, result.WorktreeID, result.ProfileID = incarnation, worktreeID, profileID
	return nil
}

func bindingHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return true
}

// CheckBindingStamp observes only the current registered binding/policy head
// and the existence of this explicit profile. It returns no paths or policy
// content and grants no source access. Its caller must have obtained expected
// with ReadBinding and still recheck the full scope/content before delivery.
// This is useful for a declaration registry whose revision is this head;
// reparsing all immutable configuration here duplicates the enclosing guard.
func (s *Store) CheckBindingStamp(ctx context.Context, expected Binding, authorize BindingAuthorizer) error {
	if s == nil || s.lease == nil || ctx == nil || authorize == nil || !validUUID(expected.CatalogIncarnation) || !validUUID(expected.WorktreeID) || !bindingHash(expected.ProfileID) || !bindingHash(expected.PolicyDigest) || expected.BindingRevision < 1 || expected.BindingRevision > maxExactRevision || expected.PolicyEpoch < 1 || expected.PolicyEpoch > maxExactRevision {
		return ErrUnrecognizedStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrUnrecognizedStore
	}
	if expected.CatalogIncarnation != s.incarnation {
		return ErrCatalogChanged
	}
	if err := authorize(ctx, expected.WorktreeID, expected.ProfileID); err != nil {
		return err
	}
	return s.read(ctx, func(tx *sql.Tx) error {
		var status, digest string
		var revision, epoch int64
		err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(w.status AS BLOB))<=32 THEN w.status END,w.binding_revision,CASE WHEN length(CAST(h.policy_digest AS BLOB))=64 THEN h.policy_digest END,h.policy_epoch FROM worktrees AS w JOIN worktree_policy_heads AS h ON h.worktree_id=w.worktree_id JOIN policies AS p ON p.worktree_id=h.worktree_id AND p.policy_digest=h.policy_digest JOIN profiles AS f ON f.worktree_id=w.worktree_id AND f.profile_id=? WHERE w.worktree_id=?`, expected.ProfileID, expected.WorktreeID).Scan(&status, &revision, &digest, &epoch)
		if errors.Is(err, sql.ErrNoRows) || err == nil && status != "registered" {
			return ErrWorktreeUnavailable
		}
		if err != nil || !bindingHash(digest) || revision < 1 || revision > maxExactRevision || epoch < 1 || epoch > maxExactRevision {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		if revision != expected.BindingRevision || digest != expected.PolicyDigest || epoch != expected.PolicyEpoch {
			return ErrRevisionConflict
		}
		if err := authorize(ctx, expected.WorktreeID, expected.ProfileID); err != nil {
			return err
		}
		return ctx.Err()
	})
}
