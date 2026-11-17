package compose

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type guardedConfigStore struct {
	root      *paths.Root
	protected []*paths.Root
	name      string
	inner     *fsruntime.RuntimeConfigStore
	authorize func(context.Context) error
}

// NewGuardedConfigService borrows owner-authorized roots and a complete set of
// protected target roots. The caller keeps all handles open and proves that the
// supplied set is complete. The callback must not reenter the config store.
func NewGuardedConfigService(ctx context.Context, root *paths.Root, name string, protected []*paths.Root, cpus int, authorize func(context.Context) error) (*app.RuntimeConfigService, error) {
	if ctx == nil || root == nil || len(protected) == 0 || len(protected) > 64 || authorize == nil {
		return nil, app.ErrAdminDenied
	}
	inner, err := fsruntime.NewRuntimeConfigStore(root, name)
	if err != nil {
		return nil, err
	}
	store := &guardedConfigStore{root: root, protected: append([]*paths.Root(nil), protected...), name: name, inner: inner, authorize: authorize}
	return app.NewRuntimeConfigService(ctx, store, cpus)
}

func (s *guardedConfigStore) check(ctx context.Context) error {
	if s == nil || ctx == nil || s.authorize == nil || ctx.Err() != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return app.ErrAdminDenied
	}
	if s.authorize(ctx) != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return app.ErrAdminDenied
	}
	if fsruntime.CheckCleanupRoot(s.root, s.protected) != nil {
		return app.ErrAdminDenied
	}
	return ctx.Err()
}

func (s *guardedConfigStore) Read(ctx context.Context, maxBytes int) ([]byte, bool, error) {
	if err := s.check(ctx); err != nil {
		return nil, false, err
	}
	data, present, err := s.inner.Read(ctx, maxBytes)
	if err != nil {
		return nil, false, err
	}
	if err := s.check(ctx); err != nil {
		return nil, false, err
	}
	return data, present, nil
}

func (s *guardedConfigStore) Set(ctx context.Context, raw []byte, cpus int) (ports.RuntimeConfigWriteResult, error) {
	if err := s.check(ctx); err != nil {
		return ports.RuntimeConfigWriteResult{State: "not_applied"}, err
	}
	result, err := fsruntime.SetRuntimeConfigGuardedAt(ctx, s.root, s.name, raw, cpus, s.check)
	return result, errors.Join(err, s.check(ctx))
}

var _ ports.RuntimeConfigStore = (*guardedConfigStore)(nil)
