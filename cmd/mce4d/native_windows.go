package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func prepareNativeProcess() {}

func configureNativeDependencies(deps *cli.Dependencies) {
	root, err := sqlite.ProtectedProfileRootFromEnvironment()
	if err != nil {
		return
	}
	deps.StoreRecoveryReport = func(ctx context.Context, target, worktree string) (cli.StoreRecoveryReport, error) {
		report, err := sqlite.ReadProtectedRecoveryReport(ctx, root, target, worktree)
		return cli.StoreRecoveryReport(report), err
	}
	// IPC mode requires the protected profile controller and launches its
	// configured AppContainer roles. The PID fallback is registration only.
	engine, engineErr := strconv.ParseUint(os.Getenv("MCE4D_WINDOWS_STAGED_ENGINE_PID"), 10, 32)
	client, clientErr := strconv.ParseUint(os.Getenv("MCE4D_WINDOWS_STAGED_CLIENT_PID"), 10, 32)
	ipc := os.Getenv("MCE4D_WINDOWS_ROLE_IPC") == "1"
	if ipc {
		deps.LocalInventory = true
		deps.OpenAdmin = func(ctx context.Context, selected, kind, worktree string) (cli.AdminOwner, func() error, error) {
			if selected != "" && selected != filepath.Join(root, "runtime.json") {
				return nil, nil, app.ErrAdminDenied
			}
			if err := sqlite.CheckWindowsCatalogAdmin(ctx, root, kind, worktree); err != nil {
				return nil, nil, windowsAdminError(err)
			}
			return windowsCatalogAdmin{root: root, worktree: worktree, kind: kind}, func() error { return nil }, nil
		}
		deps.Index = func(ctx context.Context, selected, worktree, profile string, full, watch bool, progress func(cli.IndexProgress) error) error {
			err := compose.RunWindowsIndex(ctx, root, selected, worktree, profile, deps.CPUs, full, watch, func(value compose.NativeIndexProgress) error {
				return progress(cli.IndexProgress(value))
			})
			return windowsStoreMaintenanceError(err)
		}
		deps.StoreBackup = func(ctx context.Context, selected, target, worktree, output string) (cli.StoreBackupReport, error) {
			if selected != "" {
				return cli.StoreBackupReport{}, cli.ErrBindingDenied
			}
			result, err := sqlite.BackupWindowsProtectedStore(ctx, root, target, worktree, output)
			return cli.StoreBackupReport(result), windowsStoreMaintenanceError(err)
		}
		deps.StoreRestore = func(ctx context.Context, manifest, target, worktree string, wait time.Duration) (cli.StoreRestoreReport, error) {
			var result sqlite.ProtectedRestoreSummary
			var err error
			if manifest != "" {
				result, err = sqlite.RestoreWindowsProtectedStore(ctx, root, manifest, true, wait)
			} else {
				result, err = sqlite.RecoverWindowsProtectedStore(ctx, root, target, worktree, true, wait)
			}
			return cli.StoreRestoreReport(result), windowsStoreMaintenanceError(err)
		}
		deps.OpenCore = func(ctx context.Context, selected, worktree, profile string) (*app.CoreService, func() error, error) {
			service, release, err := compose.OpenWindowsInspection(ctx, root, selected, worktree, profile, deps.CPUs)
			switch {
			case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
				err = app.ErrCoreStoreRecovery
			case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, sqlite.ErrProfileInput):
				err = errors.Join(cli.ErrBindingDenied, err)
			}
			return service, release, err
		}
		deps.OpenRetainedCore = deps.OpenCore
	}
	if !ipc && (engineErr != nil || clientErr != nil || engine == 0 || client == 0 || engine == client) {
		return
	}
	deps.AddRepo = func(ctx context.Context, selected, target string) (cli.RegisteredRepo, error) {
		if selected != "" {
			return cli.RegisteredRepo{}, sqlite.ErrProfileDenied
		}
		bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		var value catalog.Registration
		var err error
		if ipc {
			value, err = sqlite.RegisterWindowsStagedRepoIPC(bounded, root, target)
		} else {
			value, err = sqlite.RegisterWindowsStagedRepo(bounded, root, target, uint32(engine), uint32(client))
		}
		if errors.Is(err, sqlite.ErrStoreRecoveryRequired) {
			err = errors.Join(app.ErrCoreStoreRecovery, err)
		}
		return cli.RegisteredRepo{WorktreeID: value.WorktreeID, ProfileID: value.ProfileID}, err
	}
}

type windowsCatalogAdmin struct{ root, worktree, kind string }

func (a windowsCatalogAdmin) SetProfile(ctx context.Context, raw []byte) (model.ProfileAdminData, error) {
	if a.kind != "profile" {
		return model.ProfileAdminData{}, app.ErrAdminDenied
	}
	out, err := sqlite.SetWindowsCatalogProfile(ctx, a.root, a.worktree, raw)
	return out, windowsAdminError(err)
}

func (a windowsCatalogAdmin) SetPolicy(ctx context.Context, raw []byte) (model.PolicyAdminData, error) {
	if a.kind != "policy" {
		return model.PolicyAdminData{}, app.ErrAdminDenied
	}
	out, err := sqlite.SetWindowsCatalogPolicy(ctx, a.root, a.worktree, raw)
	return out, windowsAdminError(err)
}

func windowsAdminError(err error) error {
	switch {
	case errors.Is(err, sqlite.ErrStoreRecoveryRequired), errors.Is(err, catalog.ErrCatalogCommitUnknown):
		return err
	case errors.Is(err, sqlite.ErrProfileDenied), errors.Is(err, sqlite.ErrProfileInput):
		return app.ErrAdminDenied
	case errors.Is(err, catalog.ErrRevisionConflict):
		return app.ErrAdminConflict
	case errors.Is(err, catalog.ErrWorktreeUnavailable):
		return app.ErrAdminUnavailable
	case errors.Is(err, catalog.ErrUnsupportedProfile):
		return app.ErrAdminUnsupported
	default:
		return err
	}
}

func windowsStoreMaintenanceError(err error) error {
	switch {
	case errors.Is(err, sqlite.ErrStoreRecoveryRequired):
		return errors.Join(app.ErrCoreStoreRecovery, err)
	case errors.Is(err, sqlite.ErrProfileInput), errors.Is(err, sqlite.ErrProfileDenied):
		return errors.Join(cli.ErrBindingDenied, err)
	case errors.Is(err, sqlite.ErrStateLimit), errors.Is(err, catalog.ErrBackupLimit):
		return errors.Join(app.ErrCoreLimit, err)
	default:
		return err
	}
}
