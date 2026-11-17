package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

// RecordsOwner assumes pool ownership only after successful verification.
// The caller must hold the matching lifecycle lease until Close succeeds.
type RecordsOwner struct {
	lifetime *poolLifetime
	store    *records.Store
}

func OpenRecordsOwner(ctx context.Context, lease *runtime.Lifecycle, db *sql.DB, worktreeID string) (*RecordsOwner, error) {
	if ctx == nil || lease == nil || db == nil {
		return nil, records.ErrInvalidStore
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	store, err := records.Open(ctx, db, worktreeID, lease)
	if err != nil {
		return nil, errors.Join(err, hold.Close())
	}
	return &RecordsOwner{store: store, lifetime: &poolLifetime{pools: []*sql.DB{db}, hold: hold}}, nil
}

func (o *RecordsOwner) Store() (*records.Store, error) {
	if o == nil || o.lifetime == nil {
		return nil, ErrOwnerClosed
	}
	o.lifetime.mu.Lock()
	defer o.lifetime.mu.Unlock()
	if o.lifetime.closing || o.lifetime.closed {
		return nil, ErrOwnerClosed
	}
	return o.store, nil
}

func (o *RecordsOwner) Close() error {
	if o == nil || o.lifetime == nil {
		return ErrOwnerClosed
	}
	return o.lifetime.close()
}
