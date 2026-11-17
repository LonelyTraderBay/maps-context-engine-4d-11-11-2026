package runtime

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var (
	ErrInvalidLease     = errors.New("invalid lock request or lease")
	ErrLifecycleInUse   = errors.New("lifecycle lease still has attached handles")
	ErrWriterInUse      = errors.New("writer lease still has active transactions")
	ErrStoreMaintenance = errors.New("store lifecycle lock is busy")
	ErrWorktreeBusy     = errors.New("worktree writer lock is busy")
	ErrLockReplaced     = errors.New("lock file identity changed")
	ErrLockUnsafe       = errors.New("store lock directory or file is not owner-only")
)

// Lifecycle is a cooperative OS lease for one catalog or worktree. The caller
// must authorize and own root, and must hold attachments for every DB handle
// until all such handles are closed. Never remove or replace its lock file.
type Lifecycle struct{ state *lifecycleState }

type lifecycleState struct {
	mu     sync.Mutex
	root   *paths.Root
	file   *os.File
	scope  string
	mode   ports.LeaseMode
	holds  int
	closed bool
}

// AcquireLifecycle does not open a DB or authorize a caller. A catalog scope
// is named "catalog"; a worktree scope is its canonical UUID. It never upgrades
// an existing lease: close all handles and release before acquiring exclusive.
func AcquireLifecycle(ctx context.Context, root *paths.Root, scope string, mode ports.LeaseMode, wait time.Duration) (*Lifecycle, error) {
	if ctx == nil || root == nil || !validLockScope(scope) || (mode != ports.SharedLease && mode != ports.ExclusiveLease) || wait <= 0 {
		return nil, ErrInvalidLease
	}
	file, err := acquireOwnedLock(ctx, root, "lifecycle.lock", mode == ports.ExclusiveLease, wait, ErrStoreMaintenance)
	if err != nil {
		return nil, err
	}
	return &Lifecycle{state: &lifecycleState{root: root, file: file, scope: scope, mode: mode}}, nil
}

func (l *Lifecycle) Check(ctx context.Context, scope string, required ports.LeaseMode) error {
	if l == nil || l.state == nil || ctx == nil {
		return ErrInvalidLease
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkLocked(ctx, scope, required)
}

// CheckRoot verifies that the held lifecycle protects this exact root handle,
// as well as the scope, mode and current lock identity. Callers using a
// different handle must acquire their own lifecycle against that handle.
func (l *Lifecycle) CheckRoot(ctx context.Context, root *paths.Root, scope string, required ports.LeaseMode) error {
	if l == nil || l.state == nil || root == nil || ctx == nil {
		return ErrInvalidLease
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.root != root {
		return ErrInvalidLease
	}
	return s.checkLocked(ctx, scope, required)
}

func (l *lifecycleState) checkLocked(ctx context.Context, scope string, required ports.LeaseMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.closed || l.file == nil || l.scope != scope || (required != ports.SharedLease && required != ports.ExclusiveLease) || (required == ports.ExclusiveLease && l.mode != ports.ExclusiveLease) {
		return ErrInvalidLease
	}
	return verifyOwnedLock(l.root, l.file, "lifecycle.lock")
}

// Hold attaches one resource lifetime to the lease. A pool owner must keep the
// attachment until the pool and every borrowed connection/transaction close.
func (l *Lifecycle) Hold(ctx context.Context) (*LifecycleHold, error) {
	if l == nil || l.state == nil || ctx == nil {
		return nil, ErrInvalidLease
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(ctx, s.scope, ports.SharedLease); err != nil {
		return nil, err
	}
	s.holds++
	return &LifecycleHold{state: &holdState{lease: s}}, nil
}

type LifecycleHold struct{ state *holdState }

type holdState struct {
	mu    sync.Mutex
	lease *lifecycleState
}

func (h *LifecycleHold) Close() error {
	if h == nil || h.state == nil {
		return ErrInvalidLease
	}
	s := h.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lease == nil {
		return ErrInvalidLease
	}
	s.lease.mu.Lock()
	s.lease.holds--
	s.lease.mu.Unlock()
	s.lease = nil
	return nil
}

// Close refuses while any pool, connection, transaction, or writer attachment
// remains. It never removes the lock file or tries to steal another lease.
func (l *Lifecycle) Close() error {
	if l == nil || l.state == nil {
		return ErrInvalidLease
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return ErrInvalidLease
	}
	if s.holds != 0 {
		return ErrLifecycleInUse
	}
	s.closed = true
	err := releaseOwnedLock(s.file)
	s.file = nil
	return err
}

func validLockScope(scope string) bool { return scope == "catalog" || validLockUUID(scope) }

func validLockUUID(value string) bool {
	if len(value) != 36 || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
