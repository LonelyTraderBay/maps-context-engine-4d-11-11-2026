package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

var ErrMaintenancePartial = errors.New("catalog schema may be initialized; reconcile before retry")

// MaintenanceOwner owns a single caller-provided pool under an exclusive
// lifecycle lease. Close the owner, then the writer, then the lifecycle.
type MaintenanceOwner struct {
	lifetime *poolLifetime
	lease    *runtime.Lifecycle
	db       *sql.DB
	scope    string
}

func OpenMaintenanceOwner(ctx context.Context, lease *runtime.Lifecycle, db *sql.DB, scope string) (*MaintenanceOwner, error) {
	if ctx == nil || lease == nil || db == nil || db.Stats().MaxOpenConnections != 1 {
		return nil, index.ErrInvalidStore
	}
	if err := lease.Check(ctx, scope, ports.ExclusiveLease); err != nil {
		return nil, err
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	return &MaintenanceOwner{lifetime: &poolLifetime{pools: []*sql.DB{db}, hold: hold}, lease: lease, db: db, scope: scope}, nil
}

func (o *MaintenanceOwner) InitializeIndex(ctx context.Context, writer ports.IndexWriterLease, jobID string) (index.Identity, error) {
	if o == nil || o.lifetime == nil || o.scope == "catalog" {
		return index.Identity{}, index.ErrInvalidStore
	}
	o.lifetime.mu.Lock()
	defer o.lifetime.mu.Unlock()
	if o.lifetime.closing || o.lifetime.closed {
		return index.Identity{}, ErrOwnerClosed
	}
	return index.InitializeWithWriter(ctx, o.db, o.scope, o.lease, writer, jobID)
}

// InitializeCatalog changes WAL mode only after validating the recognized
// catalog schema and while exclusive lifecycle and mutation locks are held.
func (o *MaintenanceOwner) InitializeCatalog(ctx context.Context, mutation ports.CatalogMutationLease, operationID string) (incarnation string, resultErr error) {
	if o == nil || o.lifetime == nil || o.scope != "catalog" || ctx == nil || mutation == nil {
		return "", catalog.ErrUnrecognizedStore
	}
	o.lifetime.mu.Lock()
	defer o.lifetime.mu.Unlock()
	if o.lifetime.closing || o.lifetime.closed {
		return "", ErrOwnerClosed
	}
	guard, err := mutation.HoldCatalogMutation(ctx, operationID)
	if err != nil {
		return "", err
	}
	if guard == nil {
		return "", catalog.ErrCatalogWriterRequired
	}
	defer func() { resultErr = errors.Join(resultErr, guard.Close()) }()
	incarnation, err = catalog.InitializeLeased(ctx, o.db, o.lease, mutation, operationID)
	if err != nil {
		return "", err
	}
	if err := o.lease.Check(ctx, "catalog", ports.ExclusiveLease); err != nil {
		return "", errors.Join(ErrMaintenancePartial, err)
	}
	conn, err := o.db.Conn(ctx)
	if err != nil {
		return "", errors.Join(ErrMaintenancePartial, err)
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		return "", errors.Join(ErrMaintenancePartial, err)
	}
	if err := o.lease.Check(ctx, "catalog", ports.ExclusiveLease); err != nil {
		return "", errors.Join(ErrMaintenancePartial, err)
	}
	if err := mutation.CheckCatalogMutation(ctx, operationID); err != nil {
		return "", errors.Join(ErrMaintenancePartial, err)
	}
	return incarnation, nil
}

func (o *MaintenanceOwner) Close() error {
	if o == nil || o.lifetime == nil {
		return ErrOwnerClosed
	}
	return o.lifetime.close()
}
