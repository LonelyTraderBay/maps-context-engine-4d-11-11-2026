package contextpack

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var ErrPackExpired = errors.New("materialized pack expired or unavailable")

type packCacheEntry struct {
	pack    *PagedPack
	bytes   int64
	expires int64
}

// PackCache bounds the number and estimated serialized materialization bytes
// retained by this cache. This is not an exact Go heap limit; the application
// must also bound concurrent readers and release other references to packs.
type PackCache struct {
	mu         sync.Mutex
	entries    map[string]packCacheEntry
	maxEntries int
	maxBytes   int64
	used       int64
	now        func() time.Time
}

func NewPackCache(maxEntries int, maxBytes int64) (*PackCache, error) {
	if maxEntries < 1 || maxEntries > 1024 || maxBytes < 1 || maxBytes > 256*1024*1024 {
		return nil, ErrInput
	}
	return &PackCache{entries: map[string]packCacheEntry{}, maxEntries: maxEntries, maxBytes: maxBytes, now: time.Now}, nil
}

// ID identifies the process-local materialization, not the source contents.
func (p *PagedPack) ID() string {
	if p == nil {
		return ""
	}
	return p.binding.ResultID
}

func packMaterializedBytes(p *PagedPack, maxBytes int64) (int64, error) {
	if p == nil || p.slice == nil || p.slice.selection == nil || p.slice.selection.snapshot == nil {
		return 0, ErrInput
	}
	var used int64
	manifest := p.slice.selection.snapshot.Manifest()
	manifestJSON, err := json.Marshal(manifest)
	if err != nil || int64(len(manifestJSON)) > maxBytes {
		return 0, ErrLimit
	}
	used += int64(len(manifestJSON))
	for _, entry := range manifest.Files {
		file, present := p.slice.selection.snapshot.File(entry.Path)
		if !present {
			continue
		}
		if int64(file.Len()) > maxBytes-used {
			return 0, ErrLimit
		}
		used += int64(file.Len())
	}
	for _, value := range []any{p.report, p.slice.selection.report, p.items} {
		raw, err := json.Marshal(value)
		if err != nil {
			return 0, ErrInput
		}
		if int64(len(raw)) > maxBytes-used {
			return 0, ErrLimit
		}
		used += int64(len(raw))
	}
	return used, nil
}

// MaterializedBytes estimates serialized data retained by this pack. It is not
// an exact Go heap measurement and excludes values retained by caller-owned
// authorization or fact-verifier closures, which the caller must charge too.
func (p *PagedPack) MaterializedBytes(maxBytes int64) (int64, error) {
	if maxBytes < 1 || maxBytes > 32*1024*1024 {
		return 0, ErrInput
	}
	return packMaterializedBytes(p, maxBytes)
}

func (c *PackCache) expireLocked(now int64) {
	for id, entry := range c.entries {
		if now >= entry.expires {
			delete(c.entries, id)
			c.used -= entry.bytes
		}
	}
}

// Admit verifies the original pack token and refuses over-cap admission. It
// expires only entries this cache owns; it does not touch files or databases.
func (c *PackCache) Admit(ctx context.Context, p *PagedPack) error {
	if c == nil || c.now == nil || p == nil || p.codec == nil || !cursorUUID(p.ID()) {
		return ErrInput
	}
	claims, err := p.codec.Verify(ctx, p.token, p.binding, p.authorize)
	if err != nil {
		return err
	}
	weight, err := packMaterializedBytes(p, c.maxBytes)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked(c.now().Unix())
	if c.now().Unix() >= claims.ExpiresAt {
		return ErrPackExpired
	}
	if _, exists := c.entries[p.ID()]; exists {
		return ErrInput
	}
	if len(c.entries) >= c.maxEntries || weight > c.maxBytes-c.used {
		return ErrLimit
	}
	c.entries[p.ID()] = packCacheEntry{pack: p, bytes: weight, expires: claims.ExpiresAt}
	c.used += weight
	return nil
}

// Continue verifies the current caller and token on every use. Cache expiry or
// eviction never resolves the same cursor against a newer pack or snapshot.
func (c *PackCache) Continue(ctx context.Context, id, packToken, cursor, principalDigest string, currentGuard CursorGuard, options PackPageOptions) (PackPage, error) {
	if c == nil || c.now == nil || !cursorUUID(id) {
		return PackPage{}, ErrInput
	}
	c.mu.Lock()
	c.expireLocked(c.now().Unix())
	entry, ok := c.entries[id]
	c.mu.Unlock()
	if !ok {
		return PackPage{}, ErrPackExpired
	}
	return entry.pack.Continue(ctx, packToken, cursor, principalDigest, currentGuard, options)
}
