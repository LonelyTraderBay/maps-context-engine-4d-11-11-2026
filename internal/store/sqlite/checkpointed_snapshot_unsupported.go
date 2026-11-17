//go:build !linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type CheckpointedSnapshot struct{}
type SnapshotQuery struct{}

func OpenCheckpointedSnapshot(context.Context, *paths.Root, []*paths.Root, *runtime.Lifecycle, string, string, int64, func(context.Context) error, func(context.Context, [sha256.Size]byte) error) (*CheckpointedSnapshot, error) {
	return nil, ErrStateUnsupported
}
func (*CheckpointedSnapshot) Read(context.Context, func(*SnapshotQuery) error) error {
	return ErrStateUnsupported
}
func (*CheckpointedSnapshot) Close() error { return ErrStateUnsupported }
func (*SnapshotQuery) Query(string, ...any) (*sql.Rows, error) {
	return nil, ErrStateUnsupported
}
