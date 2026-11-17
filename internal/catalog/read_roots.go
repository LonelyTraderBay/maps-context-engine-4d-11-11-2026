package catalog

import (
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// VerifyRoots checks already-authorized, opened root handles against the
// observed catalog identity and policy, including reciprocal Git metadata.
// linkedRelative must be the explicit worktrees/<name> child previously
// authorized by the caller; pointer contents never select an arbitrary path.
// The caller must still re-read catalog epochs and check principal authority.
func (b Binding) VerifyRoots(worktree, common *paths.Root, linkedRelative string) error {
	if worktree == nil || common == nil || !validUUID(b.WorktreeID) || !validUUID(b.RepoID) || !bindingHash(b.PolicyDigest) {
		return ErrUnrecognizedStore
	}
	digest, err := model.PolicyDigest(b.Policy, maxAdminBytes)
	if err != nil || digest != b.PolicyDigest {
		return errors.Join(ErrUnrecognizedStore, err)
	}
	verify := func() error {
		return verifyRegistrationBinding(worktree, common, linkedRelative, b.CanonicalRoot, b.RootIdentity, b.CommonDirectory, b.CommonIdentity)
	}
	if err := verify(); err != nil {
		return err
	}
	allowed, err := policyContainsRoot(b.Policy.AllowedRoots, worktree, b.CanonicalRoot)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRootNotAllowed
	}
	return verify()
}
