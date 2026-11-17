package overview

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type PlanPager struct {
	plan    *Plan
	report  PlanReport
	codec   *contextpack.CursorCodec
	binding contextpack.CursorBinding
	guard   contextpack.CursorGuard
	anchor  string
	expires int64
}

type PlanPageItem struct {
	PlanItem
	FragmentOf *model.SourceRef `json:"fragment_of"`
}

type PlanPage struct {
	Scope                  Scope                      `json:"scope"`
	TaskKind               string                     `json:"task_kind"`
	Intent                 string                     `json:"intent"`
	Constraints            []string                   `json:"constraints"`
	ProjectStage           string                     `json:"project_stage"`
	ImplementationEvidence string                     `json:"implementation_evidence"`
	ValidationStatus       string                     `json:"validation_status"`
	ReviewStatus           string                     `json:"review_status"`
	Gaps                   []string                   `json:"gaps"`
	Items                  []PlanPageItem             `json:"items"`
	RequiredPending        int                        `json:"required_pending"`
	PendingItems           int                        `json:"pending_items"`
	NextCursor             *string                    `json:"next_cursor"`
	NextPosition           contextpack.SignedPosition `json:"-"`
}

// NewPlanPager pins a complete plan. Its materialization has to be retained in
// a bounded cache by the application; it never guesses that the host reviewed
// a delivered fragment, and it never reads a newer working tree on continuation.
func NewPlanPager(ctx context.Context, plan *Plan, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard) (*PlanPager, error) {
	if ctx == nil || plan == nil || codec == nil || guard == nil || binding.Purpose != "list" || binding.ResultID != "" || binding.ResultDigest != "" || !pinnedOverviewMatches(binding.Scope, plan.overview) {
		return nil, ErrInput
	}
	report, err := plan.Read(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(report)
	if err != nil || len(raw) > plan.maxBytes {
		return nil, ErrLimit
	}
	digest, err := planCanonicalDigest("document-plan-v1", report, plan.maxBytes)
	if err != nil {
		return nil, err
	}
	binding.ResultID, err = groupResultID()
	if err != nil {
		return nil, ErrInput
	}
	binding.ResultDigest = digest
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := guard(ctx, b); err != nil {
			return err
		}
		return plan.authorize(ctx)
	}
	// Signing is private work. Verify below checks the complete plan after
	// signing before this pager/anchor can leave the owner.
	anchor, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
	if err != nil {
		return nil, err
	}
	claims, err := codec.Verify(ctx, anchor, binding, check)
	if err != nil {
		return nil, err
	}
	return &PlanPager{plan: plan, report: report, codec: codec, binding: claims.Binding, guard: guard, anchor: anchor, expires: claims.ExpiresAt}, nil
}

func (p *PlanPager) Anchor() string {
	if p == nil {
		return ""
	}
	return p.anchor
}

// VerifyOversizedItem binds a metadata descriptor to the next item of this
// exact plan and cursor, while rechecking the original authorization.
func (p *PlanPager) VerifyOversizedItem(ctx context.Context, cursor, principal string, current contextpack.CursorGuard, item *contextpack.OversizedItemError) error {
	if p == nil || ctx == nil || current == nil || item == nil || principal != p.binding.PrincipalDigest {
		return ErrInput
	}
	check := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if err := p.guard(ctx, binding); err != nil {
			return err
		}
		if err := current(ctx, binding); err != nil {
			return err
		}
		return p.plan.authorize(ctx)
	}
	anchor, err := p.codec.Verify(ctx, p.anchor, p.binding, check)
	if err != nil {
		return err
	}
	position := contextpack.SignedPosition{LastKey: []string{}}
	var previous contextpack.SignedCursor
	if cursor != "" {
		previous, err = p.codec.Verify(ctx, cursor, p.binding, check)
		if err != nil {
			return err
		}
		position = previous.Position
	}
	if position.NextItem < 0 || position.NextItem >= int64(len(p.report.Items)) || position.NextByteOffset != 0 || len(position.LastKey) != 0 {
		return contextpack.ErrCursorInvalid
	}
	candidate := p.report.Items[position.NextItem]
	if candidate.Kind != "structure_module" && candidate.Kind != "structure_file" || candidate.ID != item.ItemID || len(item.Sources) != 1 || candidate.Source != item.Sources[0] {
		return ErrAuthority
	}
	if err := p.codec.Recheck(ctx, anchor, check); err != nil {
		return err
	}
	if cursor != "" {
		return p.codec.Recheck(ctx, previous, check)
	}
	return ctx.Err()
}

// MaterializedBytes estimates serialized data retained by the pager, including
// captured files and the overview it references. The caller separately charges
// data captured by external callbacks; this is not an exact heap bound.
func (p *PlanPager) MaterializedBytes(limit int64) (int64, error) {
	if p == nil || p.plan == nil || p.plan.overview == nil || p.plan.snapshot == nil || limit < 1 || limit > 32*1024*1024 {
		return 0, ErrInput
	}
	var used int64
	add := func(value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return ErrInput
		}
		if int64(len(raw)) > limit-used {
			return ErrLimit
		}
		used += int64(len(raw))
		return nil
	}
	manifest := p.plan.snapshot.Manifest()
	if err := add(manifest); err != nil {
		return 0, err
	}
	for _, entry := range manifest.Files {
		file, ok := p.plan.snapshot.File(entry.Path)
		if !ok {
			continue
		}
		if int64(file.Len()) > limit-used {
			return 0, ErrLimit
		}
		used += int64(file.Len())
	}
	for _, value := range []any{p.plan.report, p.report, p.plan.declarations, p.plan.selectorJSON, p.plan.overview.summary, p.plan.overview.groups, p.plan.overview.paths} {
		if err := add(value); err != nil {
			return 0, err
		}
	}
	return used, nil
}

// Page delivers one whole item or one exact UTF-8 fragment. A partially sent
// mandatory item remains pending. maxBytes covers this JSON page and token;
// the transport still has to measure its complete outer envelope.
func (p *PlanPager) Page(ctx context.Context, cursor, principal string, current contextpack.CursorGuard, maxBytes int) (PlanPage, error) {
	if p == nil || ctx == nil || current == nil || principal != p.binding.PrincipalDigest || maxBytes < 512 || maxBytes > 131072 || int64(maxBytes) > p.plan.policyMaxOutput {
		return PlanPage{}, ErrInput
	}
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := p.guard(ctx, b); err != nil {
			return err
		}
		if err := current(ctx, b); err != nil {
			return err
		}
		return p.plan.authorize(ctx)
	}
	if time.Now().Unix() >= p.expires {
		return PlanPage{}, contextpack.ErrCursorExpired
	}
	anchor, err := p.codec.Verify(ctx, p.anchor, p.binding, check)
	if err != nil {
		return PlanPage{}, err
	}
	position := contextpack.SignedPosition{LastKey: []string{}}
	var previous contextpack.SignedCursor
	if cursor != "" {
		previous, err = p.codec.Verify(ctx, cursor, p.binding, check)
		if err != nil {
			return PlanPage{}, err
		}
		position = previous.Position
	}
	if len(position.LastKey) != 0 || position.NextItem > int64(len(p.report.Items)) || position.NextItem == int64(len(p.report.Items)) && position.NextByteOffset != 0 {
		return PlanPage{}, contextpack.ErrCursorInvalid
	}
	index, offset := int(position.NextItem), int(position.NextByteOffset)
	result := PlanPage{Scope: p.report.Scope, TaskKind: p.report.TaskKind, Intent: p.report.Intent, Constraints: append([]string{}, p.report.Constraints...), ProjectStage: p.report.ProjectStage, ImplementationEvidence: p.report.ImplementationEvidence, ValidationStatus: p.report.ValidationStatus, ReviewStatus: "not_checked", Gaps: append([]string{}, p.report.Gaps...), Items: []PlanPageItem{}, NextPosition: position}
	if index < len(p.report.Items) {
		item := p.report.Items[index]
		data := []byte(item.Content)
		structure := item.Kind == "structure_module" || item.Kind == "structure_file"
		if offset < 0 || offset >= len(data) || offset > 0 && !utf8.RuneStart(data[offset]) {
			return PlanPage{}, contextpack.ErrCursorInvalid
		}
		if structure && offset != 0 {
			return PlanPage{}, contextpack.ErrCursorInvalid
		}
		file, ok := p.plan.snapshot.File(item.Source.Path)
		if !ok || file.ContentHash() != item.Source.ContentHash || item.Source.Range.EndByte > int64(file.Len()) {
			return PlanPage{}, ErrInput
		}
		whole, err := file.Range(int(item.Source.Range.StartByte), int(item.Source.Range.EndByte))
		if err != nil || !structure && !bytes.Equal(whole, data) {
			return PlanPage{}, ErrInput
		}
		end := len(data)
		for {
			part := item
			part.Content = string(data[offset:end])
			var parent *model.SourceRef
			if offset != 0 || end != len(data) {
				ref := item.Source
				ref.Range = model.ByteRange{StartByte: item.Source.Range.StartByte + int64(offset), EndByte: item.Source.Range.StartByte + int64(end)}
				id, e := model.SourceFragmentID(item.Source, ref.Range, file.Bytes(), 65536)
				if e != nil {
					return PlanPage{}, ErrInput
				}
				part.ID = id
				part.Source = ref
				original := item.Source
				parent = &original
			}
			result.Items = []PlanPageItem{{PlanItem: part, FragmentOf: parent}}
			next := contextpack.SignedPosition{NextItem: int64(index), NextByteOffset: int64(end), NextCoverageGap: position.NextCoverageGap, NextWarningGap: position.NextWarningGap, LastKey: []string{}}
			if end == len(data) {
				next.NextItem = int64(index + 1)
				next.NextByteOffset = 0
			}
			result.NextPosition = next
			result.PendingItems = len(p.report.Items) - int(next.NextItem)
			result.RequiredPending = 0
			for i := int(next.NextItem); i < len(p.report.Items); i++ {
				if p.report.Items[i].RequiredForReview {
					result.RequiredPending++
				}
			}
			if result.PendingItems > 0 {
				if !contextpack.CursorAdvances(position, next) {
					return PlanPage{}, contextpack.ErrCursorInvalid
				}
				token, e := p.codec.Issue(ctx, p.binding, next, check)
				if e != nil {
					return PlanPage{}, e
				}
				result.NextCursor = &token
			} else {
				result.NextCursor = nil
			}
			raw, e := json.Marshal(result)
			if e != nil {
				return PlanPage{}, ErrInput
			}
			if len(raw) <= maxBytes {
				break
			}
			if structure {
				// The caller may still advance safety-gap details without
				// consuming or truncating this indivisible item.
				result.Items = []PlanPageItem{}
				result.NextPosition = position
				result.NextCursor = nil
				result.PendingItems = len(p.report.Items) - index
				result.RequiredPending = 0
				for i := index; i < len(p.report.Items); i++ {
					if p.report.Items[i].RequiredForReview {
						result.RequiredPending++
					}
				}
				if err := p.codec.Recheck(ctx, anchor, check); err != nil {
					return PlanPage{}, err
				}
				if cursor != "" {
					if err := p.codec.Recheck(ctx, previous, check); err != nil {
						return PlanPage{}, err
					}
				}
				return result, &contextpack.OversizedItemError{ItemID: item.ID, Sources: []model.SourceRef{item.Source}}
			}
			span := (end - offset) / 2
			if span < 1 {
				return PlanPage{}, ErrLimit
			}
			end = offset + span
			for end > offset && end < len(data) && !utf8.RuneStart(data[end]) {
				end--
			}
			if newline := bytes.LastIndexByte(data[offset:end], '\n'); newline >= 0 {
				end = offset + newline + 1
			}
			if end == offset {
				return PlanPage{}, ErrLimit
			}
		}
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > maxBytes {
		return PlanPage{}, ErrLimit
	}
	if err = p.codec.Recheck(ctx, anchor, check); err != nil {
		return PlanPage{}, err
	}
	if cursor != "" {
		if err = p.codec.Recheck(ctx, previous, check); err != nil {
			return PlanPage{}, err
		}
	}
	if result.NextCursor != nil {
		if _, err = p.codec.Verify(ctx, *result.NextCursor, p.binding, check); err != nil {
			return PlanPage{}, err
		}
	}
	if time.Now().Unix() >= p.expires {
		return PlanPage{}, contextpack.ErrCursorExpired
	}
	return result, nil
}
