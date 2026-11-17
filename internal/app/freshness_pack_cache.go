package app

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
)

// RetainedPack is sealed to this package. All pack kinds share one quota and
// verify their own immutable result and current authority before returning.
type RetainedPack interface {
	retainedPack()
	retainedID() string
	retainedToken() string
	retainedPrincipal() string
	retainedCheck(context.Context, string, contextpack.CursorGuard) error
	retainedBytes(int64) (int64, error)
}

func (*FreshnessPack) retainedPack() {}
func (p *FreshnessPack) retainedID() string {
	if p == nil || p.pack == nil {
		return ""
	}
	return p.pack.ID()
}
func (p *FreshnessPack) retainedToken() string     { return p.token }
func (p *FreshnessPack) retainedPrincipal() string { return p.principal }
func (p *FreshnessPack) retainedCheck(ctx context.Context, principal string, guard contextpack.CursorGuard) error {
	return p.recheck(ctx, p.token, "", principal, guard)
}
func (p *FreshnessPack) retainedBytes(limit int64) (int64, error) {
	return p.pack.MaterializedBytes(limit)
}

func validRetainedPack(pack RetainedPack) bool {
	if pack == nil {
		return false
	}
	value := reflect.ValueOf(pack)
	return value.Kind() == reflect.Pointer && !value.IsNil()
}

type freshnessPackEntry struct {
	pack    RetainedPack
	weight  int64
	expires int64
}

// FreshnessPackCache retains the mode-aware wrapper needed by continuation
// and validation. Serialized weights are admission estimates, not hard heap
// measurements; caller-owned closure data must be conservatively charged.
type FreshnessPackCache struct {
	mu             sync.Mutex
	entries        map[string]freshnessPackEntry
	codec          *contextpack.CursorCodec
	maxEntries     int
	maxBytes, used int64
	ttlSeconds     int64
	now            func() time.Time
}

func NewFreshnessPackCache(codec *contextpack.CursorCodec, maxEntries int, maxBytes, ttlSeconds int64) (*FreshnessPackCache, error) {
	if codec == nil || maxEntries < 1 || maxEntries > 32 || maxBytes < 1 || maxBytes > 32*1024*1024 || ttlSeconds < 1 || ttlSeconds > 900 {
		return nil, ErrValidationInput
	}
	return &FreshnessPackCache{entries: make(map[string]freshnessPackEntry), codec: codec, maxEntries: maxEntries, maxBytes: maxBytes, ttlSeconds: ttlSeconds, now: time.Now}, nil
}

// WithinLimits verifies that a cache supplied by a process owner uses the
// same codec and does not exceed the immutable effective configuration.
func (c *FreshnessPackCache) WithinLimits(codec *contextpack.CursorCodec, maxEntries int64, maxBytes, ttlSeconds int64) bool {
	return c != nil && c.codec == codec && int64(c.maxEntries) <= maxEntries && c.maxBytes <= maxBytes && c.ttlSeconds <= ttlSeconds
}

func (c *FreshnessPackCache) expireLocked(now int64) {
	for id, entry := range c.entries {
		if now >= entry.expires {
			delete(c.entries, id)
			c.used -= entry.weight
		}
	}
}

// Admit checks the signed token before retaining the wrapper. extraBytes must
// include data captured by verifier/authorization closures; callers must also
// bound concurrent materialization and release all non-cache references.
func (c *FreshnessPackCache) Admit(ctx context.Context, pack *FreshnessPack, token, principal string, guard contextpack.CursorGuard, extraBytes int64) error {
	return c.AdmitRetained(ctx, pack, token, principal, guard, extraBytes)
}

// AdmitRetained charges all retained pack types to this same process budget.
func (c *FreshnessPackCache) AdmitRetained(ctx context.Context, pack RetainedPack, token, principal string, guard contextpack.CursorGuard, extraBytes int64) error {
	if c == nil || c.codec == nil || c.now == nil || !validRetainedPack(pack) || pack.retainedID() == "" || guard == nil || token == "" || token != pack.retainedToken() || extraBytes < 0 || extraBytes > c.maxBytes {
		return ErrValidationInput
	}
	claims, err := c.codec.VerifyPrincipal(ctx, token, "pack", principal, guard)
	if err != nil {
		return err
	}
	if claims.Binding.ResultID != pack.retainedID() || pack.retainedPrincipal() != principal {
		return ErrValidationAuthority
	}
	if err := pack.retainedCheck(ctx, principal, guard); err != nil {
		return err
	}
	weight, err := pack.retainedBytes(c.maxBytes)
	if err != nil {
		return err
	}
	if weight < 0 || weight > c.maxBytes || extraBytes > c.maxBytes-weight {
		return contextpack.ErrLimit
	}
	weight += extraBytes
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked(c.now().Unix())
	expires := claims.ExpiresAt
	if claims.IssuedAt+c.ttlSeconds < expires {
		expires = claims.IssuedAt + c.ttlSeconds
	}
	if c.now().Unix() >= expires {
		return contextpack.ErrPackExpired
	}
	if _, exists := c.entries[pack.retainedID()]; exists {
		return ErrValidationInput
	}
	if len(c.entries) >= c.maxEntries || weight > c.maxBytes-c.used {
		return contextpack.ErrLimit
	}
	c.entries[pack.retainedID()] = freshnessPackEntry{pack: pack, weight: weight, expires: expires}
	c.used += weight
	return nil
}

// Get authenticates the token before looking up its ID. A missing or expired
// entry never rebuilds from the active generation. Page methods repeat checks.
func (c *FreshnessPackCache) Get(ctx context.Context, token, principal string, guard contextpack.CursorGuard) (*FreshnessPack, error) {
	pack, err := c.GetRetained(ctx, token, principal, guard)
	if err != nil {
		return nil, err
	}
	result, ok := pack.(*FreshnessPack)
	if !ok {
		return nil, ErrValidationInput
	}
	return result, nil
}

// GetRetained authenticates, checks authority, then rechecks cache expiry.
func (c *FreshnessPackCache) GetRetained(ctx context.Context, token, principal string, guard contextpack.CursorGuard) (RetainedPack, error) {
	if c == nil || c.codec == nil || c.now == nil || guard == nil {
		return nil, ErrValidationInput
	}
	claims, err := c.codec.VerifyPrincipal(ctx, token, "pack", principal, guard)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.expireLocked(c.now().Unix())
	entry, ok := c.entries[claims.Binding.ResultID]
	c.mu.Unlock()
	if !ok {
		return nil, contextpack.ErrPackExpired
	}
	if !validRetainedPack(entry.pack) || entry.pack.retainedToken() != token || entry.pack.retainedPrincipal() != principal {
		return nil, ErrValidationAuthority
	}
	if err := entry.pack.retainedCheck(ctx, principal, guard); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.expireLocked(c.now().Unix())
	current, exists := c.entries[claims.Binding.ResultID]
	c.mu.Unlock()
	if !exists || current.pack != entry.pack {
		return nil, contextpack.ErrPackExpired
	}
	return entry.pack, nil
}

// Discard removes only the identical in-process object after an unpublished
// first-page failure; it cannot remove another pack by a caller-supplied ID.
func (c *FreshnessPackCache) Discard(pack *FreshnessPack) {
	c.DiscardRetained(pack)
}

// DiscardRetained removes only the identical retained pointer.
func (c *FreshnessPackCache) DiscardRetained(pack RetainedPack) {
	if c == nil || !validRetainedPack(pack) || pack.retainedID() == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	id := pack.retainedID()
	if entry, ok := c.entries[id]; ok && entry.pack == pack {
		delete(c.entries, id)
		c.used -= entry.weight
	}
}
