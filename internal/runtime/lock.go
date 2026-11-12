package runtime

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// Writer is an exclusive worktree writer lease bound to one job.
type Writer struct{ state *writerState }

type writerState struct {
	mu         sync.Mutex
	lease      *Lifecycle
	hold       *LifecycleHold
	file       *os.File
	job        string
	holds      int
	streamHeld bool
}

func AcquireWriter(ctx context.Context, lifecycle *Lifecycle, job string, wait time.Duration) (*Writer, error) {
	return acquireScopedWriter(ctx, lifecycle, job, wait, false)
}

// AcquireCatalogMutation holds the catalog mutation lock for a single
// operation; it is not a global build lock.
func AcquireCatalogMutation(ctx context.Context, lifecycle *Lifecycle, operationID string, wait time.Duration) (*Writer, error) {
	return acquireScopedWriter(ctx, lifecycle, operationID, wait, true)
}

func acquireScopedWriter(ctx context.Context, lifecycle *Lifecycle, job string, wait time.Duration, catalog bool) (*Writer, error) {
	if ctx == nil || lifecycle == nil || lifecycle.state == nil || !validLockUUID(job) || wait <= 0 {
		return nil, ErrInvalidLease
	}
	if catalog && lifecycle.state.scope != "catalog" || !catalog && !validLockUUID(lifecycle.state.scope) {
		return nil, ErrInvalidLease
	}
	hold, err := lifecycle.Hold(ctx)
	if err != nil {
		return nil, err
	}
	file, err := acquireOwnedLock(ctx, lifecycle.state.root, "writer.lock", true, wait, ErrWorktreeBusy)
	if err != nil {
		return nil, errorsJoinHold(err, hold)
	}
	if err := lifecycle.Check(ctx, lifecycle.state.scope, ports.SharedLease); err != nil {
		return nil, errorsJoinWriter(err, file, hold)
	}
	return &Writer{state: &writerState{lease: lifecycle, hold: hold, file: file, job: job}}, nil
}

func (w *Writer) CheckCatalogMutation(ctx context.Context, operationID string) error {
	return w.CheckWriter(ctx, "catalog", operationID)
}

func (w *Writer) CheckWriter(ctx context.Context, worktree, job string) error {
	if w == nil || w.state == nil || ctx == nil {
		return ErrInvalidLease
	}
	s := w.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkLocked(ctx, worktree, job)
}

func (s *writerState) checkLocked(ctx context.Context, worktree, job string) error {
	if s.file == nil || s.lease == nil || s.job != job {
		return ErrInvalidLease
	}
	if err := s.lease.Check(ctx, worktree, ports.SharedLease); err != nil {
		return err
	}
	return verifyOwnedLock(s.lease.state.root, s.file, "writer.lock")
}

func (w *Writer) HoldWriter(ctx context.Context, worktree, job string) (ports.LeaseGuard, error) {
	if w == nil || w.state == nil || ctx == nil {
		return nil, ErrInvalidLease
	}
	s := w.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(ctx, worktree, job); err != nil {
		return nil, err
	}
	s.holds++
	return &writerGuard{state: &writerGuardState{writer: s}}, nil
}

func (w *Writer) HoldCatalogMutation(ctx context.Context, operationID string) (ports.LeaseGuard, error) {
	return w.HoldWriter(ctx, "catalog", operationID)
}

// HoldIndexStream reserves one long-lived provider stream or publication on
// this already-held worktree writer. It never serializes different writers.
func (w *Writer) HoldIndexStream(ctx context.Context, worktree, job string) (ports.LeaseGuard, error) {
	if w == nil || w.state == nil || ctx == nil {
		return nil, ErrInvalidLease
	}
	s := w.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(ctx, worktree, job); err != nil {
		return nil, err
	}
	if s.streamHeld {
		return nil, ports.ErrIndexStreamBusy
	}
	s.streamHeld = true
	s.holds++
	return &indexStreamGuard{state: &writerGuardState{writer: s}}, nil
}

type indexStreamGuard struct{ state *writerGuardState }

func (g *indexStreamGuard) Close() error {
	if g == nil || g.state == nil {
		return ErrInvalidLease
	}
	s := g.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return ErrInvalidLease
	}
	s.writer.mu.Lock()
	s.writer.streamHeld = false
	s.writer.holds--
	s.writer.mu.Unlock()
	s.writer = nil
	return nil
}

type writerGuard struct{ state *writerGuardState }
type writerGuardState struct {
	mu     sync.Mutex
	writer *writerState
}

func (g *writerGuard) Close() error {
	if g == nil || g.state == nil {
		return ErrInvalidLease
	}
	s := g.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return ErrInvalidLease
	}
	s.writer.mu.Lock()
	s.writer.holds--
	s.writer.mu.Unlock()
	s.writer = nil
	return nil
}

func (w *Writer) Close() error {
	if w == nil || w.state == nil {
		return ErrInvalidLease
	}
	s := w.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil || s.hold == nil {
		return ErrInvalidLease
	}
	if s.holds != 0 {
		return ErrWriterInUse
	}
	err := errorsJoinWriter(nil, s.file, s.hold)
	s.file, s.hold, s.lease = nil, nil, nil
	return err
}
