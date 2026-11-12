//go:build !linux

package catalog

import "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"

func directoryIdentity(*paths.Root) (string, string, error) { return "", "", paths.ErrUnsupported }
func sameDirectory(*paths.Root, *paths.Root) (bool, error)  { return false, paths.ErrUnsupported }
