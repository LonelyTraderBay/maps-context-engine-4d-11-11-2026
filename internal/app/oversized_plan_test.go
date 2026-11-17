package app

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
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestOversizedPlanKeepsGapsAndDeliversDescriptor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "22222222-2222-4222-8222-222222222222"
	name := strings.Repeat("d/", 600) + "go.mod"
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{strings.TrimSuffix(name, "/go.mod")}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 65536)
	if err != nil {
		t.Fatal(err)
	}
	config := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/fixture"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 65536, MaxSourceBytes: 1048576, MaxOutputBytes: 65536, SecretRulesVersion: "fixture"}
	policyID, err := model.PolicyDigest(config, 65536)
	if err != nil {
		t.Fatal(err)
	}
	file, err := snapshot.NewFile(name, []byte("module example.invalid/large\n\ngo 1.27\n"), 65536)
	if err != nil {
		t.Fatal(err)
	}
	hash, size := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profileID, PolicyDigest: policyID, ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 65536, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	view, err := overview.Build(ctx, overview.Input{Snapshot: captured, Profile: profile, GenerationID: generation, PolicyEpoch: 1, Scanner: scanner, MaxManifestBytes: 65536, MaxSourceBytes: 1048576, MaxFacts: 10000, MaxOutputBytes: 1048576,
		AuthorizeScope: func(context.Context, overview.Scope) error { return nil }, AuthorizePath: func(context.Context, overview.Scope, string) error { return nil }, VerifyFacts: func(context.Context, overview.Scope, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := overview.BuildPlan(ctx, overview.PlanInput{Overview: view, Snapshot: captured, Policy: config, Selector: &model.WorkspaceSelector{Kind: "workspace"}, Declarations: []overview.DocumentDeclaration{}, Scanner: scanner, AuthorizeDocument: func(context.Context, overview.Scope, overview.DocumentDeclaration) error { return nil }, MaxBytes: 1048576})
	if err != nil {
		t.Fatal(err)
	}
	planReport, err := plan.Read(ctx)
	if err != nil || len(planReport.Modules) != 1 {
		t.Fatalf("large metadata fixture has no module: %v", err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: policyID, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan"}`)
	request, err := model.DecodePackRequestJSON(raw, 65536)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := model.RequestDigest("context_pack", "fixture-actor", "PackRequest", request, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64), RequestDigest: digest, OrderVersion: "pack-v1", Scope: scope}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	coverage := model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}
	for i := 0; i < 50; i++ {
		coverage.Gaps = append(coverage.Gaps, model.Gap{Code: fmt.Sprintf("GAP_%02d", i), Reason: strings.Repeat("x", 400), SourceRefs: []model.SourceRef{}})
	}
	pack, token, err := NewFreshnessPlan(ctx, FreshnessPlanInput{Plan: plan, Codec: codec, Binding: binding, Guard: guard, RequestJSON: raw, PrincipalID: "fixture-actor", Coverage: coverage, Scanner: scanner, Validation: ValidateCapturedInput{Scope: scope, MaxInputBytes: 65536, MaxManifestBytes: 65536}, RequestLimit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	options := contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "mcp_dual"}
	if len(planReport.Items) < 2 || planReport.Items[1].Kind != "structure_module" {
		t.Fatal("fixture needs a metadata item after the module source")
	}
	// A test-only signed position exercises pending gaps at the metadata item.
	synthetic, err := codec.Issue(ctx, pack.listBinding, contextpack.SignedPosition{NextItem: 1, LastKey: []string{}}, guard)
	if err != nil {
		t.Fatal(err)
	}
	gapPage, err := pack.Continue(ctx, token, synthetic, binding.PrincipalDigest, guard, options)
	if err != nil || gapPage.Response.Pagination.NextCursor == nil || len(gapPage.Response.Data.(model.PackData).Items) != 1 {
		t.Fatalf("pending safety gaps did not advance at oversized metadata: %v", err)
	}
	gapClaims, err := codec.VerifyPrincipal(ctx, *gapPage.Response.Pagination.NextCursor, "list", binding.PrincipalDigest, guard)
	if err != nil || gapClaims.Position.NextItem != 1 || gapClaims.Position.NextCoverageGap <= 0 {
		t.Fatalf("gap-only cursor consumed the oversized item: %v", err)
	}
	page, err := pack.First(ctx, options)
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; err == nil && pages < 50; pages++ {
		for _, gap := range page.Response.Coverage.Gaps {
			if gap.Code != "GAP_DETAILS_PENDING" {
				if seen[gap.Code] {
					t.Fatalf("safety gap repeated: %s", gap.Code)
				}
				seen[gap.Code] = true
			}
		}
		if page.Response.Pagination.NextCursor == nil {
			t.Fatal("oversized item disappeared without an error")
		}
		cursor = *page.Response.Pagination.NextCursor
		page, err = pack.Continue(ctx, token, cursor, binding.PrincipalDigest, guard, options)
	}
	var proof *oversizedEvidenceError
	if !errors.As(err, &proof) || len(seen) != len(coverage.Gaps) {
		position := int64(-1)
		kind := "unknown"
		if cursor != "" {
			if claims, verifyErr := codec.VerifyPrincipal(ctx, cursor, "list", binding.PrincipalDigest, guard); verifyErr == nil {
				position = claims.Position.NextItem
				if position >= 0 && position < int64(len(planReport.Items)) {
					kind = planReport.Items[position].Kind
				}
			}
		}
		t.Fatalf("oversized plan lost safety gaps: gaps=%d position=%d kind=%s err=%v", len(seen), position, kind, err)
	}
	service, _, backend, _ := coreServiceFixture(t)
	backend.err = proof
	expand, err := json.Marshal(model.ExpandRequest{PackToken: token, NextCursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Call(ctx, "mce4d_expand_pack", expand, coreTestOptions())
	if err != nil || !result.IsError {
		t.Fatalf("oversized plan did not reach service: %v", err)
	}
	response := decodeCoreResult(t, "mce4d_expand_pack", result)
	if response.Error.Code != "RESULT_TOO_LARGE" || len(response.Error.Details) != 1 || len(response.Error.Details[0].SourceRefs) != 1 || response.Error.Details[0].SourceRefs[0].Path != name || response.Pagination.NextCursor != nil {
		t.Fatal("oversized plan did not deliver the exact source descriptor")
	}
}
