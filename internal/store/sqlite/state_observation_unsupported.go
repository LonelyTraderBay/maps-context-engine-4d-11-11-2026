//go:build !linux

package sqlite

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func ObserveStateFiles(context.Context, *paths.Root, []*paths.Root, *runtime.Lifecycle, string, string, int64, func(context.Context) error) (*StateObservation, error) {
	return nil, ErrStateUnsupported
}
func (*StateObservation) Recheck(context.Context) error { return ErrStateUnsupported }
func (*StateObservation) Close() error                  { return ErrStateUnsupported }
