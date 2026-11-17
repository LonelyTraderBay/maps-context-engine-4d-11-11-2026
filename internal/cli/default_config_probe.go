package cli

import (
	"context"
	"errors"

	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// ProbeDefaultReadOnlyConfig checks the same owner used by config show. It
// reports no configuration values or paths and never attempts a write.
func ProbeDefaultReadOnlyConfig(ctx context.Context, configPath string, cpus int) error {
	owner, release, err := OpenDefaultReadOnlyConfig(ctx, configPath, cpus)
	if err != nil {
		if release != nil {
			err = errors.Join(err, release())
		}
		return err
	}
	if owner == nil || release == nil {
		if release != nil {
			_ = release()
		}
		return fsruntime.ErrConfigUnsupported
	}
	_, err = owner.Show(ctx)
	return errors.Join(err, release())
}
