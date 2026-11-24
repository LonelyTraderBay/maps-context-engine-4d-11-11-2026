package compose

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// VerifyDelivery performs a final live authority and expiration check after
// response serialization. It does not emit bytes or renew any lifetime.
func (b *CoreBackend) VerifyDelivery(ctx context.Context, principal app.CorePrincipal, _ string, request model.Request, response model.Response) error {
	if !b.valid() || ctx == nil {
		return app.ErrCoreInput
	}
	if err := b.Authority.samePrincipal(ctx, principal); err != nil {
		return app.ErrValidationAuthority
	}
	if response.Scope != nil {
		if _, err := b.Reader.Recheck(ctx, principal, *response.Scope); err != nil {
			return coreStoreError(err)
		}
	}
	switch r := request.(type) {
	case model.StatusRequest:
		if (r.Verify == nil || *r.Verify == "none") && (response.Freshness.Status != "not_checked" || response.Freshness.Scope != "none" || response.Freshness.CheckedAt != nil || response.Freshness.ChangedCount != nil) {
			return app.ErrCoreBackend
		}
		if response.Scope == nil {
			data, ok := response.Data.(model.StatusData)
			if !ok || data.IndexState != "missing" {
				return app.ErrCoreBackend
			}
			if _, err := b.Reader.ObserveMissingIndex(ctx, principal); err != nil {
				return coreStoreError(err)
			}
			if r.Verify != nil && *r.Verify == "workspace" && (response.Freshness.Status != "unknown" || response.Freshness.Scope != "none") {
				return app.ErrCoreBackend
			}
		} else if r.Verify != nil && *r.Verify == "workspace" {
			observation, err := b.resolveRequestScope(ctx, principal, r.Scope)
			if err != nil {
				return coreStoreError(err)
			}
			if model.ComparePinnedScope(*response.Scope, observation.Vector, 32768) != nil {
				return app.ErrValidationScopeChanged
			}
			freshness, err := b.statusWorkspaceFreshness(ctx, principal, observation)
			if err != nil {
				return err
			}
			if response.Freshness.Scope != "workspace" || response.Freshness.CheckedAt == nil || response.Freshness.Status != freshness.Status {
				return app.ErrFreshnessChanged
			}
			if freshness.Status == "observed_current" && (response.Freshness.ChangedCount == nil || *response.Freshness.ChangedCount != 0) || freshness.Status == "changed" && response.Freshness.ChangedCount != nil {
				return app.ErrCoreBackend
			}
		}
		return ctx.Err()
	case model.LocateRequest:
		if r.Mode == "topic" {
			return b.verifyTopicDelivery(ctx, principal, r, response)
		}
		if response.Pagination.NextCursor == nil {
			return ctx.Err()
		}
		if response.Scope == nil {
			return app.ErrCoreBackend
		}
		guard := func(ctx context.Context, binding contextpack.CursorBinding) error {
			if binding.PrincipalDigest != principal.Digest || binding.BindingDigest != principal.BindingDigest || binding.OrderVersion != "locate-id-v1" {
				return ErrAuthority
			}
			_, err := b.Reader.Recheck(ctx, principal, binding.Scope)
			return err
		}
		claims, err := b.Cursor.VerifyPrincipal(ctx, *response.Pagination.NextCursor, "list", principal.Digest, guard)
		if err != nil {
			return coreStoreError(err)
		}
		r.Cursor, r.Limit = nil, 20
		digest, err := model.RequestDigest("locate", principal.ID, "LocateRequest", r, 32768, 4)
		if err != nil || claims.Binding.RequestDigest != digest || model.ComparePinnedScope(*response.Scope, claims.Binding.Scope, 32768) != nil {
			return contextpack.ErrCursorInvalid
		}
		return ctx.Err()
	case model.PackRequest, model.ExpandRequest:
		data, ok := response.Data.(model.PackData)
		if !ok {
			return app.ErrCoreBackend
		}
		if expand, ok := request.(model.ExpandRequest); ok && data.PackToken != expand.PackToken {
			return app.ErrCoreBackend
		}
		expectedDigest := ""
		var previousCursor *string
		if initial, ok := request.(model.PackRequest); ok {
			var err error
			expectedDigest, err = model.RequestDigest("context_pack", principal.ID, "PackRequest", initial, 65536, 4)
			if err != nil {
				return app.ErrCoreInput
			}
		}
		if expand, ok := request.(model.ExpandRequest); ok {
			previousCursor = &expand.NextCursor
		}
		return b.verifyPackDelivery(ctx, principal, data.PackToken, expectedDigest, previousCursor, response)
	case model.ValidateRequest:
		if r.ResultToken != nil && r.PackToken == nil {
			return b.verifyRenameDelivery(ctx, principal, request, response)
		}
		if r.PackToken == nil {
			return app.ErrCoreUnsupported
		}
		return b.verifyPackDelivery(ctx, principal, *r.PackToken, "", nil, response)
	case *model.RenameInitialRequest, *model.RenameContinueRequest:
		return b.verifyRenameDelivery(ctx, principal, request, response)
	default:
		return app.ErrCoreInput
	}
}

func (b *CoreBackend) verifyPackDelivery(ctx context.Context, principal app.CorePrincipal, token, expectedDigest string, previousCursor *string, response model.Response) error {
	if b.packCache == nil || response.Scope == nil {
		return app.ErrCoreBackend
	}
	guard := b.packGuard(principal)
	var recheck func(context.Context) error
	needInitialCheck := true
	if response.Budget.DeliveryMode == "cli" {
		release, err := b.acquireCLIPackWork(ctx)
		if err != nil {
			return coreStoreError(err)
		}
		defer release()
		if handle := app.SymbolPackForDelivery(ctx, token, principal.Digest); handle != nil {
			// This request checked the same handle after its last page or validation.
			needInitialCheck = false
			recheck = handle.Recheck
		} else {
			handle, err := b.loadCLIPack(ctx, principal, token)
			if err != nil {
				return coreStoreError(err)
			}
			recheck = handle.Recheck
		}
	} else {
		recheck = func(ctx context.Context) error {
			_, err := b.packCache.GetRetained(ctx, token, principal.Digest, guard)
			return err
		}
	}
	if needInitialCheck {
		if err := recheck(ctx); err != nil {
			return coreStoreError(err)
		}
	}
	packClaims, err := b.Cursor.VerifyPrincipal(ctx, token, "pack", principal.Digest, guard)
	if err != nil {
		return coreStoreError(err)
	}
	if expectedDigest != "" && expectedDigest != packClaims.Binding.RequestDigest || model.ComparePinnedScope(*response.Scope, packClaims.Binding.Scope, 32768) != nil {
		return contextpack.ErrCursorInvalid
	}
	matchesPack := func(cursor contextpack.SignedCursor) bool {
		return cursor.Binding.ResultID == packClaims.Binding.ResultID && cursor.Binding.ResultDigest == packClaims.Binding.ResultDigest &&
			cursor.Binding.RequestDigest == packClaims.Binding.RequestDigest && cursor.Binding.BindingDigest == packClaims.Binding.BindingDigest &&
			cursor.Binding.OrderVersion == packClaims.Binding.OrderVersion &&
			model.ComparePinnedScope(cursor.Binding.Scope, packClaims.Binding.Scope, 32768) == nil
	}
	var previous contextpack.SignedCursor
	if previousCursor != nil {
		previous, err = b.Cursor.VerifyPrincipal(ctx, *previousCursor, "list", principal.Digest, guard)
		if err != nil {
			return coreStoreError(err)
		}
		if !matchesPack(previous) {
			return contextpack.ErrCursorInvalid
		}
	}
	if response.Pagination.NextCursor != nil {
		cursor, err := b.Cursor.VerifyPrincipal(ctx, *response.Pagination.NextCursor, "list", principal.Digest, guard)
		if err != nil {
			return coreStoreError(err)
		}
		if !matchesPack(cursor) || previousCursor != nil && !contextpack.CursorAdvances(previous.Position, cursor.Position) {
			return contextpack.ErrCursorInvalid
		}
	}
	if err := recheck(ctx); err != nil {
		return coreStoreError(err)
	}
	return ctx.Err()
}
