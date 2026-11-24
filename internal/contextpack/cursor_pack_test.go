package contextpack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func packCursorBinding(slice *TaskSlice) CursorBinding {
	return CursorBinding{Purpose: "pack", PrincipalDigest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64), RequestDigest: strings.Repeat("3", 64), OrderVersion: "pack-v1", Scope: slice.report.Scope}
}

func TestExplorePackDeliversTargetDirectCallBeforeOtherEvidence(t *testing.T) {
	ctx, input, policy, rules := taskFixture(t)
	selection, err := Select(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	slice, err := BuildTaskSlice(ctx, SliceInput{Selection: selection, Policy: policy, TaskKind: "explore",
		RuleAnchors: rules[:2], MaxBytes: 4 << 20,
		VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	options := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 24576, MaxItems: 1, DeliveryMode: "mcp_dual"}
	binding := packCursorBinding(slice)
	original, _, err := NewPagedPack(ctx, slice, codec, binding, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	binding.OrderVersion = ExplorePackOrder
	pack, token, err := NewPagedPack(ctx, slice, codec, binding, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	first, err := pack.First(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	items := first.Response.Data.(model.PackData).Items
	if len(items) != 1 || items[0].Kind != "reference_catalog" || items[0].RequiredForReview ||
		len(items[0].EvidenceRefs) != 1 || items[0].EvidenceRefs[0].EvidenceID != "r-helper" ||
		items[0].EvidenceRefs[0].Kind != "type_resolved" {
		t.Fatalf("expected the selected symbol's verified outgoing call, got %+v", items)
	}
	paging := first.Response.Pagination
	if paging.NextCursor == nil || paging.RequiredPending == nil || *paging.RequiredPending == 0 {
		t.Fatalf("required evidence must remain pending: %+v", paging)
	}
	second, err := pack.Continue(ctx, token, *paging.NextCursor, binding.PrincipalDigest, allowCursor, options)
	if err != nil {
		t.Fatal(err)
	}
	oldFirst, err := original.First(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	oldItems := oldFirst.Response.Data.(model.PackData).Items
	nextItems := second.Response.Data.(model.PackData).Items
	if len(oldItems) != 1 || oldItems[0].WhyIncluded != "target_source" ||
		len(nextItems) != 1 || nextItems[0].ItemID != oldItems[0].ItemID {
		t.Fatalf("the original source must remain available by continuation: %+v, %+v", oldItems, nextItems)
	}
}

func TestPagedPackOversizedDescriptorBindsPosition(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, err := NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	pager, _, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil || len(pager.items) < 2 {
		t.Fatalf("fixture has too few items: %v", err)
	}
	first := &OversizedItemError{ItemID: pager.items[0].ID, Sources: append([]model.SourceRef{}, pager.items[0].SourceRefs...)}
	if err := pager.VerifyOversizedItem(ctx, "", pager.binding.PrincipalDigest, allowCursor, first); err != nil {
		t.Fatalf("next item was not verified: %v", err)
	}
	second := &OversizedItemError{ItemID: pager.items[1].ID, Sources: append([]model.SourceRef{}, pager.items[1].SourceRefs...)}
	if err := pager.VerifyOversizedItem(ctx, "", pager.binding.PrincipalDigest, allowCursor, second); !errors.Is(err, ErrAuthority) {
		t.Fatalf("descriptor for another position was accepted: %v", err)
	}
	binding := pager.binding
	binding.Purpose = "list"
	cursor, err := codec.Issue(ctx, binding, SignedPosition{NextItem: 1, LastKey: []string{}}, pager.authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := pager.VerifyOversizedItem(ctx, cursor, binding.PrincipalDigest, allowCursor, second); err != nil {
		t.Fatalf("cursor-bound descriptor was rejected: %v", err)
	}
	if err := pager.VerifyOversizedItem(ctx, cursor, binding.PrincipalDigest, allowCursor, first); !errors.Is(err, ErrAuthority) {
		t.Fatalf("old item was accepted on a new position: %v", err)
	}
	denied := func(context.Context, CursorBinding) error { return ErrAuthority }
	if err := pager.VerifyOversizedItem(ctx, cursor, binding.PrincipalDigest, denied, second); !errors.Is(err, ErrAuthority) {
		t.Fatalf("revoked descriptor passed: %v", err)
	}
	for i, candidate := range pager.items {
		if candidate.Kind != "validation_obligation" {
			continue
		}
		cursor, err := codec.Issue(ctx, binding, SignedPosition{NextItem: int64(i), LastKey: []string{}}, pager.authorize)
		if err != nil {
			t.Fatal(err)
		}
		descriptor := &OversizedItemError{ItemID: candidate.ID, Sources: []model.SourceRef{}}
		if err := pager.VerifyOversizedItem(ctx, cursor, binding.PrincipalDigest, allowCursor, descriptor); err != nil {
			t.Fatalf("derived obligation descriptor was rejected: %v", err)
		}
		return
	}
	t.Fatal("fixture omitted validation obligation")
}

func TestPagedPackReassemblesSourceAndMeasuresWire(t *testing.T) {
	ctx, slice, sourceItem, source := fragmentFixture(t)
	codec, err := NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	pager, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	options := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 4, DeliveryMode: "mcp_dual"}
	page, err := pager.First(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	var fragments []model.EvidenceItem
	seen := map[string]bool{}
	pages := 0
	for {
		pages++
		if pages > 512 {
			t.Fatal("cursor did not terminate")
		}
		if len(page.Blocks) != 2 || !bytes.Equal(page.Blocks[0], page.Blocks[1]) || !json.Valid(page.Blocks[0]) || model.ValidateDeliveredBytes(page.Response.Budget, page.Blocks) != nil || page.Response.Budget.TokenCount != nil || page.Response.Budget.TokenizerID != nil {
			t.Fatal("invalid serialized accounting")
		}
		pack := page.Response.Data.(model.PackData)
		for _, item := range pack.Items {
			if seen[item.ItemID] {
				t.Fatal("duplicate item across pages")
			}
			seen[item.ItemID] = true
			if item.FragmentOf != nil && *item.FragmentOf == sourceItem.SourceRefs[0] {
				fragments = append(fragments, item)
				if item.SourceRefs[0].Range.EndByte < sourceItem.SourceRefs[0].Range.EndByte && (page.Response.Pagination.RequiredPending == nil || *page.Response.Pagination.RequiredPending < 1) {
					t.Fatal("incomplete source was not pending")
				}
			}
		}
		next := page.Response.Pagination.NextCursor
		if next == nil {
			if page.Response.Pagination.Truncated {
				t.Fatal("pending work lacks cursor")
			}
			break
		}
		if !page.Response.Pagination.Truncated {
			t.Fatal("unexpected cursor")
		}
		if pages%2 == 0 {
			options.MaxOutputBytes = 20000
		} else {
			options.MaxOutputBytes = 16384
		}
		page, err = pager.Continue(ctx, token, *next, packCursorBinding(slice).PrincipalDigest, allowCursor, options)
		if err != nil {
			t.Fatal(err)
		}
	}
	if pages < 2 || len(fragments) < 2 {
		t.Fatal("large source did not span pages")
	}
	if err = model.ValidateCompleteFragmentSequence(sourceItem.SourceRefs[0], fragments, source, 65536); err != nil {
		t.Fatal(err)
	}
	newPager, newToken, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil || newToken == token {
		t.Fatalf("fresh pack not independent: %v", err)
	}
	newFirst, err := newPager.First(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(newFirst.Response.Data.(model.PackData).Items) == 0 {
		t.Fatal("fresh pack skipped prior source")
	}
}

func TestPagedPackRejectsCrossPackRevocationAndExpiry(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{7})
	now := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	codec.now = func() time.Time { return now }
	revoked := false
	guard := func(context.Context, CursorBinding) error {
		if revoked {
			return ErrAuthority
		}
		return nil
	}
	pager, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), guard)
	if err != nil {
		t.Fatal(err)
	}
	options := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "mcp_dual"}
	first, err := pager.First(ctx, options)
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("no continuation: %v", err)
	}
	cursor := *first.Response.Pagination.NextCursor
	other, otherToken, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), guard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.Continue(ctx, otherToken, cursor, packCursorBinding(slice).PrincipalDigest, guard, options); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("cross-pack cursor accepted: %v", err)
	}
	if _, err = pager.Continue(ctx, token, cursor, strings.Repeat("f", 64), allowCursor, options); !errors.Is(err, ErrAuthority) {
		t.Fatalf("cross-principal cursor accepted: %v", err)
	}
	if _, err = pager.Continue(ctx, token, cursor, packCursorBinding(slice).PrincipalDigest, nil, options); !errors.Is(err, ErrInput) {
		t.Fatalf("missing current request authority accepted: %v", err)
	}
	revoked = true
	if _, err = pager.Continue(ctx, token, cursor, packCursorBinding(slice).PrincipalDigest, guard, options); !errors.Is(err, ErrAuthority) {
		t.Fatalf("revocation ignored: %v", err)
	}
	revoked = false
	now = now.Add(15 * time.Minute)
	if _, err = pager.Continue(ctx, token, cursor, packCursorBinding(slice).PrincipalDigest, guard, options); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired pack resumed: %v", err)
	}
	if _, err = pager.Continue(ctx, token, "", packCursorBinding(slice).PrincipalDigest, guard, options); !errors.Is(err, ErrInput) {
		t.Fatalf("empty cursor accepted: %v", err)
	}
}

func TestPagedPackRejectsUnresumableTraversalAndSmallEnvelope(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{7})
	slice.report.EnumerationComplete = false
	if _, _, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor); !errors.Is(err, ErrContinuationUnavailable) {
		t.Fatalf("unresumable traversal accepted: %v", err)
	}
	slice.report.EnumerationComplete = true
	pager, _, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	options := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 4096, DeliveryMode: "mcp_dual"}
	if _, err = pager.First(ctx, options); !errors.Is(err, ErrBudgetTooSmall) {
		t.Fatalf("minimum envelope exceeded: %v", err)
	}
}

func TestPagedPackKeepsSafetyGapDetailsAcrossPages(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	for i := 0; i < 75; i++ {
		slice.report.Gaps = append(slice.report.Gaps, model.Gap{Code: fmt.Sprintf("GAP_FIXTURE_%02d", i), Reason: strings.Repeat("safety ", 25), SourceRefs: []model.SourceRef{}})
	}
	codec, _ := NewCursorCodec([32]byte{7})
	pager, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	options := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 3, DeliveryMode: "mcp_dual"}
	page, err := pager.First(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	grouped := false
	for pageCount := 0; ; pageCount++ {
		if pageCount > 512 {
			t.Fatal("gap continuation did not terminate")
		}
		for _, gap := range page.Response.Warnings {
			if gap.Code == "GAP_DETAILS_PENDING" {
				grouped = true
				if gap.Count == nil || *gap.Count <= 0 || page.Response.Pagination.RequiredPending == nil || *page.Response.Pagination.RequiredPending < *gap.Count {
					t.Fatal("grouped details were not retained as required")
				}
			} else if strings.HasPrefix(gap.Code, "GAP_FIXTURE_") {
				if seen[gap.Code] {
					t.Fatal("gap repeated across pages")
				}
				seen[gap.Code] = true
			}
		}
		if page.Response.Pagination.NextCursor == nil {
			break
		}
		page, err = pager.Continue(ctx, token, *page.Response.Pagination.NextCursor, packCursorBinding(slice).PrincipalDigest, allowCursor, options)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !grouped || len(seen) != 75 {
		t.Fatalf("gap details lost: grouped=%v count=%d", grouped, len(seen))
	}
}

func TestPagedPackRechecksAuthorityAfterHostProbe(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{7})
	allowed := true
	guard := func(context.Context, CursorBinding) error {
		if !allowed {
			return ErrAuthority
		}
		return nil
	}
	pager, _, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), guard)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	opts := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "mcp_structured_verified", VerifyStructuredHost: func(context.Context) error {
		calls++
		if calls == 2 {
			allowed = false
		}
		return nil
	}}
	if _, err = pager.First(ctx, opts); !errors.Is(err, ErrAuthority) || calls != 2 {
		t.Fatalf("late authority change ignored: calls=%d err=%v", calls, err)
	}
}

func TestPagedPackRechecksSourceAfterHostProbe(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{7})
	allowed := true
	pathAuth := slice.selection.pathAuth
	slice.selection.pathAuth = func(ctx context.Context, member model.ScopeMember, path string) error {
		if !allowed {
			return ErrAuthority
		}
		return pathAuth(ctx, member, path)
	}
	guard := func(context.Context, CursorBinding) error { return nil }
	pager, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), guard)
	if err != nil {
		t.Fatal(err)
	}
	opts := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 8192, MaxItems: 1, DeliveryMode: "cli"}
	first, err := pager.First(ctx, opts)
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("fixture did not yield a continuation: %v", err)
	}
	calls := 0
	opts.DeliveryMode = "mcp_structured_verified"
	opts.VerifyStructuredHost = func(context.Context) error {
		calls++
		if calls == 2 {
			allowed = false
		}
		return nil
	}
	page, err := pager.Continue(ctx, token, *first.Response.Pagination.NextCursor, packCursorBinding(slice).PrincipalDigest, guard, opts)
	if !errors.Is(err, ErrAuthority) || calls != 2 || len(page.Blocks) != 0 {
		t.Fatalf("late source revocation leaked a continuation page: calls=%d err=%v", calls, err)
	}
}
