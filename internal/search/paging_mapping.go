package search

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

var (
	ErrMappingPageInvalid = errors.New("mapping page invalid")
	ErrMappingPageLarge   = errors.New("mapping item exceeds item budget")
	ErrMappingPageExpired = errors.New("mapping materialization expired")
)

type mappingEntry struct {
	key [3]string
	raw []byte
}

// MappingPager owns a bounded, immutable process-local set of source-anchor
// and target pairs. A caller must separately budget its complete wire envelope.
type MappingPager struct {
	entries []mappingEntry
	status  string
	codec   *contextpack.CursorCodec
	binding contextpack.CursorBinding
	guard   contextpack.CursorGuard
	anchor  string
	expires int64
	now     func() time.Time
}

type MappingPage struct {
	Matches             []model.MappingMatch
	Status              string
	Total               int64
	Remaining           int64
	NextCursor          *string
	SerializedItemBytes int
}

func mappingID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

func mappingKeyCompare(a, b [3]string) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// NewMappingPager accepts a completely enumerated set. The trusted guard must
// verify session, source authority, binding/resolver and epochs. A null target
// has no CT-05 graph-ref-v1 key, so it is rejected for explicit gap handling.
func NewMappingPager(ctx context.Context, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard, scanner *policy.Scanner, buildMapID, bindingID, status string, matches []model.MappingMatch, maxEntries, maxBytes int) (*MappingPager, error) {
	if ctx == nil || codec == nil || guard == nil || scanner == nil || binding.Purpose != "mapping" || binding.ResultID != "" || binding.ResultDigest != "" || matches == nil || maxEntries < 1 || maxEntries > 10000 || len(matches) > maxEntries || maxBytes < 1 || maxBytes > 16*1024*1024 {
		return nil, ErrMappingPageInvalid
	}
	p := &MappingPager{entries: make([]mappingEntry, 0, len(matches)), status: status, codec: codec, binding: binding, guard: guard, now: time.Now}
	id, err := mappingID()
	if err != nil {
		return nil, err
	}
	p.binding.ResultID = id
	p.anchor, err = codec.Issue(ctx, p.binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
	if err != nil {
		return nil, err
	}
	claims, err := codec.Verify(ctx, p.anchor, p.binding, guard)
	if err != nil {
		return nil, err
	}
	p.expires = claims.ExpiresAt
	seen := map[string][]byte{}
	targets := map[string]bool{}
	used := 0
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(match)
		if err != nil || len(raw) > maxBytes-used {
			return nil, ErrMappingPageLarge
		}
		decoded, err := model.DecodeMappingMatchJSON(raw, maxBytes)
		if err != nil || decoded.TargetRef == nil || decoded.AnchorRef.BuildMapID != buildMapID || decoded.AnchorRef.BindingID != bindingID {
			return nil, ErrMappingPageInvalid
		}
		if status == "exact_in_supported_scope" && decoded.MappingStatus != "exact_in_supported_scope" {
			return nil, ErrMappingPageInvalid
		}
		if err = scanner.CheckContent(raw); err != nil {
			return nil, err
		}
		target, err := model.GraphRefDigest(decoded.TargetRef, maxBytes)
		if err != nil {
			return nil, ErrMappingPageInvalid
		}
		if previous, ok := seen[decoded.MatchID]; ok {
			if !bytes.Equal(previous, raw) {
				return nil, ErrMappingPageInvalid
			}
			continue
		}
		seen[decoded.MatchID] = append([]byte(nil), raw...)
		targets[target] = true
		used += len(raw)
		p.entries = append(p.entries, mappingEntry{key: [3]string{decoded.AnchorRef.AnchorID, target, decoded.MatchID}, raw: append([]byte(nil), raw...)})
	}
	sort.Slice(p.entries, func(i, j int) bool { return mappingKeyCompare(p.entries[i].key, p.entries[j].key) < 0 })
	uniqueTargets := int64(len(targets))
	if err := model.ValidateMappingCompleteness(status, true, &uniqueTargets); err != nil {
		return nil, ErrMappingPageInvalid
	}
	if err = codec.Recheck(ctx, claims, guard); err != nil {
		return nil, err
	}
	return p, nil
}

// Page accepts only the original materialization and checks the current
// authenticated principal and authority on every call. maxItemBytes includes
// the JSON array and its items but not an envelope, cursor or SDK duplication.
func (p *MappingPager) Page(ctx context.Context, cursor, principal string, currentGuard contextpack.CursorGuard, limit, maxItemBytes int) (MappingPage, error) {
	if p == nil || ctx == nil || currentGuard == nil || principal != p.binding.PrincipalDigest || limit < 1 || limit > 100 || maxItemBytes < 2 || maxItemBytes > 131072 {
		return MappingPage{}, ErrMappingPageInvalid
	}
	authorize := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := p.guard(ctx, b); err != nil {
			return err
		}
		return currentGuard(ctx, b)
	}
	if p.now().Unix() >= p.expires {
		return MappingPage{}, ErrMappingPageExpired
	}
	anchorClaims, err := p.codec.Verify(ctx, p.anchor, p.binding, authorize)
	if err != nil {
		return MappingPage{}, err
	}
	pos := contextpack.SignedPosition{LastKey: []string{}}
	var cursorClaims contextpack.SignedCursor
	if cursor != "" {
		cursorClaims, err = p.codec.Verify(ctx, cursor, p.binding, authorize)
		if err != nil {
			return MappingPage{}, err
		}
		pos = cursorClaims.Position
	}
	if pos.NextItem < 0 || pos.NextItem > int64(len(p.entries)) || pos.NextCoverageGap != 0 || pos.NextWarningGap != 0 || pos.NextByteOffset != 0 {
		return MappingPage{}, ErrMappingPageInvalid
	}
	if pos.NextItem == 0 {
		if len(pos.LastKey) != 0 {
			return MappingPage{}, ErrMappingPageInvalid
		}
	} else {
		key := p.entries[pos.NextItem-1].key
		if len(pos.LastKey) != 3 || pos.LastKey[0] != key[0] || pos.LastKey[1] != key[1] || pos.LastKey[2] != key[2] {
			return MappingPage{}, ErrMappingPageInvalid
		}
	}
	items := make([]model.MappingMatch, 0, limit)
	used := 2
	index := int(pos.NextItem)
	for index < len(p.entries) && len(items) < limit {
		if err := ctx.Err(); err != nil {
			return MappingPage{}, err
		}
		entry := p.entries[index]
		more := len(entry.raw)
		if len(items) > 0 {
			more++
		}
		if more > maxItemBytes-used {
			if len(items) == 0 {
				return MappingPage{}, ErrMappingPageLarge
			}
			break
		}
		item, err := model.DecodeMappingMatchJSON(entry.raw, maxItemBytes)
		if err != nil {
			return MappingPage{}, ErrMappingPageInvalid
		}
		items = append(items, item)
		used += more
		index++
	}
	remaining := len(p.entries) - index
	var next *string
	if remaining > 0 {
		key := p.entries[index-1].key
		newPos := contextpack.SignedPosition{NextItem: int64(index), LastKey: []string{key[0], key[1], key[2]}}
		if !contextpack.CursorAdvances(pos, newPos) {
			return MappingPage{}, ErrMappingPageInvalid
		}
		value, err := p.codec.Issue(ctx, p.binding, newPos, authorize)
		if err != nil {
			return MappingPage{}, err
		}
		next = &value
	}
	if err = p.codec.Recheck(ctx, anchorClaims, authorize); err != nil {
		return MappingPage{}, err
	}
	if cursor != "" {
		if err = p.codec.Recheck(ctx, cursorClaims, authorize); err != nil {
			return MappingPage{}, err
		}
	}
	if next != nil {
		if _, err = p.codec.Verify(ctx, *next, p.binding, authorize); err != nil {
			return MappingPage{}, err
		}
	}
	if p.now().Unix() >= p.expires {
		return MappingPage{}, ErrMappingPageExpired
	}
	return MappingPage{Matches: items, Status: p.status, Total: int64(len(p.entries)), Remaining: int64(remaining), NextCursor: next, SerializedItemBytes: used}, nil
}
