package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// CatalogOwner owns one caller-provided catalog pool under a shared lifecycle
// lease. Mutations require separately bound, short-lived catalog writer locks.
type CatalogOwner struct {
	lifetime *poolLifetime
	store    *catalog.Store
}

func OpenCatalogOwner(ctx context.Context, lease *runtime.Lifecycle, db *sql.DB) (*CatalogOwner, error) {
	if ctx == nil || lease == nil || db == nil {
		return nil, catalog.ErrUnrecognizedStore
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	store, err := catalog.OpenExistingLeased(ctx, db, lease)
	if err != nil {
		return nil, errors.Join(err, hold.Close())
	}
	return &CatalogOwner{store: store, lifetime: &poolLifetime{pools: []*sql.DB{db}, hold: hold}}, nil
}

func (o *CatalogOwner) Store() (*catalog.Store, error) {
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

func (o *CatalogOwner) Close() error {
	if o == nil || o.lifetime == nil {
		return ErrOwnerClosed
	}
	return o.lifetime.close()
}
