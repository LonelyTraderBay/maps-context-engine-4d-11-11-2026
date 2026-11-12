//go:build !linux

package runtime

import (
	"context"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func GitInventory(context.Context, *paths.Root, string, int) ([]byte, error) {
	return nil, paths.ErrUnsupported
}
