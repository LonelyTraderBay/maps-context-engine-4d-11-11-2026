//go:build !linux

package sqlite

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func NewWALIndexMemory(context.Context, *paths.Root, []*paths.Root, *runtime.Lifecycle, string, int64, func(context.Context) error) (*WALIndexMemory, error) {
	return nil, ErrStateUnsupported
}
func OpenWALIndexPeer(context.Context, *paths.Root, []*paths.Root, *runtime.Lifecycle, string, WALIndexPeer, func(context.Context) error, func(context.Context, WALIndexPeer) error) (*WALIndexMemory, error) {
	return nil, ErrStateUnsupported
}
func (*WALIndexMemory) Recheck(context.Context) error { return ErrStateUnsupported }
func (*WALIndexMemory) Peer(context.Context) (WALIndexPeer, error) {
	return WALIndexPeer{}, ErrStateUnsupported
}
func (*WALIndexMemory) Map(context.Context, int64, int64) (*WALIndexMapping, error) {
	return nil, ErrStateUnsupported
}
func (*WALIndexMemory) Hold(context.Context) (*WALIndexUse, error) {
	return nil, ErrStateUnsupported
}
func (*WALIndexUse) Close() error               { return ErrStateUnsupported }
func (*WALIndexMapping) Bytes() ([]byte, error) { return nil, ErrStateUnsupported }
func (*WALIndexMapping) Close() error           { return ErrStateUnsupported }
func (*WALIndexMemory) Close() error            { return ErrStateUnsupported }
