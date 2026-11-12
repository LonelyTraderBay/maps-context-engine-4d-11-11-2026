//go:build linux

package golang

import (
	"context"
	"errors"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func runCoverageFixture(t *testing.T, ctx context.Context, typed TypedInput, source string) {
	t.Helper()
	typed.MaxOutputBytes = 256 * 1024
	input := CoverageInput{Graph: GraphInput{Typed: typed, MaxRelations: 4000, MaxOutputBytes: 512 * 1024}, ProviderID: "go-fixture", ProviderVersion: "1", MaxOutputBytes: 512 * 1024}
	result, err := AnalyzeCapturedCoverage(ctx, input)
	if err != nil {
		t.Fatalf("coverage fixture: %v", err)
	}
	manifest := typed.Loader.Preflight.Snapshot.Manifest()
	if result.Coverage.Status != "partial" || result.WorktreeID != manifest.WorktreeID || result.ProfileID != manifest.ProfileID || result.SnapshotDigest != manifest.SnapshotDigest || len(result.Limits) == 0 || len(result.GeneratedPaths) != 1 || result.GeneratedPaths[0] != "dispatch.go" {
		t.Fatalf("generated/coverage status incorrect: %+v", result.Coverage)
	}
	gaps := map[string]int64{}
	for _, gap := range result.Coverage.Gaps {
		gaps[gap.Code] = 0
		if gap.Count != nil {
			gaps[gap.Code] = *gap.Count
		}
	}
	for _, code := range []string{"GO_SCOPE_PARTIAL", "DYNAMIC_BEHAVIOR_UNRESOLVED", "GO_TEST_REFERENCES_UNSUPPORTED", "GENERATOR_NOT_EXECUTED", "GENERATED_CAPTURED_SOURCE", "GO_FILES_NOT_SELECTED", "NON_GO_SEMANTICS_UNSUPPORTED"} {
		if _, ok := gaps[code]; !ok {
			t.Fatalf("required coverage boundary missing: %s", code)
		}
	}
	if gaps["GENERATED_CAPTURED_SOURCE"] != 1 || gaps["GO_FILES_NOT_SELECTED"] != 1 || gaps["NON_GO_SEMANTICS_UNSUPPORTED"] != 2 || len(result.Diagnostics) != len(result.Coverage.Gaps) {
		t.Fatalf("coverage counts or diagnostics incorrect: %+v", gaps)
	}
	for _, cap := range result.Coverage.Capabilities {
		if cap.Status == "supported_scope_complete" || cap.Name == "go_test_references" && cap.Status != "unsupported" {
			t.Fatalf("capability overclaim: %+v", cap)
		}
	}
	for _, relation := range result.Graph.Relations {
		if relation.Kind == "TEST_REF" || relation.SitePath == "client.ts" || relation.SitePath == "query.sql" || relation.SitePath == "dispatch_test.go" {
			t.Fatalf("unsupported text or test variant produced semantic relation: %+v", relation)
		}
	}
	runGraphPublicationFixture(t, ctx, typed, result, "dispatch.go", source)
	missingTool := input
	missingTool.Graph.Typed.Loader.Tool = nil
	partial, err := AnalyzeCapturedCoverage(ctx, missingTool)
	if err != nil || partial.Coverage.Status != "partial" || partial.Graph.Typed.Loader.BuildSelectionVerified || len(partial.Graph.Relations) != 0 || len(partial.GeneratedPaths) != 0 {
		t.Fatalf("missing tool claimed analysis: %v", err)
	}
	for _, cap := range partial.Coverage.Capabilities {
		if cap.Name == "go_typed_references" && cap.Status != "unknown" {
			t.Fatal("missing tool claimed typed references")
		}
	}
	bad := input
	bad.ProviderID = ""
	if got, err := AnalyzeCapturedCoverage(ctx, bad); !errors.Is(err, ErrLoaderInput) || len(got.Diagnostics) != 0 {
		t.Fatalf("invalid provider identity returned coverage: %v", err)
	}
	checks := 0
	revoked := input
	revoked.Graph.Typed.Loader.VerifyScratch = func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
		checks++
		return checks < 5, nil
	}
	if got, err := AnalyzeCapturedCoverage(ctx, revoked); !errors.Is(err, ErrLoaderAuthority) || len(got.Graph.Relations) != 0 || len(got.Diagnostics) != 0 || checks != 5 {
		t.Fatalf("revocation after coverage returned facts: checks=%d err=%v", checks, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := AnalyzeCapturedCoverage(canceled, input); !errors.Is(err, context.Canceled) || len(got.Graph.Relations) != 0 || len(got.Diagnostics) != 0 {
		t.Fatalf("canceled coverage returned facts: %v", err)
	}
}

func runTestReferencesPublicationFixture(t *testing.T, ctx context.Context) {
	t.Helper()
	const source = "package fixture\nfunc helper() {}\nfunc TestSame() { helper() }\n"
	preflight := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "prod.go": "package fixture\nfunc Prod() {}\n", "same_test.go": source})
	preflight.Profile.TestsEnabled = true
	resetLoaderProfile(t, &preflight)
	typed := TypedInput{Loader: CapturedLoaderInput{Preflight: preflight, MaxOutputBytes: 65536}, PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000, MaxOutputBytes: 65536}
	syntax := SyntaxInput{Path: "same_test.go", PackageKey: "fixture-internal", ImportPath: "example.test/fixture", ModuleKey: "fixture-module", PackageName: "fixture", Variant: "internal_test", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOutputBytes: 65536}
	input := CoverageInput{Graph: GraphInput{Typed: typed, MaxRelations: 1000, MaxOutputBytes: 65536}, FallbackFiles: []SyntaxInput{syntax}, ProviderID: "go-fixture", ProviderVersion: "1", MaxOutputBytes: 262144}
	result, err := AnalyzeCapturedCoverage(ctx, input)
	if err != nil || len(result.TestReferences) != 1 || result.TestReferences[0].Kind != "TEST_REF" {
		t.Fatalf("test-reference publication input: %v", err)
	}
	runGraphPublicationFixture(t, ctx, typed, result, "same_test.go", source)
}
