package runtime

import (
	"context"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// RecoveryWriter is only constructed by acquiring the real per-worktree OS
// writer lock. It cannot be obtained from a currently running job's Writer.
type RecoveryWriter struct{ writer *Writer }

func AcquireRecoveryWriter(ctx context.Context, lifecycle *Lifecycle, recoveryID string, wait time.Duration) (*RecoveryWriter, error) {
	writer, err := AcquireWriter(ctx, lifecycle, recoveryID, wait)
	if err != nil {
		return nil, err
	}
	return &RecoveryWriter{writer: writer}, nil
}

func (r *RecoveryWriter) CheckRecovery(ctx context.Context, worktreeID, recoveryID string) error {
	if r == nil || r.writer == nil {
		return ErrInvalidLease
	}
	return r.writer.CheckWriter(ctx, worktreeID, recoveryID)
}

func (r *RecoveryWriter) HoldRecovery(ctx context.Context, worktreeID, recoveryID string) (ports.LeaseGuard, error) {
	if r == nil || r.writer == nil {
		return nil, ErrInvalidLease
	}
	return r.writer.HoldWriter(ctx, worktreeID, recoveryID)
}

func (r *RecoveryWriter) Close() error {
	if r == nil || r.writer == nil {
		return ErrInvalidLease
	}
	return r.writer.Close()
}
