package golang

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"go/ast"
	"go/token"
	"sync"
)

var ErrSyntaxCacheConfig = errors.New("invalid syntax cache budget")

// SyntaxCache keeps private, read-only parser trees. EstimatedBytes is a
// conservative accounting weight, not an operating-system RSS guarantee.
// Ownership and lifetime are explicit; no global cache is installed.
type SyntaxCache struct {
	mu                      sync.Mutex
	maxBytes                int64
	maxEntries              int
	weight                  int64
	epoch                   uint64
	entries                 map[syntaxCacheKey]*list.Element
	lru                     list.List
	hits, misses, evictions uint64
}

type syntaxCacheKey struct {
	worktree, profile, policy, provider, toolchain, path, content, options string
}
type syntaxCacheEntry struct {
	key    syntaxCacheKey
	source []byte
	files  *token.FileSet
	parsed *ast.File
	weight int64
}
type SyntaxCacheStats struct {
	Hits           uint64
	Misses         uint64
	Evictions      uint64
	Entries        int
	EstimatedBytes int64
}

func NewSyntaxCache(maxBytes int64, maxEntries int) (*SyntaxCache, error) {
	if maxBytes < 1 || maxBytes > 64*1024*1024 || maxEntries < 1 || maxEntries > 4096 {
		return nil, ErrSyntaxCacheConfig
	}
	return &SyntaxCache{maxBytes: maxBytes, maxEntries: maxEntries, entries: map[syntaxCacheKey]*list.Element{}}, nil
}

func (c *SyntaxCache) Stats() SyntaxCacheStats {
	if c == nil {
		return SyntaxCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return SyntaxCacheStats{Hits: c.hits, Misses: c.misses, Evictions: c.evictions, Entries: len(c.entries), EstimatedBytes: c.weight}
}

// Clear releases all retained entries. An analysis already using an entry may
// finish with its private read-only reference and still performs normal checks.
func (c *SyntaxCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[syntaxCacheKey]*list.Element{}
	c.lru.Init()
	c.weight = 0
	c.epoch++
}

func (c *SyntaxCache) lookup(key syntaxCacheKey, source []byte) (*token.FileSet, *ast.File, bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, nil, false, c.epoch
	}
	entry := element.Value.(*syntaxCacheEntry)
	if !bytes.Equal(entry.source, source) {
		c.misses++
		return nil, nil, false, c.epoch
	}
	c.hits++
	c.lru.MoveToFront(element)
	return entry.files, entry.parsed, true, c.epoch
}

func (c *SyntaxCache) remember(ctx context.Context, key syntaxCacheKey, source []byte, files *token.FileSet, parsed *ast.File, maxNodes int, epoch uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	nodes := 0
	tooLarge := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		nodes++
		if nodes > maxNodes || nodes%64 == 0 && ctx.Err() != nil {
			tooLarge = true
			return false
		}
		return !tooLarge
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if tooLarge {
		return nil
	}
	weight := int64(len(source))*8 + int64(nodes)*512 + 4096
	if weight > c.maxBytes {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.epoch != epoch {
		return nil
	}
	if prior, exists := c.entries[key]; exists {
		if bytes.Equal(prior.Value.(*syntaxCacheEntry).source, source) {
			c.lru.MoveToFront(prior)
		}
		return nil
	}
	for len(c.entries) >= c.maxEntries || weight > c.maxBytes-c.weight {
		old := c.lru.Back()
		if old == nil {
			return nil
		}
		item := old.Value.(*syntaxCacheEntry)
		delete(c.entries, item.key)
		c.weight -= item.weight
		c.lru.Remove(old)
		c.evictions++
	}
	entry := &syntaxCacheEntry{key: key, source: bytes.Clone(source), files: files, parsed: parsed, weight: weight}
	c.entries[key] = c.lru.PushFront(entry)
	c.weight += weight
	return nil
}
