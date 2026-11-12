//go:build !linux

package inventory

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func PrepareIgnoreSandbox(context.Context, *paths.Root, func(context.Context) error) error {
	return paths.ErrUnsupported
}
