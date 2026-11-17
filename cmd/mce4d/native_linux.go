//go:build linux

package main

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"golang.org/x/sys/unix"
)

// Set once at executable startup, before handlers or child tools exist. Go's
// cache respects this inherited mask; changing it per concurrent request would
// be a process-wide race. This does not alter the launching shell's mask.
func prepareNativeProcess() { unix.Umask(0077) }

func configureNativeDependencies(deps *cli.Dependencies) {
	root, err := sqlite.ProtectedProfileRootFromEnvironment()
	if err != nil {
		return
	}
	deps.OpenConfig = func(ctx context.Context, selected string) (cli.ConfigOwner, func() error, error) {
		return compose.OpenNativeConfig(ctx, root, selected, deps.CPUs)
	}
	deps.OpenAdmin = func(ctx context.Context, selected, kind, worktree string) (cli.AdminOwner, func() error, error) {
		return compose.OpenNativeAdmin(ctx, root, selected, kind, worktree)
	}
	deps.OpenCore = func(ctx context.Context, selected, worktree, profile string) (*app.CoreService, func() error, error) {
		service, release, err := compose.OpenNativeCore(ctx, root, selected, worktree, profile, deps.CPUs)
		if errors.Is(err, app.ErrCoreAuthentication) || errors.Is(err, sqlite.ErrProfileDenied) {
			err = cli.ErrBindingDenied
		}
		return service, release, err
	}
	deps.OpenRetainedCore = func(ctx context.Context, selected, worktree, profile string) (*app.CoreService, func() error, error) {
		service, release, err := compose.OpenNativeRetainedCore(ctx, root, selected, worktree, profile, deps.CPUs)
		if errors.Is(err, app.ErrCoreAuthentication) || errors.Is(err, sqlite.ErrProfileDenied) {
			err = cli.ErrBindingDenied
		}
		return service, release, err
	}
	deps.AddRepo = func(ctx context.Context, selected, path string) (cli.RegisteredRepo, error) {
		registered, err := compose.RegisterNativeRepo(ctx, root, selected, path, deps.CPUs)
		return cli.RegisteredRepo{WorktreeID: registered.WorktreeID, ProfileID: registered.ProfileID}, err
	}
	deps.Index = func(ctx context.Context, selected, worktree, profile string, full, watch bool, progress func(cli.IndexProgress) error) error {
		err := compose.RunNativeIndex(ctx, root, selected, worktree, profile, deps.CPUs, full, watch, func(value compose.NativeIndexProgress) error {
			return progress(cli.IndexProgress{GenerationID: value.GenerationID, SnapshotDigest: value.SnapshotDigest, Files: value.Files, Unchanged: value.Unchanged})
		})
		switch {
		case errors.Is(err, sqlite.ErrNativeOutcomeUnknown), errors.Is(err, index.ErrCommitUnknown), errors.Is(err, index.ErrAfterCommit):
			return errors.Join(cli.ErrIndexRecovery, err)
		case errors.Is(err, sqlite.ErrCacheCapacity):
			return errors.Join(app.ErrCoreLimit, err)
		case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, compose.ErrAuthority):
			return errors.Join(cli.ErrBindingDenied, err)
		default:
			return err
		}
	}
}
