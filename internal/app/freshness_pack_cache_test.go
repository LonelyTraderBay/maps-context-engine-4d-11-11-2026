package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
)

type alternateRetainedFixture struct {
	id, token, principal string
	codec                *contextpack.CursorCodec
	weight               int64
}

func (*alternateRetainedFixture) retainedPack()                        {}
func (p *alternateRetainedFixture) retainedID() string                 { return p.id }
func (p *alternateRetainedFixture) retainedToken() string              { return p.token }
func (p *alternateRetainedFixture) retainedPrincipal() string          { return p.principal }
func (p *alternateRetainedFixture) retainedBytes(int64) (int64, error) { return p.weight, nil }
func (p *alternateRetainedFixture) retainedCheck(ctx context.Context, principal string, guard contextpack.CursorGuard) error {
	claims, err := p.codec.VerifyPrincipal(ctx, p.token, "pack", principal, guard)
	if err != nil {
		return err
	}
	if claims.Binding.ResultID != p.id {
		return ErrValidationAuthority
	}
	return nil
}

func TestFreshnessPackCacheSharesQuotaAcrossRetainedKinds(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	binding := f.input.Binding
	binding.ResultID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	otherToken, err := f.input.Codec.Issue(f.ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	other := &alternateRetainedFixture{id: binding.ResultID, token: otherToken, principal: binding.PrincipalDigest, codec: f.input.Codec, weight: 501}
	weight, err := pack.retainedBytes(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFreshnessPackCache(f.input.Codec, 2, weight+500, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Admit(f.ctx, pack, token, binding.PrincipalDigest, f.input.Guard, 0); err != nil {
		t.Fatal(err)
	}
	if err := cache.AdmitRetained(f.ctx, other, otherToken, binding.PrincipalDigest, f.input.Guard, 0); !errors.Is(err, contextpack.ErrLimit) {
		t.Fatalf("different pack kind bypassed byte quota: %v", err)
	}
	cache.Discard(pack)
	if err := cache.AdmitRetained(f.ctx, other, otherToken, binding.PrincipalDigest, f.input.Guard, 0); err != nil {
		t.Fatal(err)
	}
	if got, err := cache.GetRetained(f.ctx, otherToken, binding.PrincipalDigest, f.input.Guard); err != nil || got != other {
		t.Fatalf("retained lookup failed: %v", err)
	}
	if got, err := cache.Get(f.ctx, otherToken, binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrValidationInput) || got != nil {
		t.Fatalf("different kind accepted as symbol pack: %v", err)
	}
	if got, err := cache.GetRetained(f.ctx, otherToken, strings.Repeat("f", 64), f.input.Guard); !errors.Is(err, contextpack.ErrCursorInvalid) || got != nil {
		t.Fatalf("different principal accessed retained result: %v", err)
	}
	cache.now = func() time.Time { return time.Now().Add(31 * time.Second) }
	if got, err := cache.GetRetained(f.ctx, otherToken, binding.PrincipalDigest, f.input.Guard); !errors.Is(err, contextpack.ErrPackExpired) || got != nil || cache.used != 0 {
		t.Fatalf("expired retained result survived: %v", err)
	}
}

func TestFreshnessPackCacheAuthenticatesAndEnforcesQuota(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFreshnessPackCache(f.input.Codec, 1, 4*1024*1024, 30)
	if err != nil {
		t.Fatal(err)
	}
	principal := f.input.Binding.PrincipalDigest
	if err := cache.Admit(f.ctx, pack, token, principal, f.input.Guard, 1024); err != nil {
		t.Fatal(err)
	}
	if got, err := cache.Get(f.ctx, token, principal, f.input.Guard); err != nil || got != pack {
		t.Fatalf("cache lookup: %v", err)
	}
	if got, err := cache.Get(f.ctx, token, strings.Repeat("f", 64), f.input.Guard); !errors.Is(err, contextpack.ErrCursorInvalid) || got != nil {
		t.Fatalf("cross-principal lookup: %v", err)
	}
	revoked := errors.New("revoked")
	if got, err := cache.Get(f.ctx, token, principal, func(context.Context, contextpack.CursorBinding) error { return revoked }); !errors.Is(err, revoked) || got != nil {
		t.Fatalf("revoked lookup: %v", err)
	}
	other, otherToken, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Admit(f.ctx, other, otherToken, principal, f.input.Guard, 0); !errors.Is(err, contextpack.ErrLimit) {
		t.Fatalf("entry quota ignored: %v", err)
	}
	if got, err := cache.Get(f.ctx, otherToken, principal, f.input.Guard); !errors.Is(err, contextpack.ErrPackExpired) || got != nil {
		t.Fatalf("missing result silently rebuilt: %v", err)
	}
	cache.now = func() time.Time { return time.Now().Add(31 * time.Second) }
	if got, err := cache.Get(f.ctx, token, principal, f.input.Guard); !errors.Is(err, contextpack.ErrPackExpired) || got != nil {
		t.Fatalf("expired cache retained result: %v", err)
	}
	if cache.used != 0 {
		t.Fatal("expired weight not released")
	}
}

func TestFreshnessPackCacheChargesClosureData(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFreshnessPackCache(f.input.Codec, 2, 4096, 900)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Admit(f.ctx, pack, token, f.input.Binding.PrincipalDigest, f.input.Guard, 4096); !errors.Is(err, contextpack.ErrLimit) {
		t.Fatalf("closure data quota ignored: %v", err)
	}
}

func TestFreshnessPackCacheRechecksExpiryAfterAuthority(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFreshnessPackCache(f.input.Codec, 2, 4*1024*1024, 30)
	if err != nil {
		t.Fatal(err)
	}
	principal := f.input.Binding.PrincipalDigest
	if err := cache.Admit(f.ctx, pack, token, principal, f.input.Guard, 0); err != nil {
		t.Fatal(err)
	}
	calls := 0
	guard := func(context.Context, contextpack.CursorBinding) error {
		calls++
		if calls == 2 {
			cache.now = func() time.Time { return time.Now().Add(31 * time.Second) }
		}
		return nil
	}
	if got, err := cache.Get(f.ctx, token, principal, guard); !errors.Is(err, contextpack.ErrPackExpired) || got != nil || calls < 2 {
		t.Fatalf("expiry during authority check returned a pack: %v", err)
	}
}
