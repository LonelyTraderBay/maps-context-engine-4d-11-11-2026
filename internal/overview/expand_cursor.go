package overview

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type GroupPager struct {
	report    *Report
	expansion Expansion
	codec     *contextpack.CursorCodec
	binding   contextpack.CursorBinding
	guard     contextpack.CursorGuard
	anchor    string
	expires   int64
}

type GroupPage struct {
	Sites      []Site  `json:"sites"`
	Pending    int     `json:"pending"`
	NextCursor *string `json:"next_cursor"`
}

func groupResultID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

func pinnedOverviewMatches(scope model.ScopeVector, report *Report) bool {
	if report == nil || model.VerifyScopeVectorDigest(scope, 32768) != nil {
		return false
	}
	member, err := model.MatchPinnedMember(scope, &model.ModuleRef{Kind: "module", GraphIdentity: report.scope.Identity, ModuleID: "group"}, nil)
	return err == nil && member.SnapshotDigest == report.scope.SnapshotDigest && member.PolicyDigest == report.scope.PolicyDigest && member.PolicyEpoch == report.scope.PolicyEpoch
}

// NewGroupPager binds the complete already-authorized group to a signed,
// process-local materialization. The owner must bound retained pagers; losing
// one is expiry, never a reason to switch to a newer generation.
func NewGroupPager(ctx context.Context, report *Report, ref Expansion, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard) (*GroupPager, error) {
	if ctx == nil || report == nil || codec == nil || guard == nil || binding.Purpose != "list" || binding.ResultID != "" || binding.ResultDigest != "" || ref.Scope != report.scope || ref.FactsDigest != report.factsDigest {
		return nil, ErrInput
	}
	if _, ok := report.groups[ref.GroupID]; !ok {
		return nil, ErrInput
	}
	if !pinnedOverviewMatches(binding.Scope, report) {
		return nil, ErrInput
	}
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := guard(ctx, b); err != nil {
			return err
		}
		return report.authorize(ctx, report.paths)
	}
	if err := report.authorize(ctx, report.paths); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ref)
	if err != nil || len(raw) > 32768 {
		return nil, ErrInput
	}
	sum := sha256.Sum256(append([]byte("mce-overview-group-v1\n"), raw...))
	binding.ResultID, err = groupResultID()
	if err != nil {
		return nil, ErrInput
	}
	binding.ResultDigest = hex.EncodeToString(sum[:])
	anchor, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, check)
	if err != nil {
		return nil, err
	}
	claims, err := codec.Verify(ctx, anchor, binding, check)
	if err != nil {
		return nil, err
	}
	return &GroupPager{report: report, expansion: ref, codec: codec, binding: claims.Binding, guard: guard, anchor: anchor, expires: claims.ExpiresAt}, nil
}

// Anchor is a signed group descriptor; it is not a substitute for a current
// authorization check or for retaining the original materialization.
func (p *GroupPager) Anchor() string {
	if p == nil {
		return ""
	}
	return p.anchor
}

func (p *GroupPager) Page(ctx context.Context, cursor, principal string, current contextpack.CursorGuard, maxItems, maxBytes int) (GroupPage, error) {
	if p == nil || ctx == nil || current == nil || principal != p.binding.PrincipalDigest || maxItems < 1 || maxItems > 10000 || maxBytes < 4096 || maxBytes > 131072 {
		return GroupPage{}, ErrInput
	}
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := p.guard(ctx, b); err != nil {
			return err
		}
		if err := current(ctx, b); err != nil {
			return err
		}
		return p.report.authorize(ctx, p.report.paths)
	}
	if time.Now().Unix() >= p.expires {
		return GroupPage{}, contextpack.ErrCursorExpired
	}
	anchor, err := p.codec.Verify(ctx, p.anchor, p.binding, check)
	if err != nil {
		return GroupPage{}, err
	}
	position := contextpack.SignedPosition{LastKey: []string{}}
	var previous contextpack.SignedCursor
	if cursor != "" {
		previous, err = p.codec.Verify(ctx, cursor, p.binding, check)
		if err != nil {
			return GroupPage{}, err
		}
		position = previous.Position
	}
	entries := p.report.groups[p.expansion.GroupID]
	if len(position.LastKey) != 0 || position.NextCoverageGap != 0 || position.NextWarningGap != 0 || position.NextByteOffset != 0 || position.NextItem > int64(len(entries)) {
		return GroupPage{}, contextpack.ErrCursorInvalid
	}
	start := int(position.NextItem)
	after := ""
	if start > 0 {
		after = entries[start-1].Relation.RelationID
	}
	limit := maxItems
	for {
		page, err := p.report.Expand(ctx, p.expansion, after, limit)
		if err != nil {
			return GroupPage{}, err
		}
		nextIndex := start + len(page.Sites)
		if nextIndex > len(entries) || nextIndex < len(entries) && len(page.Sites) == 0 {
			return GroupPage{}, ErrLimit
		}
		result := GroupPage{Sites: page.Sites, Pending: len(entries) - nextIndex}
		if result.Pending > 0 {
			nextPosition := contextpack.SignedPosition{NextItem: int64(nextIndex), LastKey: []string{}}
			if !contextpack.CursorAdvances(position, nextPosition) {
				return GroupPage{}, contextpack.ErrCursorInvalid
			}
			token, e := p.codec.Issue(ctx, p.binding, nextPosition, check)
			if e != nil {
				return GroupPage{}, e
			}
			result.NextCursor = &token
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return GroupPage{}, ErrInput
		}
		if len(raw) > maxBytes {
			if limit == 1 {
				return GroupPage{}, ErrLimit
			}
			limit /= 2
			continue
		}
		if err = p.codec.Recheck(ctx, anchor, check); err != nil {
			return GroupPage{}, err
		}
		if cursor != "" {
			if err = p.codec.Recheck(ctx, previous, check); err != nil {
				return GroupPage{}, err
			}
		}
		if result.NextCursor != nil {
			if _, err = p.codec.Verify(ctx, *result.NextCursor, p.binding, check); err != nil {
				return GroupPage{}, err
			}
		}
		if time.Now().Unix() >= p.expires {
			return GroupPage{}, contextpack.ErrCursorExpired
		}
		return result, nil
	}
}
