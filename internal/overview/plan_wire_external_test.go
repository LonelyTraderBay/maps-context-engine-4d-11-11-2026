package overview_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestPlanWireReconstructsLongUnicodeSourceWithinDualBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "33333333-3333-4333-8333-333333333333"
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 65536)
	if err != nil {
		t.Fatal(err)
	}
	config := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/fixture"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{"spec.md"}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 65536, MaxSourceBytes: 1048576, MaxOutputBytes: 65536, SecretRulesVersion: "v1"}
	policyID, err := model.PolicyDigest(config, 65536)
	if err != nil {
		t.Fatal(err)
	}
	// One long line exercises UTF-8 fallback when there is no newline boundary.
	content := strings.Repeat("ພາສາ😀\"\\", 1200)
	file, err := snapshot.NewFile("spec.md", []byte(content), 65536)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profileID, PolicyDigest: policyID, ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{{Path: "spec.md", Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 65536, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	report, err := overview.Build(ctx, overview.Input{Snapshot: captured, Profile: profile, GenerationID: generation, PolicyEpoch: 1, Scanner: scanner, MaxManifestBytes: 65536, MaxSourceBytes: 1048576, MaxFacts: 10000, MaxOutputBytes: 1048576, AuthorizeScope: func(context.Context, overview.Scope) error { return nil }, AuthorizePath: func(context.Context, overview.Scope, string) error { return nil }, VerifyFacts: func(context.Context, overview.Scope, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, Path: "spec.md", ContentHash: hash, Range: model.ByteRange{StartByte: 0, EndByte: length}}
	plan, err := overview.BuildPlan(ctx, overview.PlanInput{Overview: report, Snapshot: captured, Policy: config, Selector: &model.WorkspaceSelector{Kind: "workspace"}, Declarations: []overview.DocumentDeclaration{{Kind: "constraint", Source: ref}}, Scanner: scanner, AuthorizeDocument: func(context.Context, overview.Scope, overview.DocumentDeclaration) error { return nil }, MaxBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: policyID, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","max_output_bytes":16384}`)
	request, err := model.DecodePackRequestJSON(raw, 65536)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := model.RequestDigest("context_pack", "fixture", "PackRequest", request, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	binding := contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: digest, OrderVersion: "pack-v1", Scope: scope}
	pack, token, err := app.NewFreshnessPlan(ctx, app.FreshnessPlanInput{Plan: plan, Codec: codec, Binding: binding, Guard: guard, RequestJSON: raw, PrincipalID: "fixture", Coverage: model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}, Scanner: scanner, Validation: app.ValidateCapturedInput{Scope: scope, MaxInputBytes: 65536, MaxManifestBytes: 65536}, RequestLimit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	options := contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "mcp_dual"}
	page, err := pack.First(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	var reconstructed strings.Builder
	start, fragments := int64(0), 0
	for pages := 0; ; pages++ {
		if len(page.Blocks) != 2 || string(page.Blocks[0]) != string(page.Blocks[1]) || model.ValidateDeliveredBytes(page.Response.Budget, page.Blocks) != nil {
			t.Fatal("dual delivery exceeded its actual budget")
		}
		for _, item := range page.Response.Data.(model.PackData).Items {
			if item.WhyIncluded == "plan_summary" {
				continue
			}
			if item.FragmentOf == nil || len(item.SourceRefs) != 1 || item.Content == nil || item.SourceRefs[0].Range.StartByte != start {
				t.Fatal("invalid source fragment")
			}
			if err := model.ValidateSourceFragment(*item.FragmentOf, item.SourceRefs[0], *item.Content, item.ItemID, file.Bytes(), 65536); err != nil {
				t.Fatal(err)
			}
			start = item.SourceRefs[0].Range.EndByte
			reconstructed.WriteString(*item.Content)
			fragments++
		}
		if page.Response.Pagination.NextCursor == nil {
			break
		}
		if pages >= 100 || page.Response.Pagination.RequiredPending == nil || *page.Response.Pagination.RequiredPending < 1 {
			t.Fatal("mandatory fragment lost its continuation")
		}
		page, err = pack.Continue(ctx, token, *page.Response.Pagination.NextCursor, binding.PrincipalDigest, guard, options)
		if err != nil {
			t.Fatal(err)
		}
	}
	if reconstructed.String() != content || start != length || fragments < 2 {
		t.Fatal("source fragments did not reconstruct the captured bytes")
	}
	if _, err := json.Marshal(page.Response); err != nil {
		t.Fatal(err)
	}
	checkedRaw := []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","freshness_mode":"checked_workspace","max_output_bytes":16384}`)
	checkedRequest, err := model.DecodePackRequestJSON(checkedRaw, 65536)
	if err != nil {
		t.Fatal(err)
	}
	checkedBinding := binding
	checkedBinding.RequestDigest, err = model.RequestDigest("context_pack", "fixture", "PackRequest", checkedRequest, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	validation := app.ValidateCapturedInput{Scope: scope, MaxInputBytes: 65536, MaxManifestBytes: 65536,
		CurrentScope:    func(context.Context) (model.ScopeVector, error) { return scope, nil },
		ResolveCapture:  func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error) { return captured, nil },
		AuthorizeSource: func(context.Context, model.ScopeMember, model.SourceRef) error { return nil },
		VerifyWorkspace: func(context.Context, model.ScopeMember, *snapshot.Snapshot) (app.WorkspaceObservation, error) {
			return app.WorkspaceObservedCurrent, nil
		},
	}
	for width := 100; width <= 900; width += 17 {
		coverage := model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}
		for i := 0; i < 12; i++ {
			coverage.Gaps = append(coverage.Gaps, model.Gap{Code: fmt.Sprintf("FIXTURE_%02d", i), Reason: strings.Repeat("x", width), SourceRefs: []model.SourceRef{}})
		}
		checked, checkedToken, err := app.NewFreshnessPlan(ctx, app.FreshnessPlanInput{Plan: plan, Codec: codec, Binding: checkedBinding, Guard: guard, RequestJSON: checkedRaw, PrincipalID: "fixture", Coverage: coverage, Scanner: scanner, Validation: validation, CaptureLive: func(context.Context, model.ScopeMember, string) (snapshot.File, error) { return file, nil }, RequestLimit: 65536})
		if err != nil {
			t.Fatal(err)
		}
		first, err := checked.First(ctx, options)
		if err != nil {
			t.Fatalf("checked workspace could not fit grouped safety details (width %d): %v", width, err)
		}
		if first.Response.Freshness.Status != "observed_current" || model.ValidateDeliveredBytes(first.Response.Budget, first.Blocks) != nil {
			t.Fatalf("checked workspace exceeded the dual budget (width %d)", width)
		}
		if width == 202 {
			for pages := 0; first.Response.Pagination.NextCursor != nil; pages++ {
				if pages >= 100 {
					t.Fatal("checked continuation failed to terminate")
				}
				first, err = checked.Continue(ctx, checkedToken, *first.Response.Pagination.NextCursor, binding.PrincipalDigest, guard, options)
				if err != nil || model.ValidateDeliveredBytes(first.Response.Budget, first.Blocks) != nil {
					t.Fatalf("checked continuation exceeded its budget: %v", err)
				}
			}
		}
	}
}
