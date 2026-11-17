// Package schemaref caches only schema references generated from migrations
// embedded in the running binary. It never caches an observed store schema.
package schemaref

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
)

var ErrInvalid = errors.New("invalid schema reference")

// Cache holds at most two versions. T must contain only immutable values:
// the cache and callers own separate slices, but elements are copied by value.
// A failed or canceled build is never deliberately retained. A Cache must not
// be copied after its first use. Builders must honor ctx; waiting callers may
// cancel independently.
type Cache[T any] struct {
	versions [2]slot[T]
}

type slot[T any] struct {
	once  sync.Once
	gate  chan struct{}
	value atomic.Pointer[[]T]
}

func (c *Cache[T]) Get(ctx context.Context, version int, build func(context.Context, int) ([]T, error)) ([]T, error) {
	if c == nil || ctx == nil || version < 1 || version > len(c.versions) || build == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry := &c.versions[version-1]
	if existing := entry.value.Load(); existing != nil {
		return slices.Clone(*existing), nil
	}
	entry.once.Do(func() { entry.gate = make(chan struct{}, 1) })
	select {
	case entry.gate <- struct{}{}:
		defer func() { <-entry.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if existing := entry.value.Load(); existing != nil {
		return slices.Clone(*existing), nil
	}
	objects, err := build(ctx, version)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(objects) == 0 || len(objects) > 256 {
		return nil, ErrInvalid
	}
	immutable := slices.Clone(objects)
	entry.value.Store(&immutable)
	return objects, nil
}
