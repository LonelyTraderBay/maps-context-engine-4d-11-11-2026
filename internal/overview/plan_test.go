package overview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const fixturePlanDocument = "Objective: serve users\nComponent: PaymentService\nConstraint: preserve keys\nAlias: số dư khả dụng\nAlias: ຍອດເງິນ\n"

func TestPlanCanonicalRecipesAndTiedOrder(t *testing.T) {
	ref := model.SourceRef{WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64), GenerationID: "33333333-3333-4333-8333-333333333333", Path: "spec.md", ContentHash: strings.Repeat("b", 64), Range: model.ByteRange{StartByte: 0, EndByte: 12}}
	declaration := struct {
		Kind     string          `json:"kind"`
		Source   model.SourceRef `json:"source"`
		EntityID string          `json:"entity_id"`
	}{"constraint", ref, ""}
	if got, err := planCanonicalDigest("plan-declaration-v1", declaration, 32768); err != nil || got != "f083f1f7d2cf57975212b8d489ee151e44c340f6c06534fe5b7c8247cd7b1bb1" {
		t.Fatalf("declaration recipe differs from Python reference: %s %v", got, err)
	}
	if got, err := planCanonicalDigest("plan-observed-module-v1", ref, 32768); err != nil || got != "1e9a9295fc2ceefb6c4a17e1f085686ff926214311b9327f8c864586d58f5460" {
		t.Fatalf("module recipe differs from Python reference: %s %v", got, err)
	}
	moduleSource := ref
	moduleSource.Path = "go.mod"
	module := model.ModuleRef{Kind: "module", GraphIdentity: model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}, ModuleID: "fixture"}
	structure := PlanModuleStructure{Module: module, ParentID: "", Root: ".", ImportPath: "example.invalid/fixture", ModuleSource: moduleSource}
	if got, err := planCanonicalDigest("plan-structure-v1", structure, 32768); err != nil || got != "3a82f41f2423f34a6969a4f6c86fd77b9734aad0494eb6aa5de74f4bd22efb30" {
		t.Fatalf("structure recipe differs from Python reference: %s %v", got, err)
	}
	ctx, input := planFixture(t, false)
	a, b := input.Declarations[0], input.Declarations[0]
	a.EntityID, b.EntityID = "billing", "payments"
	input.Declarations = []DocumentDeclaration{a, b}
	first, err := BuildPlan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Declarations = []DocumentDeclaration{b, a}
	second, err := BuildPlan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	x, err := first.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	y, err := second.Read(ctx)
	if err != nil || len(x.Items) != 2 || len(y.Items) != 2 || x.Items[0].ID != y.Items[0].ID || x.Items[1].ID != y.Items[1].ID || x.Items[0].ID == x.Items[1].ID {
		t.Fatalf("tied plan items changed order: %v", err)
	}
	left, err := planCanonicalDigest("document-plan-v1", x, 65536)
	if err != nil {
		t.Fatal(err)
	}
	right, err := planCanonicalDigest("document-plan-v1", y, 65536)
	if err != nil || left != right {
		t.Fatalf("plan digest changed with input order: %v", err)
	}
}

func TestPlanStructureScanningAndBoundedMaterialization(t *testing.T) {
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planStructureItem(scanner, "structure_module", model.SourceRef{}, struct{}{}, `api_key="TESTONLY`+strings.Repeat("X", 16)+`"`); !errors.Is(err, policy.ErrSecretBlocked) {
		t.Fatalf("sensitive decoded metadata was accepted: %v", err)
	}
	ctx, input := planFixture(t, true)
	declaration := input.Declarations[1]
	input.Declarations = nil
	for i := 0; i < 30; i++ {
		declaration.EntityID = fmt.Sprintf("component-%02d", i)
		input.Declarations = append(input.Declarations, declaration)
	}
	input.MaxBytes = 4096
	if plan, err := BuildPlan(ctx, input); !errors.Is(err, ErrLimit) || plan != nil {
		t.Fatalf("oversized materialization was accepted: %v", err)
	}
}

func TestExactDocumentKeepsGlobalRuleWithoutInventingSelection(t *testing.T) {
	ctx, input := planFixture(t, true)
	selected := input.Declarations[0]
	file, ok := input.Snapshot.File(selected.Source.Path)
	if !ok {
		t.Fatal("fixture document missing")
	}
	whole := selected.Source
	whole.Range = model.ByteRange{StartByte: 0, EndByte: int64(file.Len())}
	input.Declarations = []DocumentDeclaration{selected, {Kind: "constraint", Source: whole}}
	input.Selector = &model.DocumentSelector{Kind: "document_anchor", Path: selected.Source.Path, ContentHash: selected.Source.ContentHash, Range: selected.Source.Range}
	plan, err := BuildPlan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	report, err := plan.Read(ctx)
	if err != nil || len(report.Items) != 2 || report.Items[0].Source != whole || report.Items[1].Source != selected.Source {
		t.Fatalf("exact document lost its global trusted rule: %v", err)
	}
	missing := *input.Selector.(*model.DocumentSelector)
	missing.Range.StartByte++
	input.Selector = &missing
	if plan, err := BuildPlan(ctx, input); !errors.Is(err, ErrInput) || plan != nil {
		t.Fatalf("global rule falsely satisfied a missing document selector: %v", err)
	}
}

func planFixture(t *testing.T, mixed bool) (context.Context, PlanInput) {
	return planFixtureExtra(t, mixed, "")
}

func planFixtureExtra(t *testing.T, mixed bool, extra string) (context.Context, PlanInput) {
	return planFixtureWithFiles(t, mixed, extra, nil)
}

func planFixtureWithFiles(t *testing.T, mixed bool, extra string, additions map[string]string) (context.Context, PlanInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 65536)
	if err != nil {
		t.Fatal(err)
	}
	config := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/fixture"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{"spec.md"}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 65536, MaxSourceBytes: 1048576, MaxOutputBytes: 65536, SecretRulesVersion: "v1"}
	if _, ok := additions["vocabulary.json"]; ok {
		config.TrustedRulePaths = append(config.TrustedRulePaths, "vocabulary.json")
	}
	policyID, err := model.PolicyDigest(config, 65536)
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{"spec.md": fixturePlanDocument + extra}
	for path, body := range additions {
		contents[path] = body
	}
	if mixed {
		contents["go.mod"] = "module example.test/fixture\n\ngo 1.27.4\n"
		contents["main.go"] = "package fixture\nfunc Existing() {}\n"
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profileID, PolicyDigest: policyID, ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	files := []snapshot.File{}
	for name, body := range contents {
		f, err := snapshot.NewFile(name, []byte(body), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := f.ContentHash(), int64(f.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size})
		files = append(files, f)
	}
	captured, err := snapshot.Freeze(manifest, files, 65536, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Snapshot: captured, Profile: profile, GenerationID: generation, PolicyEpoch: 1, Scanner: scanner, MaxManifestBytes: 65536, MaxSourceBytes: 1048576, MaxFacts: 10000, MaxOutputBytes: 1048576, AuthorizeScope: func(context.Context, Scope) error { return nil }, AuthorizePath: func(context.Context, Scope, string) error { return nil }, VerifyFacts: func(context.Context, Scope, string) error { return nil }}
	if mixed {
		moduleID, err := GoModuleID(worktree, "go.mod")
		if err != nil {
			t.Fatal(err)
		}
		input.Packages = []PackageInput{{Fact: model.PackageFact{PackageKey: "fixture", ImportPath: "example.test/fixture", ModuleKey: moduleID, Variant: "production", InputDigest: captured.Manifest().SnapshotDigest, SemanticStatus: "partial"}, Files: []string{"main.go"}}}
	}
	report, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := captured.File("spec.md")
	declarations := []DocumentDeclaration{}
	for _, pair := range [][2]string{{"objective", "serve users"}, {"component", "PaymentService"}, {"constraint", "preserve keys"}} {
		start := strings.Index(contents["spec.md"], pair[1])
		ref := model.SourceRef{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, Path: "spec.md", ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: int64(start), EndByte: int64(start + len(pair[1]))}}
		declarations = append(declarations, DocumentDeclaration{Kind: pair[0], Source: ref})
	}
	return ctx, PlanInput{Overview: report, Snapshot: captured, Policy: config, Selector: &model.WorkspaceSelector{Kind: "workspace"}, Declarations: declarations, Scanner: scanner, AuthorizeDocument: func(context.Context, Scope, DocumentDeclaration) error { return nil }, MaxBytes: 65536}
}

func TestSpecOnlyPlanKeepsDeclaredTextSeparate(t *testing.T) {
	ctx, in := planFixture(t, false)
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err := plan.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.ProjectStage != "spec_only" || report.ImplementationEvidence != "none" || len(report.Items) != 3 || len(report.Modules) != 0 {
		t.Fatalf("unexpected plan: %+v", report)
	}
	for _, item := range report.Items {
		if item.Plane != "intended" || item.MappingStatus != "unmapped" || item.Content == "" || item.Source.Path != "spec.md" {
			t.Fatalf("invented observed fact: %+v", item)
		}
	}
	gaps := strings.Join(report.Gaps, ",")
	if !strings.Contains(gaps, "INTENDED_MAPPING_UNKNOWN") || !strings.Contains(gaps, "SEMANTIC_COVERAGE_NOT_PROVEN") {
		t.Fatal("missing mapping or coverage gap")
	}
	first := report.Items[0]
	report.Items[0].Content = "changed"
	again, err := plan.Read(ctx)
	if err != nil || again.Items[0] != first {
		t.Fatal("plan result was not copied")
	}
}

func TestPlanSelectorsAndMixedProject(t *testing.T) {
	ctx, in := planFixture(t, true)
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err := plan.Read(ctx)
	if err != nil || report.ProjectStage != "mixed" || report.ImplementationEvidence != "partial" || len(report.Modules) != 1 {
		t.Fatalf("mixed evidence: %+v %v", report, err)
	}
	in.Selector = &model.ModuleSelector{Kind: "module", ModuleID: report.Modules[0].Ref.ModuleID}
	if plan, err = BuildPlan(ctx, in); err != nil {
		t.Fatal(err)
	}
	ref := in.Declarations[1].Source
	in.Selector = &model.DocumentSelector{Kind: "document_anchor", Path: ref.Path, ContentHash: ref.ContentHash, Range: ref.Range}
	plan, err = BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err = plan.Read(ctx)
	if err != nil || len(report.Items) != 1 || report.Items[0].Content != "PaymentService" || report.ProjectStage != "mixed" {
		t.Fatalf("exact selector: %+v %v", report, err)
	}
	ref.ContentHash = strings.Repeat("f", 64)
	in.Selector = &model.DocumentSelector{Kind: "document_anchor", Path: ref.Path, ContentHash: ref.ContentHash, Range: ref.Range}
	if _, err = BuildPlan(ctx, in); err == nil {
		t.Fatal("stale document selector accepted")
	}
}

func TestPlanEntitySelectorUsesExplicitDeclaration(t *testing.T) {
	ctx, in := planFixture(t, false)
	in.Declarations = append([]DocumentDeclaration{}, in.Declarations...)
	in.Declarations[1].EntityID = "billing"
	in.Selector = &model.EntitySelector{Kind: "entity", EntityID: "billing"}
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err := plan.Read(ctx)
	if err != nil || len(report.Items) != 1 || report.Items[0].Content != "PaymentService" || report.Items[0].Plane != "intended" || report.Items[0].MappingStatus != "unmapped" {
		t.Fatal("entity did not preserve declared provenance")
	}
	in.Selector = &model.EntitySelector{Kind: "entity", EntityID: "unknown"}
	if _, err = BuildPlan(ctx, in); err == nil {
		t.Fatal("invented entity mapping")
	}
	ref := &model.EntityRef{Kind: "entity", GraphIdentity: in.Overview.scope.Identity, EntityID: "billing"}
	in.Selector = &model.RefSelector{Kind: "ref", Ref: ref}
	if _, err = BuildPlan(ctx, in); err != nil {
		t.Fatal(err)
	}
	ref.GenerationID = "22222222-2222-4222-8222-222222222222"
	if _, err = BuildPlan(ctx, in); err == nil {
		t.Fatal("foreign entity scope accepted")
	}
}

func TestPlanRejectsStaleUntrustedAndRevokedDeclarations(t *testing.T) {
	ctx, in := planFixture(t, false)
	allowed := true
	in.AuthorizeDocument = func(context.Context, Scope, DocumentDeclaration) error {
		if !allowed {
			return errors.New("denied private text")
		}
		return nil
	}
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	allowed = false
	if report, err := plan.Read(ctx); !errors.Is(err, ErrAuthority) || len(report.Items) != 0 {
		t.Fatal("revoked plan leaked")
	}
	allowed = true
	bad := in
	bad.Declarations = append([]DocumentDeclaration{}, in.Declarations...)
	bad.Declarations[0].Source.ContentHash = strings.Repeat("f", 64)
	if _, err = BuildPlan(ctx, bad); err == nil {
		t.Fatal("stale source accepted")
	}
	bad = in
	bad.Policy.TrustedRulePaths = []string{}
	if _, err = BuildPlan(ctx, bad); err == nil {
		t.Fatal("changed policy accepted")
	}
	bad = in
	bad.Declarations = append([]DocumentDeclaration{}, in.Declarations...)
	bad.Declarations[0].Kind = "constraint"
	bad.Declarations[0].Source.Path = "untrusted.md"
	if _, err = BuildPlan(ctx, bad); !errors.Is(err, ErrAuthority) {
		t.Fatal("untrusted constraint path accepted")
	}
	bad = in
	bad.Declarations = append([]DocumentDeclaration{}, in.Declarations...)
	bad.Declarations[0].Source.Range.StartByte++
	bad.Declarations[0].Source.Range.EndByte = bad.Declarations[0].Source.Range.StartByte
	if _, err = BuildPlan(ctx, bad); err == nil {
		t.Fatal("empty source range accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = BuildPlan(cancelled, in); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
}

func TestPlanRechecksOverviewSourcesAfterRevocation(t *testing.T) {
	for _, deniedPath := range []string{"go.mod", "main.go"} {
		t.Run(deniedPath, func(t *testing.T) {
			ctx, in := planFixture(t, true)
			allowed := true
			in.Overview.authorizePath = func(_ context.Context, _ Scope, name string) error {
				if !allowed && name == deniedPath {
					return errors.New("source authority revoked")
				}
				return nil
			}
			plan, err := BuildPlan(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			allowed = false
			if report, err := plan.Read(ctx); !errors.Is(err, ErrAuthority) || len(report.Items) != 0 || len(report.Modules) != 0 {
				t.Fatalf("plan returned content after %s revocation: %v", deniedPath, err)
			}
		})
	}
}

func TestPlanDoesNotClassifyUnknownSourceAsSpecOnly(t *testing.T) {
	ctx, in := planFixtureWithFiles(t, false, "", map[string]string{"unsupported.ts": "export const value = 1\n"})
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err := plan.Read(ctx)
	if err != nil || report.ProjectStage != "unknown" || report.ImplementationEvidence != "partial" {
		t.Fatalf("unknown source misclassified: %+v %v", report, err)
	}
}

func TestPlanAuthorizesBeforeReadingDeclaredSource(t *testing.T) {
	ctx, in := planFixture(t, false)
	in.Declarations = append([]DocumentDeclaration{}, in.Declarations...)
	in.Declarations[0].Source.ContentHash = strings.Repeat("f", 64)
	in.AuthorizeDocument = func(context.Context, Scope, DocumentDeclaration) error { return errors.New("denied") }
	if _, err := BuildPlan(ctx, in); !errors.Is(err, ErrAuthority) {
		t.Fatalf("source was checked before approval: %v", err)
	}
}
