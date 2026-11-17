//go:build !linux

package cli

import (
	"context"

	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// Other platforms need their own approved state-path and permissions adapter.
func OpenDefaultReadOnlyConfig(context.Context, string, int) (ConfigOwner, func() error, error) {
	return nil, nil, fsruntime.ErrConfigUnsupported
}
