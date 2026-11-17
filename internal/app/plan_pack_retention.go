package app

import (
	"context"
	"crypto/sha256"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// PlanRestoreFactory reopens only the pinned generation and provides current
// principal, fact, path, declaration and workspace authority callbacks. Its
// request is authenticated against the actor and original token before use.
type PlanRestoreFactory func(context.Context, contextpack.CursorBinding, model.PackRequest, string) (FreshnessPlanRestoreInput, error)

type PlanPackRetention struct {
	store   SymbolPackStorage
	codec   *contextpack.CursorCodec
	max     int
	ttl     int64
	restore PlanRestoreFactory
}

// NewPlanPackRetention does not open state or enable a public CLI route.
func NewPlanPackRetention(store SymbolPackStorage, codec *contextpack.CursorCodec, maxBytes int, ttlSeconds int64, restore PlanRestoreFactory) (*PlanPackRetention, error) {
	if store == nil || store.WorktreeID() == "" || codec == nil || maxBytes < 1024 || maxBytes > maxPackEnvelope || int64(maxBytes) > store.MaxResultBytes() || ttlSeconds < 1 || ttlSeconds > 900 || restore == nil {
		return nil, ErrValidationInput
	}
	return &PlanPackRetention{store: store, codec: codec, max: maxBytes, ttl: ttlSeconds, restore: restore}, nil
}

func (s *PlanPackRetention) claims(ctx context.Context, token, principal string, guard contextpack.CursorGuard) (contextpack.SignedCursor, error) {
	if s == nil || s.store == nil || s.codec == nil || guard == nil {
		return contextpack.SignedCursor{}, ErrValidationInput
	}
	claims, err := s.codec.VerifyPrincipal(ctx, token, "pack", principal, sanitizedPackGuard(guard))
	if err != nil {
		return contextpack.SignedCursor{}, packContextError(ctx, err)
	}
	scope := claims.Binding.Scope
	if scope.Kind != "single" || len(scope.Members) != 1 || len(scope.UnavailableMembers) != 0 || scope.Members[0].WorktreeID != s.store.WorktreeID() || claims.Binding.ResultID == "" {
		return contextpack.SignedCursor{}, ErrValidationAuthority
	}
	return claims, nil
}

func (s *PlanPackRetention) restorePlan(ctx context.Context, raw []byte, token, principal, actor string, guard contextpack.CursorGuard) (*FreshnessPlan, error) {
	claims, err := s.claims(ctx, token, principal, guard)
	if err != nil {
		return nil, err
	}
	binding, err := copyRetentionBinding(claims.Binding)
	if err != nil {
		return nil, err
	}
	request, err := authenticatedRetainedPlanRequest(ctx, raw, s.codec, token, principal, actor, guard, s.max)
	if err != nil {
		return nil, err
	}
	in, err := s.restore(ctx, binding, request.request, request.revisionDigest)
	if err != nil {
		return nil, packContextError(ctx, ErrValidationAuthority)
	}
	ownerGuard := in.Plan.Guard
	if ownerGuard == nil {
		return nil, ErrValidationAuthority
	}
	// Restore keeps the independent owner's guard. The caller's current
	// guard is checked by claims/request authentication above and by recheck
	// below before this private materialization can leave the retention owner.
	// Nesting it inside every token/plan callback multiplies identical store
	// observations without adding another output boundary.
	in.Plan.Codec, in.Plan.PackToken, in.Plan.PrincipalDigest = s.codec, token, principal
	in.Plan.Guard, in.Plan.MaxArchiveBytes = ownerGuard, s.max
	in.PrincipalID = actor
	plan, err := RestoreFreshnessPlanEnvelope(ctx, raw, in)
	if err != nil {
		return nil, err
	}
	if err := plan.recheck(ctx, "", principal, guard); err != nil {
		return nil, packContextError(ctx, err)
	}
	return plan, nil
}

// Save persists before returning a usable handle. Errors, including an
// uncertain publication, never return a token to the caller.
func (s *PlanPackRetention) Save(ctx context.Context, plan *FreshnessPlan, principal, actor string, requestJSON []byte, guard contextpack.CursorGuard) (*RetainedPlanPack, error) {
	if s == nil || plan == nil || guard == nil || actor == "" {
		return nil, ErrValidationInput
	}
	envelope, err := plan.ArchiveEnvelope(ctx, actor, requestJSON, s.ttl, s.max)
	if err != nil {
		return nil, err
	}
	if envelope.PrincipalDigest != principal {
		return nil, ErrValidationAuthority
	}
	claims, err := s.claims(ctx, envelope.Token, principal, guard)
	if err != nil {
		return nil, err
	}
	expected := sha256.Sum256(envelope.Bytes)
	var verified *FreshnessPlan
	verify := func(ctx context.Context, raw []byte) error {
		if sha256.Sum256(raw) != expected {
			return ErrValidationAuthority
		}
		if verified == nil {
			var err error
			verified, err = s.restorePlan(ctx, raw, envelope.Token, principal, actor, guard)
			if err != nil {
				return err
			}
		}
		return verified.retainedAuthority(ctx, principal, guard)
	}
	if err := s.store.Create(ctx, claims.Binding.ResultID, envelope.Bytes, verify); err != nil {
		return nil, err
	}
	if verified == nil {
		return nil, ErrValidationAuthority
	}
	handle := &RetainedPlanPack{owner: s, plan: verified, token: envelope.Token, principal: principal, guard: guard, resultID: claims.Binding.ResultID, contentHash: expected}
	// Create already synced, read back and verified these exact installed
	// bytes. Recheck complete authority after its last callback; First will
	// independently verify current storage again before releasing a page.
	if err := verified.recheck(ctx, "", principal, guard); err != nil {
		return nil, packContextError(ctx, err)
	}
	return handle, nil
}

// Get authenticates and restores an existing result; missing or expired
// storage is never replaced by rebuilding an active generation.
func (s *PlanPackRetention) Get(ctx context.Context, token, principal, actor string, guard contextpack.CursorGuard) (*RetainedPlanPack, error) {
	if actor == "" || len(actor) > 4096 {
		return nil, ErrValidationInput
	}
	claims, err := s.claims(ctx, token, principal, guard)
	if err != nil {
		return nil, err
	}
	var restored *FreshnessPlan
	var restoredHash [32]byte
	raw, err := s.store.Read(ctx, claims.Binding.ResultID, func(ctx context.Context, raw []byte) error {
		hash := sha256.Sum256(raw)
		if restored == nil {
			var err error
			restored, err = s.restorePlan(ctx, raw, token, principal, actor, guard)
			if err != nil {
				return err
			}
			restoredHash = hash
		} else if hash != restoredHash {
			return ErrValidationAuthority
		}
		return restored.retainedAuthority(ctx, principal, guard)
	})
	if err != nil {
		return nil, err
	}
	if restored == nil || sha256.Sum256(raw) != restoredHash {
		return nil, ErrValidationAuthority
	}
	handle := &RetainedPlanPack{owner: s, plan: restored, token: token, principal: principal, guard: guard, resultID: claims.Binding.ResultID, contentHash: restoredHash}
	// Read already verified the retained bytes and path. Recheck complete
	// authority after its callbacks without opening the same file a second time.
	if err := restored.recheck(ctx, "", principal, guard); err != nil {
		return nil, packContextError(ctx, err)
	}
	return handle, nil
}

// ProveExpiredRetainedEnvelope dispatches across the two domain-separated
// archive formats so GC can safely handle mixed symbol and plan results.
func ProveExpiredRetainedEnvelope(ctx context.Context, id string, raw []byte, codec *contextpack.CursorCodec, maxBytes int) (bool, error) {
	expired, err := ProveExpiredPackEnvelope(ctx, id, raw, codec, maxBytes)
	if err == nil {
		return expired, nil
	}
	if ctx != nil && ctx.Err() != nil {
		return false, ctx.Err()
	}
	return ProveExpiredPlanEnvelope(ctx, id, raw, codec, maxBytes)
}

// GCExpired needs independently authorized writer and worktree budget leases.
func (s *PlanPackRetention) GCExpired(ctx context.Context, writer ports.IndexWriterLease, job string) (int, error) {
	if s == nil || s.store == nil || s.codec == nil {
		return 0, ErrValidationInput
	}
	return s.store.GCExpired(ctx, writer, job, func(ctx context.Context, id string, raw []byte) (bool, error) {
		return ProveExpiredRetainedEnvelope(ctx, id, raw, s.codec, maxPackEnvelope)
	})
}

type RetainedPlanPack struct {
	owner       *PlanPackRetention
	plan        *FreshnessPlan
	token       string
	principal   string
	guard       contextpack.CursorGuard
	resultID    string
	contentHash [32]byte
}

func (p *RetainedPlanPack) Token() string {
	if p == nil {
		return ""
	}
	return p.token
}

// Recheck verifies the stored bytes, token lifetime and current declaration
// authority immediately before an adapter releases a result.
func (p *RetainedPlanPack) Recheck(ctx context.Context) error {
	return p.check(ctx)
}

func (p *RetainedPlanPack) check(ctx context.Context) error {
	if p == nil || p.owner == nil || p.plan == nil || p.guard == nil {
		return ErrValidationInput
	}
	claims, err := p.owner.claims(ctx, p.token, p.principal, p.guard)
	if err != nil {
		return err
	}
	if claims.Binding.ResultID != p.resultID {
		return ErrValidationAuthority
	}
	verified := false
	raw, err := p.owner.store.Read(ctx, p.resultID, func(ctx context.Context, raw []byte) error {
		if sha256.Sum256(raw) != p.contentHash {
			return ErrValidationAuthority
		}
		// claims above authorize this private read; the complete independent
		// plan, caller, source and expiry check follows Read before release.
		// This callback proves byte identity only, including both read passes.
		verified = true
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	if !verified || sha256.Sum256(raw) != p.contentHash {
		return ErrValidationAuthority
	}
	return p.plan.recheck(ctx, "", p.principal, p.guard)
}

func (p *RetainedPlanPack) First(ctx context.Context, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p == nil || p.plan == nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	page, err := p.plan.First(ctx, options)
	if checkErr := p.check(ctx); checkErr != nil {
		return contextpack.PackPage{}, checkErr
	}
	return page, err
}

func (p *RetainedPlanPack) Continue(ctx context.Context, cursor string, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p == nil || p.plan == nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	// As with First, prepare a private page and then verify stored bytes and
	// complete authority even on a paging error, before returning anything.
	page, err := p.plan.Continue(ctx, p.token, cursor, p.principal, p.guard, options)
	if checkErr := p.check(ctx); checkErr != nil {
		return contextpack.PackPage{}, checkErr
	}
	return page, err
}

func (p *RetainedPlanPack) Validate(ctx context.Context) (CapturedValidation, error) {
	if p == nil || p.plan == nil {
		return CapturedValidation{}, ErrValidationInput
	}
	result, err := p.plan.Validate(ctx, p.token, p.principal, p.guard)
	if checkErr := p.check(ctx); checkErr != nil {
		return CapturedValidation{}, checkErr
	}
	return result, err
}
