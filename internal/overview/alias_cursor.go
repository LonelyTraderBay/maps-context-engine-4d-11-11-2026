package overview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type AliasPager struct {
	index   *AliasIndex
	result  AliasResult
	codec   *contextpack.CursorCodec
	binding contextpack.CursorBinding
	guard   contextpack.CursorGuard
	anchor  string
	expires int64
}

type AliasPage struct {
	Candidates []AliasCandidate `json:"candidates"`
	Ambiguous  bool             `json:"ambiguous"`
	Pending    int              `json:"pending"`
	NextCursor *string          `json:"next_cursor"`
}

// NewAliasPager pins the complete candidate set and query identity once.
// The raw query is not placed in continuation tokens. Pagers are process-local
// and must be retained under a bounded owner-controlled cache.
func NewAliasPager(ctx context.Context, index *AliasIndex, query string, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard) (*AliasPager, error) {
	if ctx == nil || index == nil || codec == nil || guard == nil || binding.Purpose != "list" || binding.ResultID != "" || binding.ResultDigest != "" || !pinnedOverviewMatches(binding.Scope, index.overview) {
		return nil, ErrInput
	}
	result, err := index.collect(ctx, query)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		Set    string      `json:"set"`
		Query  string      `json:"query"`
		Result AliasResult `json:"result"`
	}{index.digest, query, result})
	if err != nil || len(raw) > index.maxBytes+8192 {
		return nil, ErrLimit
	}
	sum := sha256.Sum256(append([]byte("mce-alias-result-v1\n"), raw...))
	binding.ResultID, err = groupResultID()
	if err != nil {
		return nil, ErrInput
	}
	binding.ResultDigest = hex.EncodeToString(sum[:])
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := guard(ctx, b); err != nil {
			return err
		}
		return index.authorize(ctx)
	}
	anchor, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, check)
	if err != nil {
		return nil, err
	}
	claims, err := codec.Verify(ctx, anchor, binding, check)
	if err != nil {
		return nil, err
	}
	return &AliasPager{index: index, result: result, codec: codec, binding: claims.Binding, guard: guard, anchor: anchor, expires: claims.ExpiresAt}, nil
}

func (p *AliasPager) Anchor() string {
	if p == nil {
		return ""
	}
	return p.anchor
}

func (p *AliasPager) Page(ctx context.Context, cursor, principal string, current contextpack.CursorGuard, maxItems, maxBytes int) (AliasPage, error) {
	if p == nil || ctx == nil || current == nil || principal != p.binding.PrincipalDigest || maxItems < 1 || maxItems > 100 || maxBytes < 4096 || maxBytes > 131072 {
		return AliasPage{}, ErrInput
	}
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := p.guard(ctx, b); err != nil {
			return err
		}
		if err := current(ctx, b); err != nil {
			return err
		}
		return p.index.authorize(ctx)
	}
	if time.Now().Unix() >= p.expires {
		return AliasPage{}, contextpack.ErrCursorExpired
	}
	anchor, err := p.codec.Verify(ctx, p.anchor, p.binding, check)
	if err != nil {
		return AliasPage{}, err
	}
	position := contextpack.SignedPosition{LastKey: []string{}}
	var previous contextpack.SignedCursor
	if cursor != "" {
		previous, err = p.codec.Verify(ctx, cursor, p.binding, check)
		if err != nil {
			return AliasPage{}, err
		}
		position = previous.Position
	}
	if len(position.LastKey) != 0 || position.NextCoverageGap != 0 || position.NextWarningGap != 0 || position.NextByteOffset != 0 || position.NextItem > int64(len(p.result.Candidates)) {
		return AliasPage{}, contextpack.ErrCursorInvalid
	}
	start := int(position.NextItem)
	limit := maxItems
	for {
		end := start + limit
		if end > len(p.result.Candidates) {
			end = len(p.result.Candidates)
		}
		items := make([]AliasCandidate, 0, end-start)
		for _, candidate := range p.result.Candidates[start:end] {
			copied := candidate
			copied.SourceRefs = append([]model.SourceRef{}, candidate.SourceRefs...)
			items = append(items, copied)
		}
		result := AliasPage{Candidates: items, Ambiguous: p.result.Ambiguous, Pending: len(p.result.Candidates) - end}
		if result.Pending > 0 {
			next := contextpack.SignedPosition{NextItem: int64(end), LastKey: []string{}}
			if !contextpack.CursorAdvances(position, next) {
				return AliasPage{}, contextpack.ErrCursorInvalid
			}
			token, e := p.codec.Issue(ctx, p.binding, next, check)
			if e != nil {
				return AliasPage{}, e
			}
			result.NextCursor = &token
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return AliasPage{}, ErrInput
		}
		if len(raw) > maxBytes {
			if limit == 1 {
				return AliasPage{}, ErrLimit
			}
			limit /= 2
			continue
		}
		if err = p.codec.Recheck(ctx, anchor, check); err != nil {
			return AliasPage{}, err
		}
		if cursor != "" {
			if err = p.codec.Recheck(ctx, previous, check); err != nil {
				return AliasPage{}, err
			}
		}
		if result.NextCursor != nil {
			if _, err = p.codec.Verify(ctx, *result.NextCursor, p.binding, check); err != nil {
				return AliasPage{}, err
			}
		}
		if time.Now().Unix() >= p.expires {
			return AliasPage{}, contextpack.ErrCursorExpired
		}
		return result, nil
	}
}
