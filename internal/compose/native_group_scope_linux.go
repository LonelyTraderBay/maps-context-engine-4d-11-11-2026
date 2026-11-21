//go:build linux

package compose

import (
	"context"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

// RunNativeGroupScope resolves only explicitly declared group metadata.
// Each scope member is opened through its own protected read owner.
func RunNativeGroupScope(ctx context.Context, installation, selected string, request model.StatusRequest, cpus int) (_ model.ScopeVector, resultErr error) {
	defer func() {
		switch {
		case errors.Is(resultErr, ErrScopeInput):
			resultErr = app.ErrCoreInput
		case errors.Is(resultErr, ErrGroupMemberUnavailable), errors.Is(resultErr, ErrGroupProfileMissing):
			resultErr = app.ErrValidationScopeChanged
		case errors.Is(resultErr, sqliteowner.ErrProfileDenied), errors.Is(resultErr, app.ErrCoreAuthentication):
			resultErr = app.ErrValidationAuthority
		default:
			resultErr = coreStoreError(resultErr)
		}
	}()
	if request.Scope == nil || request.Verify != nil && *request.Verify != "none" {
		return model.ScopeVector{}, app.ErrCoreUnsupported
	}
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return model.ScopeVector{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	service, err := protectedConfig(ctx, state, selected, cpus)
	if err != nil {
		return model.ScopeVector{}, err
	}
	effective, _, err := service.StartupConfig()
	if err != nil {
		return model.ScopeVector{}, err
	}
	timeout, err := nativeMilliseconds(effective.Core.QueryTimeoutMs)
	if err != nil {
		return model.ScopeVector{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cat, err := state.OpenCatalog(bounded)
	if err != nil {
		return model.ScopeVector{}, err
	}
	actor := app.CorePrincipal{ID: "installation:" + state.InstallationID()}
	authorize := func(ctx context.Context, principal app.CorePrincipal) error {
		if principal != actor {
			return ErrAuthority
		}
		return state.Check(ctx)
	}
	resolver := GroupScopeResolver{
		ReadGroup: func(ctx context.Context, principal app.CorePrincipal, groupID string) (model.GroupSummary, string, error) {
			if err := authorize(ctx, principal); err != nil {
				return model.GroupSummary{}, "", err
			}
			group, incarnation, err := cat.ObserveGroup(ctx, groupID)
			if errors.Is(err, catalog.ErrGroupUnavailable) {
				return model.GroupSummary{}, "", ErrAuthority
			}
			if err != nil {
				return model.GroupSummary{}, "", err
			}
			return group, incarnation, authorize(ctx, principal)
		},
		AuthorizeMember: func(ctx context.Context, principal app.CorePrincipal, member model.GroupMemberConfig) error {
			if err := authorize(ctx, principal); err != nil {
				return err
			}
			root, err := cat.ReadWorktreeRoot(ctx, member.WorktreeID, state.Check)
			if errors.Is(err, catalog.ErrWorktreeUnavailable) {
				return ErrGroupMemberUnavailable
			}
			if err != nil {
				return err
			}
			if !state.TargetApproved(root) {
				return ErrAuthority
			}
			return authorize(ctx, principal)
		},
		ReadMember: func(ctx context.Context, principal app.CorePrincipal, member model.GroupMemberConfig, generation string) (_ model.ScopeVector, resultErr error) {
			if err := authorize(ctx, principal); err != nil {
				return model.ScopeVector{}, err
			}
			_, err := cat.ReadBinding(ctx, member.WorktreeID, member.ProfileID, func(ctx context.Context, _, _ string) error {
				return authorize(ctx, principal)
			})
			if errors.Is(err, catalog.ErrProfileUnavailable) {
				return model.ScopeVector{}, ErrGroupProfileMissing
			}
			if err != nil {
				return model.ScopeVector{}, err
			}
			owner, closeOwner, err := openNativeCoreParts(ctx, installation, selected, member.WorktreeID, member.ProfileID, cpus, false, false)
			if errors.Is(err, os.ErrPermission) || errors.Is(err, sqliteowner.ErrProfileDenied) {
				return model.ScopeVector{}, ErrAuthority
			}
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, sqliteowner.ErrStoreRecoveryRequired) {
				return model.ScopeVector{}, ErrGroupMemberUnavailable
			}
			if err != nil {
				return model.ScopeVector{}, err
			}
			defer func() { resultErr = errors.Join(resultErr, closeOwner()) }()
			p, err := owner.backend.Authority.Authenticate(ctx)
			if err != nil {
				return model.ScopeVector{}, err
			}
			if p.ID != principal.ID {
				return model.ScopeVector{}, ErrAuthority
			}
			observed, err := owner.backend.Reader.Resolve(ctx, p, generation)
			return observed.Vector, err
		},
	}
	if request.Scope.PinnedVector != nil {
		return resolver.Recheck(bounded, actor, *request.Scope.PinnedVector)
	}
	if request.Scope.GroupID == nil {
		return model.ScopeVector{}, ErrScopeInput
	}
	vector, err := resolver.Resolve(bounded, actor, *request.Scope.GroupID)
	if err != nil {
		return model.ScopeVector{}, err
	}
	return resolver.Recheck(bounded, actor, vector)
}
