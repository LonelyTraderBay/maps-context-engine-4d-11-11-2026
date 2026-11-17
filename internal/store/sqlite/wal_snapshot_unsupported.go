//go:build !linux

package sqlite

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type WALSnapshot struct{}

func OpenWALSnapshot(context.Context, *paths.Root, []*paths.Root, *runtime.Lifecycle, string, string, int64, func(context.Context) error, func(context.Context, WALSnapshotPair) error) (*WALSnapshot, error) {
	return nil, ErrStateUnsupported
}
func (*WALSnapshot) Read(context.Context, func(*SnapshotQuery) error) error {
	return ErrStateUnsupported
}
func (*WALSnapshot) Close() error { return ErrStateUnsupported }
