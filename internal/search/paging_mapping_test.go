package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func mappingFixture(t *testing.T, count int) (context.Context, context.CancelFunc, *contextpack.CursorCodec, contextpack.CursorBinding, *policy.Scanner, string, string, []model.MappingMatch) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	id := "11111111-1111-4111-8111-111111111111"
	gen := "22222222-2222-4222-8222-222222222222"
	h := strings.Repeat("a", 64)
	member := model.ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: gen, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: gen, RecordsIncarnation: gen}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: gen, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "mapping", PrincipalDigest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64), RequestDigest: strings.Repeat("3", 64), OrderVersion: "mapping-v1", Scope: scope}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	build, bindingID := strings.Repeat("b", 64), strings.Repeat("c", 64)
	identity := model.GraphIdentity{WorktreeID: id, ProfileID: h, GenerationID: gen}
	matches := make([]model.MappingMatch, count)
	for i := range matches {
		matches[i] = model.MappingMatch{MatchID: fmt.Sprintf("%064x", i+1), AnchorRef: model.AnchorRef{Kind: "source_anchor", GraphIdentity: identity, BuildMapID: build, AnchorID: fmt.Sprintf("%064x", count-i), BindingID: bindingID}, TargetRef: &model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: fmt.Sprintf("target-%d", i)}, SourceRefs: []model.SourceRef{}, MappingStatus: "candidate", EvidenceRefs: []model.EvidenceRef{}}
	}
	return ctx, cancel, codec, binding, scanner, build, bindingID, matches
}

func mappingAllow(context.Context, contextpack.CursorBinding) error { return nil }

func TestMappingPagesPreserve137PairsAndKeyset(t *testing.T) {
	ctx, cancel, codec, binding, scanner, build, bindingID, matches := mappingFixture(t, 137)
	defer cancel()
	pager, err := NewMappingPager(ctx, codec, binding, mappingAllow, scanner, build, bindingID, "ambiguous", matches, 1000, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, match := range matches {
		expected[match.MatchID] = match.AnchorRef.AnchorID + ":" + match.TargetRef.(*model.SymbolRef).SymbolID
	}
	seen := map[string]bool{}
	cursor := ""
	last := [3]string{}
	pages := 0
	for {
		pages++
		if pages > 100 {
			t.Fatal("mapping cursor loop")
		}
		page, err := pager.Page(ctx, cursor, binding.PrincipalDigest, mappingAllow, 20, 8192)
		if err != nil {
			t.Fatal(err)
		}
		if page.Status != "ambiguous" || page.Total != 137 || page.SerializedItemBytes > 8192 {
			t.Fatal("bad global status or item budget")
		}
		raw, err := json.Marshal(page.Matches)
		if err != nil || len(raw) != page.SerializedItemBytes {
			t.Fatal("item bytes measured incorrectly")
		}
		for _, match := range page.Matches {
			if seen[match.MatchID] || expected[match.MatchID] != match.AnchorRef.AnchorID+":"+match.TargetRef.(*model.SymbolRef).SymbolID {
				t.Fatal("mapping duplicated or pair changed")
			}
			seen[match.MatchID] = true
			digest, err := model.GraphRefDigest(match.TargetRef, 65536)
			if err != nil {
				t.Fatal(err)
			}
			key := [3]string{match.AnchorRef.AnchorID, digest, match.MatchID}
			if len(seen) > 1 && mappingKeyCompare(last, key) >= 0 {
				t.Fatal("keyset order did not advance")
			}
			last = key
		}
		if page.NextCursor == nil {
			if page.Remaining != 0 {
				t.Fatal("pending matches lost")
			}
			break
		}
		if page.Remaining <= 0 || *page.NextCursor == cursor {
			t.Fatal("invalid continuation")
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 137 || pages < 2 {
		t.Fatal("mapping results were lost")
	}
	if _, err := NewMappingPager(ctx, codec, binding, mappingAllow, scanner, build, bindingID, "exact_in_supported_scope", matches, 1000, 1024*1024); !errors.Is(err, ErrMappingPageInvalid) {
		t.Fatalf("false unique accepted: %v", err)
	}
}

func TestMappingPagesRejectTamperRevokeAndOversize(t *testing.T) {
	ctx, cancel, codec, binding, scanner, build, bindingID, matches := mappingFixture(t, 3)
	defer cancel()
	pager, err := NewMappingPager(ctx, codec, binding, mappingAllow, scanner, build, bindingID, "ambiguous", matches, 100, 65536)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pager.Page(ctx, "", binding.PrincipalDigest, mappingAllow, 1, 8192)
	if err != nil || page.NextCursor == nil {
		t.Fatalf("missing cursor: %v", err)
	}
	cursor := *page.NextCursor
	if _, err = pager.Page(ctx, cursor, strings.Repeat("f", 64), mappingAllow, 1, 8192); !errors.Is(err, ErrMappingPageInvalid) {
		t.Fatalf("wrong principal accepted: %v", err)
	}
	revoked := errors.New("revoked")
	if _, err = pager.Page(ctx, cursor, binding.PrincipalDigest, func(context.Context, contextpack.CursorBinding) error { return revoked }, 1, 8192); !errors.Is(err, revoked) {
		t.Fatalf("revocation ignored: %v", err)
	}
	if _, err = pager.Page(ctx, cursor+"x", binding.PrincipalDigest, mappingAllow, 1, 8192); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("tampered cursor accepted: %v", err)
	}
	if _, err = pager.Page(ctx, "", binding.PrincipalDigest, mappingAllow, 1, 2); !errors.Is(err, ErrMappingPageLarge) {
		t.Fatalf("oversized item accepted: %v", err)
	}
	pager.now = func() time.Time { return time.Unix(pager.expires, 0) }
	if _, err = pager.Page(ctx, cursor, binding.PrincipalDigest, mappingAllow, 1, 8192); !errors.Is(err, ErrMappingPageExpired) {
		t.Fatalf("expired materialization accepted: %v", err)
	}
	duplicate := append([]model.MappingMatch{}, matches...)
	changed := duplicate[0]
	changed.TargetRef = matches[1].TargetRef
	duplicate = append(duplicate, changed)
	if _, err = NewMappingPager(ctx, codec, binding, mappingAllow, scanner, build, bindingID, "ambiguous", duplicate, 100, 65536); !errors.Is(err, ErrMappingPageInvalid) {
		t.Fatalf("conflicting match ID accepted: %v", err)
	}
	nullTarget := append([]model.MappingMatch{}, matches...)
	nullTarget[0].TargetRef = nil
	if _, err = NewMappingPager(ctx, codec, binding, mappingAllow, scanner, build, bindingID, "ambiguous", nullTarget, 100, 65536); !errors.Is(err, ErrMappingPageInvalid) {
		t.Fatalf("unkeyable target accepted: %v", err)
	}
}

func TestMappingUniqueTargetCanHaveManyAnchors(t *testing.T) {
	ctx, cancel, codec, binding, scanner, build, bindingID, matches := mappingFixture(t, 3)
	defer cancel()
	for i := range matches {
		matches[i].TargetRef = matches[0].TargetRef
		matches[i].MappingStatus = "exact_in_supported_scope"
	}
	pager, err := NewMappingPager(ctx, codec, binding, mappingAllow, scanner, build, bindingID, "exact_in_supported_scope", matches, 100, 65536)
	if err != nil {
		t.Fatalf("one target with multiple anchors rejected: %v", err)
	}
	page, err := pager.Page(ctx, "", binding.PrincipalDigest, mappingAllow, 2, 8192)
	if err != nil || page.Status != "exact_in_supported_scope" || len(page.Matches) != 2 {
		t.Fatalf("bad exact page: %v", err)
	}
}
