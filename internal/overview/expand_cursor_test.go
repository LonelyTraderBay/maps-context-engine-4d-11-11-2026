package overview

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func groupFixture(t *testing.T, count int) (context.Context, *GroupPager, map[string]bool) {
	t.Helper()
	ctx, in := overviewFixture(t, count)
	report, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	moduleID := ""
	for _, m := range summary.Modules {
		if m.Root == "nested" {
			moduleID = m.Ref.ModuleID
		}
	}
	var selected *Group
	for i := range summary.Groups {
		if summary.Groups[i].SourceModuleID == moduleID {
			selected = &summary.Groups[i]
			break
		}
	}
	if selected == nil {
		t.Fatal("fixture missing nested group")
	}
	m := in.Snapshot.Manifest()
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: m.ProfileID, GenerationID: generation, SnapshotDigest: m.SnapshotDigest, PolicyDigest: m.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	var key [32]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "overview-group-v1", Scope: scope}
	pager, err := NewGroupPager(ctx, report, selected.Expand, codec, binding, func(context.Context, contextpack.CursorBinding) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{}
	for _, rel := range in.Relations {
		if rel.SitePath == "nested/use.go" {
			expected[rel.RelationID] = true
		}
	}
	return ctx, pager, expected
}

func TestGroupCursorPreservesEverySharedConsumer(t *testing.T) {
	ctx, pager, expected := groupFixture(t, 3001)
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	lastPending := len(expected) + 1
	for {
		page, err := pager.Page(ctx, cursor, pager.binding.PrincipalDigest, func(context.Context, contextpack.CursorBinding) error { return nil }, 80, 8192)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > 8192 {
			t.Fatal("page exceeds budget")
		}
		if len(page.Sites) == 0 || page.Pending >= lastPending {
			t.Fatal("cursor did not advance")
		}
		lastPending = page.Pending
		pages++
		for _, site := range page.Sites {
			id := site.Relation.RelationID
			if !expected[id] || seen[id] || site.Source.Path != "nested/use.go" {
				t.Fatalf("wrong or repeated site %s", id)
			}
			seen[id] = true
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
		if pages > len(expected) {
			t.Fatal("unbounded pagination")
		}
	}
	if len(seen) != len(expected) || pages < 2 {
		t.Fatalf("lost sites %d/%d", len(seen), len(expected))
	}
}

func TestGroupCursorRejectsTamperRevocationAndExpiry(t *testing.T) {
	ctx, pager, _ := groupFixture(t, 60)
	allow := func(context.Context, contextpack.CursorBinding) error { return nil }
	page, err := pager.Page(ctx, "", pager.binding.PrincipalDigest, allow, 2, 8192)
	if err != nil || page.NextCursor == nil {
		t.Fatal("missing initial cursor")
	}
	token := *page.NextCursor
	if _, err = pager.Page(ctx, token+"x", pager.binding.PrincipalDigest, allow, 2, 8192); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	if _, err = pager.Page(ctx, token, strings.Repeat("f", 64), allow, 2, 8192); err == nil {
		t.Fatal("wrong principal accepted")
	}
	denied := func(context.Context, contextpack.CursorBinding) error { return errors.New("revoked") }
	if got, err := pager.Page(ctx, token, pager.binding.PrincipalDigest, denied, 2, 8192); err == nil || len(got.Sites) != 0 {
		t.Fatal("revoked cursor leaked")
	}
	pager.expires = 0
	if _, err = pager.Page(ctx, token, pager.binding.PrincipalDigest, allow, 2, 8192); !errors.Is(err, contextpack.ErrCursorExpired) {
		t.Fatal("expired group accepted")
	}
}
