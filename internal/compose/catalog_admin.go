package compose

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// CatalogAdmin borrows an existing catalog, scoped mutation lease, and an
// authenticated owner callback. It neither opens a store nor grants access.
// Authorize must check the current actor and exact operation/worktree without
// reentering the catalog pool, including on the pre-commit check.
type CatalogAdmin struct {
	Store       *catalog.Store
	Mutation    ports.CatalogMutationLease
	OperationID string
	WorktreeID  string
	Authorize   func(context.Context, string, string) error
}

func (a *CatalogAdmin) bound(ctx context.Context, kind, worktree string) (*catalog.Store, error) {
	if a == nil || a.Store == nil || a.Mutation == nil || a.Authorize == nil || ctx == nil || worktree != a.WorktreeID {
		return nil, app.ErrAdminDenied
	}
	authorize := a.Authorize
	return a.Store.WithAuthorizedMutation(ctx, a.Mutation, a.OperationID, func(ctx context.Context) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := authorize(ctx, kind, worktree); err != nil {
			return app.ErrAdminDenied
		}
		return ctx.Err()
	})
}

func (a *CatalogAdmin) SetProfile(ctx context.Context, raw []byte) (model.ProfileAdminData, error) {
	request, err := model.DecodeProfileAdminRequestJSON(raw, 32768)
	if err != nil {
		return model.ProfileAdminData{}, err
	}
	store, err := a.bound(ctx, "profile", request.WorktreeID)
	if err != nil {
		return model.ProfileAdminData{}, catalogAdminError(err)
	}
	data, err := store.SetProfile(ctx, append([]byte(nil), raw...))
	return data, catalogAdminError(err)
}

func (a *CatalogAdmin) SetPolicy(ctx context.Context, raw []byte) (model.PolicyAdminData, error) {
	request, err := model.DecodePolicyAdminRequestJSON(raw, 32768)
	if err != nil {
		return model.PolicyAdminData{}, err
	}
	store, err := a.bound(ctx, "policy", request.WorktreeID)
	if err != nil {
		return model.PolicyAdminData{}, catalogAdminError(err)
	}
	data, err := store.SetPolicy(ctx, append([]byte(nil), raw...))
	return data, catalogAdminError(err)
}

func catalogAdminError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, app.ErrAdminDenied):
		return app.ErrAdminDenied
	case errors.Is(err, catalog.ErrRevisionConflict):
		return app.ErrAdminConflict
	case errors.Is(err, catalog.ErrWorktreeUnavailable):
		return app.ErrAdminUnavailable
	case errors.Is(err, catalog.ErrUnsupportedProfile), errors.Is(err, catalog.ErrCatalogWriterRequired):
		return app.ErrAdminUnsupported
	default:
		// Includes commit/cleanup uncertainty. The caller must not retry blindly.
		return err
	}
}
