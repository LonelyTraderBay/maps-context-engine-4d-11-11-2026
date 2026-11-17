// Package packstore provides a bounded owner-only store for temporary pack
// bytes. It does not authenticate packs, authorize roots or create directories.
package packstore

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var (
	ErrInvalid     = errors.New("invalid pack store request")
	ErrUnsafe      = errors.New("pack store authority or integrity unavailable")
	ErrCapacity    = errors.New("pack store capacity exceeded")
	ErrMissing     = errors.New("retained pack unavailable")
	ErrUncertain   = errors.New("pack store write outcome uncertain")
	ErrUnsupported = errors.New("secure pack store unsupported")
)

// BudgetReservation is a trusted owner-provided reservation against the total
// worktree cache budget. Check must prove it remains held across publication;
// Close must not discard the charge for a file that has already been published.
type BudgetReservation interface {
	Check(context.Context) error
	Close() error
}

type ReserveBudget func(context.Context, int64) (BudgetReservation, error)
type VerifyBytes = func(context.Context, []byte) error

// ExpiryProof must independently authenticate the exact owned file and prove
// it is expired; an unknown or unverifiable file must return false or an error.
// A callback must not infer ownership or expiry from a filename alone.
type ExpiryProof = func(context.Context, string, []byte) (bool, error)

// GCWriter is an OS writer lease for the same authorized worktree state root.
// GCExpired also requires its concrete value to provide CheckWriterRoot;
// callers must not supply untrusted implementations of this authority.
type GCWriter = ports.IndexWriterLease

// Store borrows previously authorized handles. Its caller must keep them open,
// prove the protected-root set is complete, supply real worktree-budget
// reservations, and authenticate the pack/token in every VerifyBytes callback.
// Callbacks must not reenter this store while its quota lock is held.
type Store struct {
	root      *paths.Root
	protected []*paths.Root
	lease     *runtime.Lifecycle
	worktree  string
	maxResult int64
	quota     int64
	maxCount  int
	authorize func(context.Context) error
	reserve   ReserveBudget
}

// New configures a store in an already-created worktree state root. It does
// not authorize paths, create directories, perform GC or enable a CLI route.
func New(root *paths.Root, protected []*paths.Root, lease *runtime.Lifecycle, worktree string, maxResult, quota int64, maxCount int, authorize func(context.Context) error, reserve ReserveBudget) (*Store, error) {
	if root == nil || lease == nil || len(protected) < 1 || len(protected) > 64 || !validID(worktree) || maxResult < 1024 || maxResult > 32*1024*1024 || quota < maxResult || quota > 32*1024*1024 || maxCount < 1 || maxCount > 32 || authorize == nil || reserve == nil {
		return nil, ErrInvalid
	}
	for _, r := range protected {
		if r == nil {
			return nil, ErrInvalid
		}
	}
	return &Store{root: root, protected: append([]*paths.Root(nil), protected...), lease: lease, worktree: worktree, maxResult: maxResult, quota: quota, maxCount: maxCount, authorize: authorize, reserve: reserve}, nil
}

func (s *Store) WorktreeID() string {
	if s == nil {
		return ""
	}
	return s.worktree
}

func (s *Store) MaxResultBytes() int64 {
	if s == nil {
		return 0
	}
	return s.maxResult
}

// WithinRetentionLimits reports the immutable spool ceilings chosen at
// construction. The owner must separately enforce the total worktree budget,
// root containment, lifecycle and authorization throughout each operation.
func (s *Store) WithinRetentionLimits(worktree string, maxBytes, maxCount int64) bool {
	return s != nil && worktree != "" && s.worktree == worktree && maxBytes > 0 && maxCount > 0 &&
		s.maxResult <= s.quota && s.quota <= maxBytes && int64(s.maxCount) <= maxCount
}

func validID(value string) bool {
	if len(value) != 36 || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func checked(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil || fn == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fn(ctx); err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return ErrUnsafe
	}
	return ctx.Err()
}
