//go:build !linux

package runtime

import (
	"context"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func runtimeConfigPending(*paths.Root) (bool, error) { return false, paths.ErrUnsupported }
func verifyRuntimeConfigFile(*os.File) error         { return paths.ErrUnsupported }

func SetRuntimeConfig(context.Context, *paths.Root, []byte, int) (ConfigWriteResult, error) {
	return ConfigWriteResult{State: "not_applied"}, paths.ErrUnsupported
}

func SetRuntimeConfigAt(context.Context, *paths.Root, string, []byte, int) (ConfigWriteResult, error) {
	return ConfigWriteResult{State: "not_applied"}, paths.ErrUnsupported
}
