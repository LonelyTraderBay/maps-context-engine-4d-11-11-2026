//go:build linux

package sqlite

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

// MutateRecord exposes a typed append/CAS only under an already held worktree
// writer. The protected connector keeps the durable WAL authority on commit.
func (o *ProtectedWorktreeOwner) MutateRecord(ctx context.Context, request records.MutationRequest) (out records.MutationResult, resultErr error) {
	if ctx == nil || request.Writer == nil || len(request.JobID) != 36 {
		return out, records.ErrInvalidStore
	}
	if err := o.check(ctx); err != nil {
		return out, err
	}
	if err := request.Writer.CheckWriter(ctx, o.manifest.WorktreeID, request.JobID); err != nil {
		return out, err
	}
	if len(o.stores) != 2 || o.stores[1].name != "records.sqlite" {
		return out, ErrStateUnsafe
	}
	pool := o.stores[1].pool(false, 1)
	owner, err := OpenRecordsOwner(ctx, o.lease, pool, o.manifest.WorktreeID)
	if err != nil {
		return out, errors.Join(err, pool.Close())
	}
	defer func() {
		if err := owner.Close(); err != nil {
			if out.Record.Version > 0 && !out.Replayed {
				err = errors.Join(records.ErrAfterCommit, err)
			}
			out = records.MutationResult{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	store, err := owner.Store()
	if err != nil {
		return out, err
	}
	return store.MutateMapping(ctx, request)
}
