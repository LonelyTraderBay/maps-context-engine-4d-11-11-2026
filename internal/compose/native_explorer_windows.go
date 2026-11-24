package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
)

// OpenWindowsExplorer uses the authenticated scoped pools behind Windows
// inspection to serve workspace, graph, source, and selection context.
func OpenWindowsExplorer(ctx context.Context, installation, selected, worktree, profile string, cpus int) (explorer.Options, func() error, error) {
	backend, config, release, err := openWindowsInspectionBackend(ctx, installation, selected, worktree, profile, cpus, false)
	if err != nil {
		return explorer.Options{}, nil, err
	}
	effective, _, err := config.StartupConfig()
	if err != nil {
		return explorer.Options{}, nil, errors.Join(err, release())
	}
	reader, err := NewExplorerReader(backend, effective)
	if err != nil {
		return explorer.Options{}, nil, errors.Join(err, release())
	}
	reader.selection, err = app.NewCoreService(config, backend.Authority, backend, nil)
	if err != nil {
		return explorer.Options{}, nil, errors.Join(err, release())
	}
	return explorer.Options{
		Reader:           reader,
		QueryTimeout:     time.Duration(min(effective.Core.QueryTimeoutMs, 60000)) * time.Millisecond,
		BearerTTL:        time.Duration(effective.Explorer.BearerTtlSeconds) * time.Second,
		SelectionContext: true,
	}, release, nil
}
