//go:build linux

package main

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
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
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = app.ErrCoreStoreRecovery
		} else if errors.Is(err, app.ErrCoreAuthentication) || errors.Is(err, sqlite.ErrProfileDenied) {
			err = cli.ErrBindingDenied
		}
		return service, release, err
	}
	deps.OpenRetainedCore = func(ctx context.Context, selected, worktree, profile string) (*app.CoreService, func() error, error) {
		service, release, err := compose.OpenNativeRetainedCore(ctx, root, selected, worktree, profile, deps.CPUs)
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = app.ErrCoreStoreRecovery
		} else if errors.Is(err, app.ErrCoreAuthentication) || errors.Is(err, sqlite.ErrProfileDenied) {
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
			return progress(cli.IndexProgress{GenerationID: value.GenerationID, SnapshotDigest: value.SnapshotDigest, Files: value.Files, Unchanged: value.Unchanged, Typed: value.Typed})
		})
		switch {
		case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
			return app.ErrCoreStoreRecovery
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
	deps.StoreGC = func(ctx context.Context, selected, worktree, profile string, apply bool) (cli.StoreGCReport, error) {
		value, err := compose.RunNativeGC(ctx, root, selected, worktree, profile, deps.CPUs, apply)
		if err != nil {
			switch {
			case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
				err = app.ErrCoreStoreRecovery
			case errors.Is(err, sqlite.ErrNativeOutcomeUnknown), errors.Is(err, index.ErrCommitUnknown), errors.Is(err, index.ErrAfterCommit):
				err = errors.Join(cli.ErrIndexRecovery, err)
			case errors.Is(err, sqlite.ErrCacheCapacity):
				err = errors.Join(app.ErrCoreLimit, err)
			case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, compose.ErrAuthority):
				err = errors.Join(cli.ErrBindingDenied, err)
			}
			return cli.StoreGCReport{}, err
		}
		report := cli.StoreGCReport{WorktreeID: worktree, ProfileID: profile, KeepGenerations: value.KeepGenerations, DryRun: !apply, More: value.More, Generations: []cli.StoreGCGeneration{}}
		for _, generation := range value.Generations {
			report.Generations = append(report.Generations, cli.StoreGCGeneration{GenerationID: generation.GenerationID, Applied: generation.Applied, AlreadyAbsent: generation.AlreadyAbsent, Rows: generation.Rows, BlobBytes: generation.BlobBytes})
		}
		return report, nil
	}
	deps.StoreBackup = func(ctx context.Context, selected, target, worktree, output string) (cli.StoreBackupReport, error) {
		value, err := compose.RunNativeBackup(ctx, root, selected, target, worktree, output, deps.CPUs)
		if err != nil {
			switch {
			case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
				err = app.ErrCoreStoreRecovery
			case errors.Is(err, index.ErrSchemaTooNew), errors.Is(err, records.ErrSchemaTooNew), errors.Is(err, catalog.ErrSchemaTooNew):
				err = errors.Join(cli.ErrStoreSchemaTooNew, err)
			case errors.Is(err, sqlite.ErrStateLimit), errors.Is(err, catalog.ErrBackupLimit):
				err = errors.Join(app.ErrCoreLimit, err)
			case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, compose.ErrAuthority):
				err = errors.Join(cli.ErrBindingDenied, err)
			}
			return cli.StoreBackupReport{}, err
		}
		return cli.StoreBackupReport{OperationID: value.OperationID, Target: value.Target, WorktreeID: value.WorktreeID, ManifestSHA256: value.ManifestSHA256, FileCount: value.FileCount, TotalBytes: value.TotalBytes}, nil
	}
	deps.StoreRecoveryReport = func(ctx context.Context, target, worktree string) (cli.StoreRecoveryReport, error) {
		r, err := sqlite.ReadProtectedRecoveryReport(ctx, root, target, worktree)
		return cli.StoreRecoveryReport{Target: r.Target, WorktreeID: r.WorktreeID, ReportOnly: r.ReportOnly, JournalPresent: r.JournalPresent, OperationID: r.OperationID, Phase: r.Phase, Outcome: r.Outcome, CompletedSteps: r.CompletedSteps, ActualStateVerified: r.ActualStateVerified}, err
	}
	deps.StoreRestore = func(ctx context.Context, manifest, target, worktree string, wait time.Duration) (cli.StoreRestoreReport, error) {
		var result sqlite.ProtectedRestoreSummary
		var err error
		if manifest != "" {
			result, err = sqlite.RestoreProtectedStore(ctx, root, manifest, true, wait)
		} else {
			result, err = sqlite.RecoverProtectedStore(ctx, root, target, worktree, true, wait)
		}
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = app.ErrCoreStoreRecovery
		}
		return cli.StoreRestoreReport(result), err
	}
}
