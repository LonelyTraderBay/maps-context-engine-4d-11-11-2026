//go:build linux

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// OpenDefaultReadOnlyConfig supplies only config show for the current user's
// standard Linux state path. It never creates state and does not authorize a
// caller-selected config path or any state mutation. The checks in the
// existing config reader are observations, not an atomic filesystem snapshot.
func OpenDefaultReadOnlyConfig(ctx context.Context, configPath string, cpus int) (ConfigOwner, func() error, error) {
	if ctx == nil || cpus < 1 || configPath != "" {
		return nil, nil, fsruntime.ErrConfigUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	state, err := defaultLinuxStatePath()
	if err != nil {
		return nil, nil, err
	}
	service, err := app.NewRuntimeConfigService(ctx, &defaultReadOnlyConfigStore{state: state}, cpus)
	if err != nil {
		return nil, nil, err
	}
	return service, func() error { return nil }, nil
}

func defaultLinuxStatePath() (string, error) {
	if state := os.Getenv("MCE4D_HOME"); state != "" {
		if !filepath.IsAbs(state) || filepath.Clean(state) != state {
			return "", fsruntime.ErrConfigUnsupported
		}
		return state, nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) || filepath.Clean(home) != home {
			return "", fsruntime.ErrConfigUnsupported
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) || filepath.Clean(base) != base {
		return "", fsruntime.ErrConfigUnsupported
	}
	return filepath.Join(base, "mce4d"), nil
}

type defaultReadOnlyConfigStore struct{ state string }

func (s *defaultReadOnlyConfigStore) Read(ctx context.Context, maxBytes int) (data []byte, present bool, resultErr error) {
	if s == nil || ctx == nil {
		return nil, false, fsruntime.ErrConfigUnsupported
	}
	if maxBytes < 1 || maxBytes > 1<<20 {
		return nil, false, fsruntime.ErrConfigLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	root, err := paths.OpenRoot(s.state)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, ctx.Err()
	}
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if err := root.Close(); err != nil {
			data, present = nil, false
			resultErr = errors.Join(resultErr, err)
		}
	}()
	store, err := fsruntime.NewRuntimeConfigStore(root, "runtime.json")
	if err != nil {
		return nil, false, err
	}
	return store.Read(ctx, maxBytes)
}

func (*defaultReadOnlyConfigStore) Set(ctx context.Context, _ []byte, _ int) (ports.RuntimeConfigWriteResult, error) {
	result := ports.RuntimeConfigWriteResult{State: "not_applied"}
	if ctx == nil {
		return result, fsruntime.ErrConfigUnsupported
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, fsruntime.ErrConfigUnsupported
}
