package compose

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// WithCLIPackRetention returns a separate backend configured to use an
// owner-provided store for packs. The owner must establish and maintain
// secure storage, complete protected roots, worktree quota, and leases; this
// method does not open state or prove those properties. The original backend
// and its process-local MCP behavior remain unchanged.
func (b *CoreBackend) WithCLIPackRetention(store app.SymbolPackStorage) (*CoreBackend, error) {
	if !b.valid() || store == nil || store.WorktreeID() != b.Reader.WorktreeID || store.MaxResultBytes() < 1024 || store.MaxResultBytes() > 32*1024*1024 {
		return nil, app.ErrCoreInput
	}
	limits, ok := store.(interface {
		WithinRetentionLimits(string, int64, int64) bool
	})
	if !ok || !limits.WithinRetentionLimits(b.Reader.WorktreeID, b.limits.QueryResultCacheBytes, b.limits.MaxRetainedQueryResults) {
		return nil, app.ErrCoreInput
	}
	copy := *b
	copy.cliPackStore = store
	return &copy, nil
}

func (b *CoreBackend) SupportsCLIPackRetention() bool {
	return b.valid() && b.cliPackStore != nil
}

func (b *CoreBackend) cliPackStoreWithinLimits() bool {
	if !b.SupportsCLIPackRetention() {
		return false
	}
	limits, ok := b.cliPackStore.(interface {
		WithinRetentionLimits(string, int64, int64) bool
	})
	return ok && limits.WithinRetentionLimits(b.Reader.WorktreeID, b.limits.QueryResultCacheBytes, b.limits.MaxRetainedQueryResults)
}

func (b *CoreBackend) cliSymbolRetention(principal app.CorePrincipal, captured *index.PublishedGraph) (*app.SymbolPackRetention, error) {
	if !b.cliPackStoreWithinLimits() {
		return nil, app.ErrCoreUnsupported
	}
	factory, err := b.symbolRestoreFactory(principal, captured, nil)
	if err != nil {
		return nil, err
	}
	ttl := minInt64(b.limits.PackTtlSeconds, 900)
	return app.NewSymbolPackRetention(b.cliPackStore, b.Cursor, int(b.cliPackStore.MaxResultBytes()), ttl, factory)
}

func (b *CoreBackend) cliPlanRetention(principal app.CorePrincipal, captured *index.PublishedGraph) (*app.PlanPackRetention, error) {
	if !b.cliPackStoreWithinLimits() {
		return nil, app.ErrCoreUnsupported
	}
	factory, err := b.planRestoreFactory(principal, captured)
	if err != nil {
		return nil, err
	}
	ttl := minInt64(b.limits.PackTtlSeconds, 900)
	return app.NewPlanPackRetention(b.cliPackStore, b.Cursor, int(b.cliPackStore.MaxResultBytes()), ttl, factory)
}

type cliRetainedPack interface {
	Continue(context.Context, string, contextpack.PackPageOptions) (contextpack.PackPage, error)
	Validate(context.Context) (app.CapturedValidation, error)
	Recheck(context.Context) error
}

// Classify authenticated bytes before rehydrating the pinned graph, then use
// the matching owner to recheck the exact token, current authority and source.
func (b *CoreBackend) loadCLIPack(ctx context.Context, principal app.CorePrincipal, token string) (cliRetainedPack, error) {
	if ctx == nil {
		return nil, app.ErrCoreInput
	}
	guard := b.packGuard(principal)
	if !b.cliPackStoreWithinLimits() {
		return nil, app.ErrCoreUnsupported
	}
	kind, err := app.IdentifyRetainedPack(ctx, b.cliPackStore, b.Cursor, token, principal.Digest, principal.ID, guard, int(b.cliPackStore.MaxResultBytes()))
	if err != nil {
		return nil, err
	}
	switch kind {
	case "symbol":
		retention, err := b.cliSymbolRetention(principal, nil)
		if err != nil {
			return nil, err
		}
		return retention.Get(ctx, token, principal.Digest, principal.ID, guard)
	case "plan":
		retention, err := b.cliPlanRetention(principal, nil)
		if err != nil {
			return nil, err
		}
		return retention.Get(ctx, token, principal.Digest, principal.ID, guard)
	default:
		return nil, app.ErrValidationAuthority
	}
}

// CLI restore may rehydrate a pinned graph. Share the backend's materializing
// gate so concurrent calls on this backend cannot all allocate it at once.
func (b *CoreBackend) acquireCLIPackWork(ctx context.Context) (func(), error) {
	if b == nil || b.buildGate == nil || ctx == nil {
		return nil, app.ErrCoreInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case b.buildGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-b.buildGate
			return nil, err
		}
		return func() { <-b.buildGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *CoreBackend) expandCLIPack(ctx context.Context, principal app.CorePrincipal, request model.ExpandRequest, options app.CoreCallOptions) ([][]byte, error) {
	release, err := b.acquireCLIPackWork(ctx)
	if err != nil {
		return nil, coreStoreError(err)
	}
	defer release()
	guard := b.packGuard(principal)
	handle, err := b.loadCLIPack(ctx, principal, request.PackToken)
	if err != nil {
		return nil, coreStoreError(err)
	}
	claims, err := b.Cursor.VerifyPrincipal(ctx, request.PackToken, "pack", principal.Digest, guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	observation, err := b.Reader.Recheck(ctx, principal, claims.Binding.Scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if options.MaxOutputBytes > observation.Binding.Policy.MaxOutputBytes {
		options.MaxOutputBytes = observation.Binding.Policy.MaxOutputBytes
	}
	page, err := handle.Continue(ctx, request.NextCursor, packPageOptions(options))
	if err != nil {
		guarded, _ := app.GuardOversizedError(err, handle.Recheck, nil)
		return nil, coreStoreError(guarded)
	}
	if err := handle.Recheck(ctx); err != nil {
		return nil, coreStoreError(err)
	}
	if symbol, ok := handle.(*app.RetainedSymbolPack); ok {
		app.RememberSymbolPackDelivery(ctx, symbol)
	}
	return page.Blocks, nil
}

func (b *CoreBackend) validateCLIPack(ctx context.Context, principal app.CorePrincipal, request model.ValidateRequest, options app.CoreCallOptions) ([][]byte, error) {
	if request.PackToken == nil {
		return nil, app.ErrCoreUnsupported
	}
	release, err := b.acquireCLIPackWork(ctx)
	if err != nil {
		return nil, coreStoreError(err)
	}
	defer release()
	guard := b.packGuard(principal)
	handle, err := b.loadCLIPack(ctx, principal, *request.PackToken)
	if err != nil {
		return nil, coreStoreError(err)
	}
	claims, err := b.Cursor.VerifyPrincipal(ctx, *request.PackToken, "pack", principal.Digest, guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	observation, err := b.Reader.Recheck(ctx, principal, claims.Binding.Scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if options.MaxOutputBytes > observation.Binding.Policy.MaxOutputBytes {
		options.MaxOutputBytes = observation.Binding.Policy.MaxOutputBytes
	}
	validation, err := handle.Validate(ctx)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if err := handle.Recheck(ctx); err != nil {
		return nil, coreStoreError(err)
	}
	if symbol, ok := handle.(*app.RetainedSymbolPack); ok {
		app.RememberSymbolPackDelivery(ctx, symbol)
	}
	response := baseResponse(options, observation.Vector)
	response.Freshness, response.Data = validation.Freshness, validation.Data
	return encodeResponse(response)
}
