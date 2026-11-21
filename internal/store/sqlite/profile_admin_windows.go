package sqlite

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// CheckWindowsCatalogAdmin authenticates the controller and both role peers
// against a registered worktree before CLI returns an administrative owner.
// Each mutation reauthenticates the same operation and scope under its lease.
func CheckWindowsCatalogAdmin(ctx context.Context, installation, kind, worktree string) error {
	if !model.ValidUUID(worktree) || kind != "profile" && kind != "policy" {
		return ErrProfileDenied
	}
	_, err := withWindowsProfileControl(ctx, installation, func(m windowsProfilePreparation, control windowsMaintenanceFiles) (struct{}, error) {
		err := withWindowsRoleIPC(ctx, m, control, windowsIPCCatalogAdmin, kind+"\x00"+worktree, func(check func() error) error {
			return control.withStagedWorktreeRegistration(ctx, m, worktree, check)
		})
		return struct{}{}, err
	})
	return err
}

func SetWindowsCatalogProfile(ctx context.Context, installation, worktree string, raw []byte) (model.ProfileAdminData, error) {
	request, err := model.DecodeProfileAdminRequestJSON(raw, 32768)
	if err != nil || request.WorktreeID != worktree {
		return model.ProfileAdminData{}, errors.Join(ErrProfileDenied, err)
	}
	profile, ok := request.Profile.(*model.GoProfile)
	if !ok || profile.Goos != "windows" || profile.Goarch != "amd64" {
		return model.ProfileAdminData{}, catalog.ErrUnsupportedProfile
	}
	return setWindowsCatalogAdmin(ctx, installation, "profile", worktree, nil, func(store *catalog.Store) (model.ProfileAdminData, error) {
		return store.SetProfile(ctx, raw)
	})
}

func SetWindowsCatalogPolicy(ctx context.Context, installation, worktree string, raw []byte) (model.PolicyAdminData, error) {
	request, err := model.DecodePolicyAdminRequestJSON(raw, 32768)
	if err != nil || request.WorktreeID != worktree {
		return model.PolicyAdminData{}, errors.Join(ErrProfileDenied, err)
	}
	roots := append(append([]string{}, request.Policy.AllowedRoots...), request.Policy.DependencyRoots...)
	return setWindowsCatalogAdmin(ctx, installation, "policy", worktree, roots, func(store *catalog.Store) (model.PolicyAdminData, error) {
		return store.SetPolicy(ctx, raw)
	})
}

func setWindowsCatalogAdmin[T any](ctx context.Context, installation, kind, worktree string, roots []string,
	set func(*catalog.Store) (T, error)) (T, error) {
	return withWindowsProfileControl(ctx, installation, func(m windowsProfilePreparation, control windowsMaintenanceFiles) (out T, resultErr error) {
		for _, root := range roots {
			pin, err := openWindowsApprovedRegistrationRoot(root, m.TargetRoots)
			if err != nil {
				return out, err
			}
			defer func() { resultErr = errors.Join(resultErr, pin.Close()) }()
		}
		err := withWindowsRoleIPC(ctx, m, control, windowsIPCCatalogAdmin, kind+"\x00"+worktree, func(check func() error) error {
			var err error
			out, err = runWindowsCatalogTransaction(ctx, control, m, check, func(store *catalog.Store) (_ T, resultErr error) {
				name, err := store.ReadWorktreeRoot(ctx, worktree, func(context.Context) error { return check() })
				if err != nil {
					var empty T
					return empty, err
				}
				target, err := openWindowsApprovedRegistrationRoot(name, m.TargetRoots)
				if err != nil {
					var empty T
					return empty, err
				}
				defer func() { resultErr = errors.Join(resultErr, target.Close()) }()
				return set(store)
			})
			return err
		})
		return out, err
	})
}
