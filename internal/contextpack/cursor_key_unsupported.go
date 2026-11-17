//go:build !linux

package contextpack

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// The platform must provide an equivalent verified owner-only file adapter.
func LoadOrCreateCursorKey(context.Context, *paths.Root) ([32]byte, error) {
	return [32]byte{}, ErrCursorKeyUnsupported
}
