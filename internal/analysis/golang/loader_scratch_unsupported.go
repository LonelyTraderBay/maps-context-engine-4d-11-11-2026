//go:build !linux && !windows

package golang

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func LoaderScratchVerifier(*paths.Root, func(context.Context) error) func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
	return func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
		return false, ErrLoaderUnsupported
	}
}
