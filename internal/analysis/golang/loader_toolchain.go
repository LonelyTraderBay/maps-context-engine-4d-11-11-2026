package golang

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// LoaderToolchainVerifier combines the Go binary/PATH observation with a
// separately supplied proof of the entire toolchain digest and root trust.
// Neither check makes a later PATH-based exec atomic against same-user changes.
func LoaderToolchainVerifier(tool *runtimeadapter.GoExecutable, goRoot string, verifyFingerprint ToolchainVerifier) ToolchainVerifier {
	return func(ctx context.Context, profile *model.GoProfile) (bool, error) {
		if ctx == nil || tool == nil || profile == nil || verifyFingerprint == nil {
			return false, nil
		}
		verified, err := verifyFingerprint(ctx, profile)
		if err != nil || !verified {
			return false, err
		}
		if err := tool.CheckForLoad(ctx, goRoot); err != nil {
			if errors.Is(err, runtimeadapter.ErrGoToolchain) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
}
