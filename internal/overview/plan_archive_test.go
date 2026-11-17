package overview

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestPlanArchiveRestoresPinnedMaterialization(t *testing.T) {
	const canary = "UNSELECTED_FIXTURE_SOURCE_MUST_NOT_BE_ARCHIVED"
	ctx, in := planFixtureWithFiles(t, true, "", map[string]string{"unrelated.txt": canary})
	allowed := true
	in.Overview.authorizePath = func(_ context.Context, _ Scope, name string) error {
		if !allowed && name == "main.go" {
			return ErrAuthority
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
	codec, err := contextpack.NewCursorCodec([32]byte{7})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "pack-v1", Scope: scope}
	pager, err := NewPlanPager(ctx, plan, codec, binding, guard)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := codec.VerifyPrincipal(ctx, pager.Anchor(), "list", binding.PrincipalDigest, guard)
	if err != nil {
		t.Fatal(err)
	}
	packBinding := claims.Binding
	packBinding.Purpose = "pack"
	pack, err := codec.Issue(ctx, packBinding, contextpack.SignedPosition{LastKey: []string{}}, guard)
	if err != nil {
		t.Fatal(err)
	}
	first, err := pager.Page(ctx, "", binding.PrincipalDigest, guard, 4096)
	if err != nil || first.NextCursor == nil {
		t.Fatalf("fixture has no continuation: %v", err)
	}
	before, err := pager.Page(ctx, *first.NextCursor, binding.PrincipalDigest, guard, 4096)
	if err != nil {
		t.Fatal(err)
	}
	metadata := []byte("bound fixture request")
	raw, err := pager.ArchiveForRetention(ctx, pack, 30, metadata, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	materialization, err := codec.OpenPlanMaterialization(ctx, raw, pack, binding.PrincipalDigest, 1024*1024, guard)
	if err != nil || bytes.Contains(materialization.Payload, []byte(canary)) {
		t.Fatal("unselected snapshot source was included in the plan archive")
	}
	currentDeclarations := append([]DocumentDeclaration{}, in.Declarations...)
	verifyDeclarations := func(_ context.Context, _ Scope, got []DocumentDeclaration) error {
		if !reflect.DeepEqual(got, currentDeclarations) {
			return ErrAuthority
		}
		return nil
	}
	restoreInput := PlanRestoreInput{Codec: codec, PackToken: pack, PrincipalDigest: binding.PrincipalDigest, Guard: guard, Snapshot: in.Snapshot,
		AuthorizeScope: in.Overview.authorizeScope, AuthorizePath: in.Overview.authorizePath, VerifyFacts: in.Overview.verifyFacts,
		AuthorizeDocument: in.AuthorizeDocument, VerifyDeclarations: verifyDeclarations, MaxArchiveBytes: 1024 * 1024}
	restored, err := RestorePlanPager(ctx, raw, restoreInput)
	if err != nil || !bytes.Equal(restored.Metadata, metadata) || restored.Pager.Anchor() != pager.Anchor() {
		t.Fatalf("restore failed: %v", err)
	}
	missingSetProof := restoreInput
	missingSetProof.VerifyDeclarations = nil
	if _, err := RestorePlanPager(ctx, raw, missingSetProof); err == nil {
		t.Fatal("plan restored without a complete declaration-set proof")
	}
	originalReport, err := plan.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restoredReport, err := restored.Plan.Read(ctx)
	if err != nil || !reflect.DeepEqual(originalReport, restoredReport) {
		t.Fatalf("restored report differs: %v", err)
	}
	after, err := restored.Pager.Page(ctx, *first.NextCursor, binding.PrincipalDigest, guard, 4096)
	if err != nil {
		t.Fatal(err)
	}
	before.NextCursor, after.NextCursor = nil, nil
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restored continuation changed the original plan")
	}
	rearchived, err := restored.Pager.ArchiveForRetention(ctx, pack, 900, metadata, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	rearchivedInfo, err := codec.InspectPlanMaterialization(rearchived, 1024*1024)
	if err != nil || rearchivedInfo.ExpiresAt != restored.ExpiresAt {
		t.Fatal("rearchive extended the original retention TTL")
	}
	tampered := bytes.Clone(raw)
	tampered[len(tampered)-1] ^= 1
	if _, err := RestorePlanPager(ctx, tampered, restoreInput); err == nil {
		t.Fatal("tampered archive was restored")
	}
	_, foreign := planFixtureExtra(t, true, "changed snapshot")
	wrong := restoreInput
	wrong.Snapshot = foreign.Snapshot
	if _, err := RestorePlanPager(ctx, raw, wrong); err == nil {
		t.Fatal("different captured snapshot was accepted")
	}
	currentDeclarations = append(currentDeclarations, currentDeclarations[0])
	if report, err := restored.Plan.Read(ctx); err == nil || len(report.Items) != 0 {
		t.Fatal("changed declaration set leaked a restored report")
	}
	currentDeclarations = currentDeclarations[:len(currentDeclarations)-1]
	lateChange := false
	restoreInput.AuthorizeDocument = func(context.Context, Scope, DocumentDeclaration) error {
		if lateChange {
			currentDeclarations = append(currentDeclarations, currentDeclarations[0])
			lateChange = false
		}
		return nil
	}
	late, err := RestorePlanPager(ctx, raw, restoreInput)
	if err != nil {
		t.Fatal(err)
	}
	lateChange = true
	if report, err := late.Plan.Read(ctx); err == nil || len(report.Items) != 0 {
		t.Fatal("late declaration set change leaked a restored report")
	}
	currentDeclarations = currentDeclarations[:len(currentDeclarations)-1]
	allowed = false
	if report, err := restored.Plan.Read(ctx); err == nil || len(report.Items) != 0 {
		t.Fatal("revoked source leaked a restored report")
	}
	if page, err := restored.Pager.Page(ctx, *first.NextCursor, binding.PrincipalDigest, guard, 4096); err == nil || len(page.Items) != 0 {
		t.Fatal("revoked source leaked a restored continuation")
	}
}
