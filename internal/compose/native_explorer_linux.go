//go:build linux

package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func OpenNativeExplorer(ctx context.Context, installation, selected, worktree, profile string, cpus int) (explorer.Options, func() error, error) {
	return openNativeExplorer(ctx, installation, selected, worktree, profile, cpus, nil)
}

// OpenNativeExplorerWithObservedRuns is for an installation owner that already
// admits local exports and supplies verified source bindings and receipt checks.
func OpenNativeExplorerWithObservedRuns(ctx context.Context, installation, selected, worktree, profile string,
	cpus int, observed ObservedRunAuthority) (explorer.Options, func() error, error) {
	if observed == nil {
		return explorer.Options{}, nil, app.ErrCoreInput
	}
	return openNativeExplorer(ctx, installation, selected, worktree, profile, cpus, observed)
}

func openNativeExplorer(ctx context.Context, installation, selected, worktree, profile string, cpus int,
	observed ObservedRunAuthority) (explorer.Options, func() error, error) {
	native, close, err := openNativeCoreParts(ctx, installation, selected, worktree, profile, cpus, false, false)
	if err != nil {
		return explorer.Options{}, nil, err
	}
	reader, err := newExplorerReader(native.backend, native.effective, observed)
	if err != nil {
		return explorer.Options{}, nil, errors.Join(err, close())
	}
	options := explorer.Options{Reader: reader,
		QueryTimeout: time.Duration(min(native.effective.Core.QueryTimeoutMs, 60000)) * time.Millisecond,
		BearerTTL:    time.Duration(native.effective.Explorer.BearerTtlSeconds) * time.Second}
	profileConfig := native.scope.binding.Profile
	if _, approved := nativeRenameExecutable(native.scope.binding.Policy, "go"); approved &&
		profileConfig != nil && profileConfig.AnalysisMode == "typed_when_available" && !profileConfig.TestsEnabled && !profileConfig.CgoEnabled {
		reader.staticFlow = native.scope.projectStaticFlow
		options.StaticFlow = true
	}
	if sqliteowner.NativeUIMapsEnabled() && native.backend.Reader.Index.SchemaVersion() == 4 {
		owner, err := newNativePreviewAuthority(native)
		if err != nil {
			return explorer.Options{}, nil, errors.Join(err, close())
		}
		options.Preview = explorer.NewPreviewSessions(owner, native.backend.Cursor,
			min(native.effective.Core.MaxOutputBytes, owner.scope.binding.Policy.MaxOutputBytes))
		options.PreviewOrigins = owner.FrameOrigins
		options.Records, err = bindNativeRecords(native.scope, native.backend.Cursor)
		if err != nil {
			return explorer.Options{}, nil, errors.Join(err, close())
		}
		options.RecordOutputCap = min(native.effective.Core.MaxOutputBytes, native.scope.binding.Policy.MaxOutputBytes, 131072)
		reader.selection, err = app.NewCoreService(native.config, native.backend.Authority, native.backend, nil)
		if err != nil {
			return explorer.Options{}, nil, errors.Join(err, close())
		}
		options.SelectionContext = true
	}
	return options, close, nil
}
