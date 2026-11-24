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
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
	"golang.org/x/sys/unix"
)

// Set once at executable startup, before handlers or child tools exist. Go's
// cache respects this inherited mask; changing it per concurrent request would
// be a process-wide race. This does not alter the launching shell's mask.
func prepareNativeProcess() { unix.Umask(0077) }

func nativeLocalAdminError(err error) error {
	switch {
	case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
		return app.ErrCoreStoreRecovery
	case errors.Is(err, catalog.ErrCatalogCommitUnknown), errors.Is(err, sqlite.ErrNativeOutcomeUnknown),
		errors.Is(err, index.ErrCommitUnknown), errors.Is(err, index.ErrAfterCommit):
		return errors.Join(cli.ErrIndexRecovery, err)
	case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, sqlite.ErrProfileIncomplete), errors.Is(err, compose.ErrAuthority),
		errors.Is(err, index.ErrGenerationConflict):
		return errors.Join(cli.ErrBindingDenied, err)
	case errors.Is(err, sqlite.ErrCacheCapacity):
		return errors.Join(app.ErrCoreLimit, err)
	default:
		return err
	}
}

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
	deps.OpenExplorer = func(ctx context.Context, selected, worktree, profile string) (explorer.Options, func() error, error) {
		options, release, err := compose.OpenNativeExplorer(ctx, root, selected, worktree, profile, deps.CPUs)
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = app.ErrCoreStoreRecovery
		}
		return options, release, err
	}
	deps.GroupAdmin = func(ctx context.Context, selected string, request model.GroupAdminRequest) (model.GroupAdminData, *string, error) {
		data, next, err := compose.RunNativeGroupAdmin(ctx, root, selected, request, deps.CPUs)
		if errors.Is(err, sqlite.ErrProfileDenied) {
			err = cli.ErrBindingDenied
		}
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = app.ErrCoreStoreRecovery
		}
		return data, next, err
	}
	deps.GroupScope = func(ctx context.Context, selected string, request model.StatusRequest) (model.ScopeVector, error) {
		return compose.RunNativeGroupScope(ctx, root, selected, request, deps.CPUs)
	}
	if sqlite.NativeUIMapsEnabled() {
		deps.PreviewPermission = func(ctx context.Context, selected string, request model.PreviewPermissionAdminRequest) (model.PreviewPermissionAdminData, *string, error) {
			data, next, err := compose.RunNativePreviewPermission(ctx, root, selected, request, deps.CPUs)
			if errors.Is(err, sqlite.ErrProfileDenied) {
				err = cli.ErrBindingDenied
			}
			if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
				err = app.ErrCoreStoreRecovery
			}
			return data, next, err
		}
		deps.ImportUIMap = func(ctx context.Context, selected, worktree, artifact string) (cli.UIMapImportReport, error) {
			report, err := compose.RunNativeUIMapImport(ctx, root, selected, worktree, artifact, deps.CPUs)
			switch {
			case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
				err = app.ErrCoreStoreRecovery
			case errors.Is(err, sqlite.ErrNativeOutcomeUnknown), errors.Is(err, index.ErrCommitUnknown), errors.Is(err, index.ErrAfterCommit):
				err = errors.Join(cli.ErrIndexRecovery, err)
			case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, app.ErrCoreAuthentication), errors.Is(err, compose.ErrAuthority):
				err = cli.ErrBindingDenied
			case errors.Is(err, index.ErrMigrationRequired):
				err = app.ErrCoreUnsupported
			}
			return cli.UIMapImportReport{BuildMapID: report.BuildMapID, BindingID: report.BindingID, Status: report.Status}, err
		}
		deps.WatchUIMap = func(ctx context.Context, selected, worktree, profile, directory string, emit func(cli.UIMapWatchReport) error) error {
			err := compose.RunNativeUIMapWatch(ctx, root, selected, worktree, profile, directory, deps.CPUs, func(event compose.UIMapWatchEvent) error {
				return emit(cli.UIMapWatchReport(event))
			})
			switch {
			case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
				return app.ErrCoreStoreRecovery
			case errors.Is(err, sqlite.ErrNativeOutcomeUnknown), errors.Is(err, index.ErrCommitUnknown), errors.Is(err, index.ErrAfterCommit):
				return errors.Join(cli.ErrIndexRecovery, err)
			case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, app.ErrCoreAuthentication), errors.Is(err, compose.ErrAuthority):
				return cli.ErrBindingDenied
			case errors.Is(err, index.ErrMigrationRequired):
				return app.ErrCoreUnsupported
			default:
				return err
			}
		}
	}
	deps.OpenRecords = func(ctx context.Context, selected, worktree, profile string) (*app.RecordService, func() error, error) {
		service, release, err := compose.OpenNativeRecords(ctx, root, selected, worktree, profile, deps.CPUs)
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = app.ErrCoreStoreRecovery
		} else if errors.Is(err, sqlite.ErrProfileDenied) {
			err = cli.ErrBindingDenied
		}
		return service, release, err
	}
	deps.AddRepo = func(ctx context.Context, selected, path string) (cli.RegisteredRepo, error) {
		registered, err := compose.RegisterNativeRepo(ctx, root, selected, path, deps.CPUs)
		return cli.RegisteredRepo{WorktreeID: registered.WorktreeID, ProfileID: registered.ProfileID}, err
	}
	deps.ListRepos = func(ctx context.Context, selected string) ([]model.LocalWorktree, error) {
		entries, err := compose.RunNativeRepoList(ctx, root, selected, "")
		return entries, nativeLocalAdminError(err)
	}
	deps.UpdateRepo = func(ctx context.Context, selected, worktree, target string, purge bool) (int64, error) {
		revision, err := compose.RunNativeRepoUpdate(ctx, root, selected, worktree, target, purge, deps.CPUs)
		return revision, nativeLocalAdminError(err)
	}
	deps.CacheProfile = func(ctx context.Context, selected, worktree string) (string, error) {
		profile, err := compose.ResolveNativeCacheProfile(ctx, root, selected, worktree)
		return profile, nativeLocalAdminError(err)
	}
	deps.InspectCache = func(ctx context.Context, selected, worktree string) (model.LocalCacheUsage, error) {
		report, err := compose.InspectNativeCache(ctx, root, selected, worktree, deps.CPUs)
		return report, nativeLocalAdminError(err)
	}
	deps.Index = func(ctx context.Context, selected, worktree, profile string, full, watch bool, progress func(cli.IndexProgress) error) error {
		err := compose.RunNativeIndex(ctx, root, selected, worktree, profile, deps.CPUs, full, watch, func(value compose.NativeIndexProgress) error {
			return progress(cli.IndexProgress{GenerationID: value.GenerationID, SnapshotDigest: value.SnapshotDigest, Files: value.Files, Unchanged: value.Unchanged, Typed: value.Typed,
				ProviderID: value.ProviderID, ProviderStatus: value.ProviderStatus})
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
	deps.StoreRebuildIndex = func(ctx context.Context, worktree string, wait time.Duration) (cli.StoreRestoreReport, error) {
		result, err := sqlite.RebuildProtectedIndex(ctx, root, worktree, true, wait)
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = app.ErrCoreStoreRecovery
		}
		return cli.StoreRestoreReport(result), err
	}
}
