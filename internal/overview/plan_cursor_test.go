package overview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestPlanPagerRejectsRevokedOverviewSource(t *testing.T) {
	ctx, in := planFixture(t, true)
	allowed := true
	in.Overview.authorizePath = func(_ context.Context, _ Scope, name string) error {
		if !allowed && name == "main.go" {
			return errors.New("source authority revoked")
		}
		return nil
	}
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	manifest := in.Snapshot.Manifest()
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec([32]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "plan-v1", Scope: scope}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	pager, err := NewPlanPager(ctx, plan, codec, binding, guard)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pager.Page(ctx, "", binding.PrincipalDigest, guard, 4096)
	if err != nil || page.NextCursor == nil {
		t.Fatalf("first page did not provide a cursor: %v", err)
	}
	allowed = false
	if page, err := pager.Page(ctx, *page.NextCursor, binding.PrincipalDigest, guard, 4096); err == nil || len(page.Items) != 0 {
		t.Fatal("revoked overview source leaked a continuation")
	}
}

func TestPlanCursorFragmentsMandatoryUnicodeEvidence(t *testing.T) {
	content := strings.Repeat("ພາສາ😀\"\n", 1200)
	ctx, in := planFixtureExtra(t, false, content)
	file, _ := in.Snapshot.File("spec.md")
	whole := file.Bytes()
	start := bytes.Index(whole, []byte(content))
	if start < 0 {
		t.Fatal("missing fixture content")
	}
	ref := in.Declarations[0].Source
	ref.Range = model.ByteRange{StartByte: int64(start), EndByte: int64(start + len(content))}
	in.Declarations = []DocumentDeclaration{{Kind: "component", Source: ref}}
	in.Intent = "Plan the declared component"
	in.Constraints = []string{"no_source_mutation", "preserve_json_keys"}
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
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
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "plan-v1", Scope: scope}
	allow := func(context.Context, contextpack.CursorBinding) error { return nil }
	pager, err := NewPlanPager(ctx, plan, codec, binding, allow)
	if err != nil {
		t.Fatal(err)
	}
	selector, err := plan.Selector()
	if err != nil {
		t.Fatal(err)
	}
	selector.(*model.WorkspaceSelector).Kind = "changed"
	selector, err = plan.Selector()
	if err != nil || selector.(*model.WorkspaceSelector).Kind != "workspace" {
		t.Fatal("plan selector was not copied")
	}
	weight, err := pager.MaterializedBytes(32 * 1024 * 1024)
	if err != nil || weight < int64(len(whole)) {
		t.Fatalf("retained materialization was not charged: %d %v", weight, err)
	}
	if _, err := pager.MaterializedBytes(weight - 1); !errors.Is(err, ErrLimit) {
		t.Fatalf("materialization exceeded limit: %v", err)
	}
	var reconstructed strings.Builder
	cursor := ""
	pages := 0
	expectedStart := ref.Range.StartByte
	firstToken := ""
	for {
		page, err := pager.Page(ctx, cursor, binding.PrincipalDigest, allow, 4096)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page)
		if len(raw) > 4096 || len(page.Items) != 1 || page.ReviewStatus != "not_checked" || page.ValidationStatus != "not_checked" || page.Intent != in.Intent || len(page.Constraints) != 2 {
			t.Fatal("missing plan metadata or budget exceeded")
		}
		item := page.Items[0]
		if !item.RequiredForReview || item.Plane != "intended" || item.MappingStatus != "unmapped" || item.Source.Range.StartByte != expectedStart || item.FragmentOf == nil {
			t.Fatal("invalid fragment metadata")
		}
		if item.Source.Range.EndByte < ref.Range.EndByte && !strings.HasSuffix(item.Content, "\n") {
			t.Fatal("fragment ignored an available newline boundary")
		}
		if err = model.ValidateSourceFragment(*item.FragmentOf, item.Source, item.Content, item.ID, whole, 65536); err != nil {
			t.Fatal(err)
		}
		expectedStart = item.Source.Range.EndByte
		reconstructed.WriteString(item.Content)
		pages++
		if page.NextCursor == nil {
			if page.RequiredPending != 0 || page.PendingItems != 0 {
				t.Fatal("final pending not zero")
			}
			break
		}
		if page.RequiredPending != 1 || page.PendingItems != 1 {
			t.Fatal("partial mandatory item not retained")
		}
		cursor = *page.NextCursor
		if firstToken == "" {
			firstToken = cursor
		}
		if pages > 1000 {
			t.Fatal("cursor failed to advance")
		}
	}
	if reconstructed.String() != content || expectedStart != ref.Range.EndByte || pages < 2 {
		t.Fatal("fragments did not reconstruct original bytes")
	}
	denied := func(context.Context, contextpack.CursorBinding) error { return errors.New("revoked") }
	if page, err := pager.Page(ctx, firstToken, binding.PrincipalDigest, denied, 4096); err == nil || len(page.Items) != 0 {
		t.Fatal("revoked continuation leaked")
	}
}

func TestPlanPagerOversizedMetadataDescriptor(t *testing.T) {
	ctx, in := planFixture(t, false)
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	source := in.Declarations[0].Source
	// Exercise an indivisible derived item without depending on host path limits.
	plan.report.Items = []PlanItem{{ID: "structure-" + strings.Repeat("a", 64), Kind: "structure_module", Plane: "observed_static", MappingStatus: "not_applicable", Source: source, Content: strings.Repeat("x", 6000)}, {ID: "structure-" + strings.Repeat("b", 64), Kind: "structure_file", Plane: "observed_static", MappingStatus: "not_applicable", Source: source, Content: strings.Repeat("y", 6000)}}
	manifest := in.Snapshot.Manifest()
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "plan-v1", Scope: scope}
	allow := func(context.Context, contextpack.CursorBinding) error { return nil }
	pager, err := NewPlanPager(ctx, plan, codec, binding, allow)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := pager.Page(ctx, "", binding.PrincipalDigest, allow, 4096)
	var descriptor *contextpack.OversizedItemError
	if !errors.Is(err, contextpack.ErrItemTooLarge) || !errors.As(err, &descriptor) || descriptor.ItemID != plan.report.Items[0].ID || len(descriptor.Sources) != 1 || descriptor.Sources[0] != source {
		t.Fatalf("missing complete metadata descriptor: %v", err)
	}
	if len(pending.Items) != 0 || pending.PendingItems != 2 || pending.NextPosition.NextItem != 0 || pending.NextCursor != nil || len(pending.Gaps) != len(plan.report.Gaps) {
		t.Fatal("oversized metadata consumed an item or hid pending gaps")
	}
	if err := pager.VerifyOversizedItem(ctx, "", binding.PrincipalDigest, allow, descriptor); err != nil {
		t.Fatalf("valid plan descriptor rejected: %v", err)
	}
	forged := *descriptor
	forged.Sources = append([]model.SourceRef{}, descriptor.Sources...)
	forged.Sources[0].Range.EndByte++
	if err := pager.VerifyOversizedItem(ctx, "", binding.PrincipalDigest, allow, &forged); !errors.Is(err, ErrAuthority) {
		t.Fatalf("forged descriptor was accepted: %v", err)
	}
	cursor, err := codec.Issue(ctx, pager.binding, contextpack.SignedPosition{NextItem: 1, LastKey: []string{}}, allow)
	if err != nil {
		t.Fatal(err)
	}
	second := &contextpack.OversizedItemError{ItemID: plan.report.Items[1].ID, Sources: []model.SourceRef{source}}
	if err := pager.VerifyOversizedItem(ctx, cursor, binding.PrincipalDigest, allow, second); err != nil {
		t.Fatalf("cursor-bound plan descriptor rejected: %v", err)
	}
	if err := pager.VerifyOversizedItem(ctx, cursor, binding.PrincipalDigest, allow, descriptor); !errors.Is(err, ErrAuthority) {
		t.Fatalf("old plan descriptor accepted for another cursor: %v", err)
	}
	denied := func(context.Context, contextpack.CursorBinding) error { return ErrAuthority }
	if err := pager.VerifyOversizedItem(ctx, "", binding.PrincipalDigest, denied, descriptor); err == nil {
		t.Fatal("revoked descriptor was accepted")
	}
}
