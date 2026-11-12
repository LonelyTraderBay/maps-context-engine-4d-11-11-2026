package ports

import (
	"context"
	"errors"
)

// LeaseMode describes the lifecycle coordination required by an operation.
// A shared lease covers the entire lifetime of any store pool or handle; an
// exclusive lease covers quiescent initialization or maintenance.
type LeaseMode string

const (
	SharedLease    LeaseMode = "shared"
	ExclusiveLease LeaseMode = "exclusive"
)

// LifecycleLease is supplied by the caller that owns the OS lease and all
// associated database handles. Check does not itself acquire or release a
// lease. The verified OS implementation is owned by T010.
type LifecycleLease interface {
	Check(ctx context.Context, worktreeID string, required LeaseMode) error
}

// IndexWriterLease is the caller-owned worktree writer authority. Its Check
// must identify the current job and held OS lock; T010 supplies the real OS
// implementation before any public index mutation is enabled.
type IndexWriterLease interface {
	CheckWriter(ctx context.Context, worktreeID, jobID string) error
	HoldWriter(ctx context.Context, worktreeID, jobID string) (LeaseGuard, error)
}

// IndexStreamLease reserves the writer's single in-process provider stream or
// captured-only publication. It shares ownership with the OS writer lease so
// independent store handles cannot each start a stream for the same writer.
type IndexStreamLease interface {
	HoldIndexStream(ctx context.Context, worktreeID, jobID string) (LeaseGuard, error)
}

// IndexRecoveryLease represents a newly acquired worktree writer for recovery,
// distinct from an already-running job's IndexWriterLease.
type IndexRecoveryLease interface {
	CheckRecovery(ctx context.Context, worktreeID, recoveryID string) error
	HoldRecovery(ctx context.Context, worktreeID, recoveryID string) (LeaseGuard, error)
}

var ErrIndexStreamBusy = errors.New("index writer already has an active stream")

// CatalogMutationLease coordinates catalog mutations with maintenance.
type CatalogMutationLease interface {
	CheckCatalogMutation(ctx context.Context, operationID string) error
	HoldCatalogMutation(ctx context.Context, operationID string) (LeaseGuard, error)
}

// LeaseGuard pins a held lock while a transaction may use it.
type LeaseGuard interface{ Close() error }
