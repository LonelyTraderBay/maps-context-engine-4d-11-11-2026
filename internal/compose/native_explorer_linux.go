//go:build linux

package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
)

func OpenNativeExplorer(ctx context.Context, installation, selected, worktree, profile string, cpus int) (explorer.Options, func() error, error) {
	native, close, err := openNativeCoreParts(ctx, installation, selected, worktree, profile, cpus, false, false)
	if err != nil {
		return explorer.Options{}, nil, err
	}
	reader, err := NewExplorerReader(native.backend, native.effective)
	if err != nil {
		return explorer.Options{}, nil, errors.Join(err, close())
	}
	return explorer.Options{Reader: reader,
		QueryTimeout: time.Duration(min(native.effective.Core.QueryTimeoutMs, 60000)) * time.Millisecond,
		BearerTTL:    time.Duration(native.effective.Explorer.BearerTtlSeconds) * time.Second}, close, nil
}
