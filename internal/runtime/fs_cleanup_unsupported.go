//go:build !linux

package runtime

import "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"

func CheckCleanupRoot(*paths.Root, []*paths.Root) error { return paths.ErrUnsupported }
