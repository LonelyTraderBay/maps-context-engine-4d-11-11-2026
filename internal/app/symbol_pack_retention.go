package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// SymbolPackStorage is an owner-supplied bounded storage port. Its callbacks
// authenticate bytes; the implementation owns secure files, quota and leases.
type SymbolPackStorage interface {
	WorktreeID() string
	MaxResultBytes() int64
	Create(context.Context, string, []byte, func(context.Context, []byte) error) error
	Read(context.Context, string, func(context.Context, []byte) error) ([]byte, error)
	GCExpired(context.Context, ports.IndexWriterLease, string, func(context.Context, string, []byte) (bool, error)) (int, error)
}

// SymbolRestoreFactory must reopen only the exact pinned generation and supply
// new principal, source, fact, record and live-workspace authority callbacks.
// It must not reconstruct a result or resolve an active generation.
type SymbolRestoreFactory func(context.Context, contextpack.CursorBinding) (FreshnessPackRestoreInput, error)

type SymbolPackRetention struct {
	store   SymbolPackStorage
	codec   *contextpack.CursorCodec
	max     int
	ttl     int64
	restore SymbolRestoreFactory
}

// NewSymbolPackRetention does not open state or enable a public CLI route.
func NewSymbolPackRetention(store SymbolPackStorage, codec *contextpack.CursorCodec, maxBytes int, ttlSeconds int64, restore SymbolRestoreFactory) (*SymbolPackRetention, error) {
	if store == nil || store.WorktreeID() == "" || codec == nil || maxBytes < 1024 || maxBytes > maxPackEnvelope || int64(maxBytes) > store.MaxResultBytes() || ttlSeconds < 1 || ttlSeconds > 900 || restore == nil {
		return nil, ErrValidationInput
	}
	return &SymbolPackRetention{store: store, codec: codec, max: maxBytes, ttl: ttlSeconds, restore: restore}, nil
}

func (s *SymbolPackRetention) claims(ctx context.Context, token, principal string, guard contextpack.CursorGuard) (contextpack.SignedCursor, error) {
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

func copyRetentionBinding(binding contextpack.CursorBinding) (contextpack.CursorBinding, error) {
	raw, err := json.Marshal(binding)
	if err != nil || len(raw) > 32768 {
		return contextpack.CursorBinding{}, ErrValidationInput
	}
	var result contextpack.CursorBinding
	if json.Unmarshal(raw, &result) != nil {
		return contextpack.CursorBinding{}, ErrValidationInput
	}
	return result, nil
}

func (s *SymbolPackRetention) restorePack(ctx context.Context, raw []byte, token, principal, actor string, guard contextpack.CursorGuard) (*FreshnessPack, error) {
	claims, err := s.claims(ctx, token, principal, guard)
	if err != nil {
		return nil, err
	}
	binding, err := copyRetentionBinding(claims.Binding)
	if err != nil {
		return nil, err
	}
	in, err := s.restore(ctx, binding)
	if err != nil {
		return nil, packContextError(ctx, ErrValidationAuthority)
	}
	in.Pack.Codec, in.Pack.Token, in.Pack.PrincipalDigest = s.codec, token, principal
	in.Pack.Guard, in.Pack.MaxArchiveBytes = guard, s.max
	in.PrincipalID = actor
	return RestoreFreshnessEnvelope(ctx, raw, in, s.max)
}

// Save publishes before returning a usable handle or token. No token is
// returned on a failed, revoked or uncertain storage outcome.
func (s *SymbolPackRetention) Save(ctx context.Context, pack *FreshnessPack, principal, actor string, requestJSON []byte, guard contextpack.CursorGuard) (*RetainedSymbolPack, error) {
	if s == nil || pack == nil || guard == nil || actor == "" {
		return nil, ErrValidationInput
	}
	envelope, err := pack.ArchiveEnvelope(ctx, actor, requestJSON, s.ttl, s.max)
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
	expectedHash := sha256.Sum256(envelope.Bytes)
	var verified *FreshnessPack
	verify := func(ctx context.Context, raw []byte) error {
		if sha256.Sum256(raw) != expectedHash {
			return ErrValidationAuthority
		}
		if verified == nil {
			var err error
			verified, err = s.restorePack(ctx, raw, envelope.Token, principal, actor, guard)
			if err != nil {
				return err
			}
		}
		return verified.retainedAuthority(ctx, envelope.Token, principal)
	}
	if err := s.store.Create(ctx, claims.Binding.ResultID, envelope.Bytes, verify); err != nil {
		return nil, err
	}
	if verified == nil {
		return nil, ErrValidationAuthority
	}
	// The independently restored pack is already bound to these exact bytes.
	// Read back the published file without loading the same generation again.
	handle := &RetainedSymbolPack{owner: s, pack: verified, token: envelope.Token, principal: principal, guard: guard, resultID: claims.Binding.ResultID, contentHash: expectedHash}
	if err := handle.check(ctx); err != nil {
		return nil, err
	}
	return handle, nil
}

// Get never rebuilds a missing result. It restores and rechecks the original
// authenticated materialization using the current owner's authority.
func (s *SymbolPackRetention) Get(ctx context.Context, token, principal, actor string, guard contextpack.CursorGuard) (*RetainedSymbolPack, error) {
	if actor == "" || len(actor) > 4096 {
		return nil, ErrValidationInput
	}
	claims, err := s.claims(ctx, token, principal, guard)
	if err != nil {
		return nil, err
	}
	var restored *FreshnessPack
	var restoredHash [32]byte
	raw, err := s.store.Read(ctx, claims.Binding.ResultID, func(ctx context.Context, raw []byte) error {
		hash := sha256.Sum256(raw)
		if restored == nil {
			var err error
			restored, err = s.restorePack(ctx, raw, token, principal, actor, guard)
			if err != nil {
				return err
			}
			restoredHash = hash
		} else if hash != restoredHash {
			return ErrValidationAuthority
		}
		return restored.retainedAuthority(ctx, token, principal)
	})
	if err != nil {
		return nil, err
	}
	if restored == nil || sha256.Sum256(raw) != restoredHash {
		return nil, ErrValidationAuthority
	}
	handle := &RetainedSymbolPack{owner: s, pack: restored, token: token, principal: principal, guard: guard,
		resultID: claims.Binding.ResultID, contentHash: restoredHash}
	if err := handle.check(ctx); err != nil {
		return nil, err
	}
	return handle, nil
}

// GCExpired only supplies signed expiry proof; the storage owner must require
// explicit GC authorization and the writer and budget leases for its root.
func (s *SymbolPackRetention) GCExpired(ctx context.Context, writer ports.IndexWriterLease, job string) (int, error) {
	if s == nil || s.store == nil || s.codec == nil {
		return 0, ErrValidationInput
	}
	return s.store.GCExpired(ctx, writer, job, func(ctx context.Context, id string, raw []byte) (bool, error) {
		return ProveExpiredRetainedEnvelope(ctx, id, raw, s.codec, maxPackEnvelope)
	})
}

// RetainedSymbolPack checks stored bytes, TTL and authority before releasing
// any page or validation. The owner keeps the underlying store leases alive.
type RetainedSymbolPack struct {
	owner       *SymbolPackRetention
	pack        *FreshnessPack
	token       string
	principal   string
	guard       contextpack.CursorGuard
	resultID    string
	contentHash [32]byte
}

type symbolPackDeliveryKey struct{}

// CoreService creates a fresh slot per CLI call. The backend submits and
// verifies the handle synchronously within that call.
type symbolPackDelivery struct {
	pack *RetainedSymbolPack
}

// RememberSymbolPackDelivery records the handle prepared for this CLI response.
func RememberSymbolPackDelivery(ctx context.Context, pack *RetainedSymbolPack) {
	if delivery, ok := ctx.Value(symbolPackDeliveryKey{}).(*symbolPackDelivery); ok {
		delivery.pack = pack
	}
}

// SymbolPackForDelivery reuses only the same request's restored handle. The
// caller must still recheck stored bytes, expiry and authority before delivery.
func SymbolPackForDelivery(ctx context.Context, token, principal string) *RetainedSymbolPack {
	delivery, _ := ctx.Value(symbolPackDeliveryKey{}).(*symbolPackDelivery)
	if delivery == nil || delivery.pack == nil || delivery.pack.token != token || delivery.pack.principal != principal {
		return nil
	}
	return delivery.pack
}

func (p *RetainedSymbolPack) Token() string {
	if p == nil {
		return ""
	}
	return p.token
}

// Recheck verifies the stored bytes, token lifetime and current authority.
// It is suitable for an adapter's final check immediately before delivery.
func (p *RetainedSymbolPack) Recheck(ctx context.Context) error {
	return p.check(ctx)
}

func (p *RetainedSymbolPack) check(ctx context.Context) error {
	if err := p.checkStorage(ctx); err != nil {
		return err
	}
	return p.pack.recheck(ctx, p.token, "", p.principal, nil)
}

func (p *RetainedSymbolPack) checkStorage(ctx context.Context) error {
	if p == nil || p.owner == nil || p.pack == nil || p.guard == nil {
		return ErrValidationInput
	}
	claims, err := p.owner.claims(ctx, p.token, p.principal, p.guard)
	if err != nil || claims.Binding.ResultID != p.resultID {
		if err != nil {
			return err
		}
		return ErrValidationAuthority
	}
	verified := false
	raw, err := p.owner.store.Read(ctx, p.resultID, func(ctx context.Context, raw []byte) error {
		if sha256.Sum256(raw) != p.contentHash {
			return ErrValidationAuthority
		}
		verified = true
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	if !verified || sha256.Sum256(raw) != p.contentHash {
		return ErrValidationAuthority
	}
	return ctx.Err()
}

func (p *RetainedSymbolPack) First(ctx context.Context, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p == nil || p.pack == nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	// First rechecks the immutable pack's complete authority. Keep its bytes
	// and even error descriptors private until the storage postcheck succeeds.
	page, err := p.pack.First(ctx, options)
	if checkErr := p.check(ctx); checkErr != nil {
		return contextpack.PackPage{}, checkErr
	}
	return page, err
}

func (p *RetainedSymbolPack) Continue(ctx context.Context, cursor string, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if err := p.check(ctx); err != nil {
		return contextpack.PackPage{}, err
	}
	page, err := p.pack.Continue(ctx, p.token, cursor, p.principal, p.guard, options)
	if err != nil {
		return contextpack.PackPage{}, err
	}
	if err := p.check(ctx); err != nil {
		return contextpack.PackPage{}, err
	}
	return page, nil
}

func (p *RetainedSymbolPack) Validate(ctx context.Context) (CapturedValidation, error) {
	if err := p.checkStorage(ctx); err != nil {
		return CapturedValidation{}, err
	}
	// The validation checks current selection authority before the workspace read.
	result, err := p.pack.validate(ctx, p.token, p.principal, nil)
	if err != nil {
		return CapturedValidation{}, err
	}
	if err := p.check(ctx); err != nil {
		return CapturedValidation{}, err
	}
	return result, nil
}
