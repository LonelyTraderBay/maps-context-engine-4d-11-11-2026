// Package sqlite coordinates the lifetime of caller-created SQLite pools.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

var (
	ErrOwnerClosed = errors.New("SQLite pool owner is closed")
	ErrPoolBusy    = errors.New("SQLite pool still has borrowed handles")
)

// IndexOwner takes ownership of the supplied pools only after OpenIndexOwner
// succeeds. The caller creates them for the same authorized store while it
// already holds lifecycle. The caller must not expose untracked driver
// connections or change the pool limits. Keep lifecycle open through Close.
type IndexOwner struct {
	lifetime *poolLifetime
	store    *index.Store
}

type poolLifetime struct {
	mu       sync.Mutex
	pools    []*sql.DB
	hold     *runtime.LifecycleHold
	closing  bool
	closed   bool
	closeErr error
}

func OpenIndexOwner(ctx context.Context, lease *runtime.Lifecycle, readers, writer *sql.DB, worktreeID string) (*IndexOwner, error) {
	if ctx == nil || lease == nil || readers == nil || writer == nil {
		return nil, index.ErrInvalidStore
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	store, err := index.Open(ctx, readers, writer, worktreeID, lease)
	if err != nil {
		return nil, errors.Join(err, hold.Close())
	}
	pools := []*sql.DB{readers}
	if writer != readers {
		pools = append(pools, writer)
	}
	return &IndexOwner{store: store, lifetime: &poolLifetime{pools: pools, hold: hold}}, nil
}

func (o *IndexOwner) Store() (*index.Store, error) {
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

// Close first closes pools, then confirms every borrowed database/sql handle
// has returned. If a handle remains, the lifecycle attachment is retained and
// Close can be retried after the handle closes. A driver close failure leaves
// the attachment held because release safety cannot be established.
func (o *IndexOwner) Close() error {
	if o == nil || o.lifetime == nil {
		return ErrOwnerClosed
	}
	return o.lifetime.close()
}

func (p *poolLifetime) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrOwnerClosed
	}
	if !p.closing {
		p.closing = true
		for _, pool := range p.pools {
			p.closeErr = errors.Join(p.closeErr, pool.Close())
		}
	}
	if p.closeErr != nil {
		return p.closeErr
	}
	for _, pool := range p.pools {
		if pool.Stats().InUse != 0 {
			return ErrPoolBusy
		}
	}
	if err := p.hold.Close(); err != nil {
		return err
	}
	p.closed = true
	p.hold, p.pools = nil, nil
	return nil
}
