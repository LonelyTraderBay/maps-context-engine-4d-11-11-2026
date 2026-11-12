package ports

import "context"

// RecoveryCandidate contains only bounded identity metadata, never source.
type RecoveryCandidate struct {
	GenerationID   string
	OwnerJobID     string
	ProfileID      string
	PolicyDigest   string
	PolicyEpoch    int64
	ProviderDigest string
	SnapshotDigest string // empty means capture was not sealed
}

// RecoveryAuthorizer must verify the current actor and recovery authority;
// the historical policy fields do not themselves grant permission. An empty
// candidate requests a worktree-level check before a listing, including an
// empty result. Nonempty candidates require an additional row-level check.
type RecoveryAuthorizer func(context.Context, string, RecoveryCandidate) error

type RecoveryStore interface {
	ListBuildingForRecovery(context.Context, string, IndexRecoveryLease, string, int, RecoveryAuthorizer) ([]RecoveryCandidate, error)
	FailAbandonedGeneration(context.Context, string, IndexRecoveryLease, RecoveryCandidate, RecoveryAuthorizer) error
	ObserveRecoveryCandidate(context.Context, string, IndexRecoveryLease, RecoveryCandidate, RecoveryAuthorizer) (string, error)
}
