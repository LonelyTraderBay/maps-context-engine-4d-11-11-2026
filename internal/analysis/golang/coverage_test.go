package golang

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCoverageSyntaxFallbackRetainsParseFailureAndNoZeroClaim(t *testing.T) {
	ctx := preflightContext(t)
	preflight := loaderPreflightFixture(t, map[string]string{
		"go.mod": "module example.test/fixture\n", "p.go": "package fixture\nfunc broken(\n",
		"client.ts": "const broken = 'broken'\n", "query.sql": "SELECT 'broken'\n",
	})
	fallback := fallbackRequest(preflight)
	input := CoverageInput{Graph: GraphInput{Typed: TypedInput{Loader: fallback.Loader, PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000, MaxOutputBytes: 65536}, MaxRelations: 1000, MaxOutputBytes: 65536}, FallbackFiles: fallback.Files, ProviderID: "go-fixture", ProviderVersion: "1", MaxOutputBytes: 262144}
	result, err := AnalyzeCapturedCoverage(ctx, input)
	if err != nil || result.Coverage.Status != "partial" || len(result.FallbackSyntax) != 1 || result.FallbackSyntax[0].BuildSelectionVerified || len(result.Graph.Relations) != 0 {
		t.Fatalf("parse failure lost fallback or partial status: %v", err)
	}
	seen := map[string]bool{}
	for _, gap := range result.Coverage.Gaps {
		seen[gap.Code] = true
	}
	for _, code := range []string{"GO_PARSE_ERRORS", "GO_SCOPE_PARTIAL", "GO_TEST_REFERENCES_UNSUPPORTED", "NON_GO_SEMANTICS_UNSUPPORTED", "GO_FILES_NOT_SELECTED"} {
		if !seen[code] {
			t.Fatalf("missing boundary %s", code)
		}
	}
	parseDiagnostic := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "GO_PARSE_ERROR" {
			parseDiagnostic = diagnostic.Path != nil && *diagnostic.Path == "p.go" && !strings.Contains(diagnostic.Message, "broken")
		}
	}
	if !parseDiagnostic {
		t.Fatal("parse diagnostic was omitted or leaked source")
	}
	duplicate := input
	duplicate.FallbackFiles = append(append([]SyntaxInput(nil), input.FallbackFiles...), input.FallbackFiles[0])
	if got, err := AnalyzeCapturedCoverage(ctx, duplicate); !errors.Is(err, ErrLoaderInput) || len(got.Diagnostics) != 0 {
		t.Fatalf("duplicate fallback produced facts: %v", err)
	}
	missing := input
	missing.FallbackFiles = append([]SyntaxInput(nil), input.FallbackFiles...)
	missing.FallbackFiles[0].Path = "missing.go"
	if got, err := AnalyzeCapturedCoverage(ctx, missing); !errors.Is(err, ErrInputMismatch) || len(got.Diagnostics) != 0 {
		t.Fatalf("uncaptured fallback produced facts: %v", err)
	}
	checks := 0
	revoked := input
	revoked.Graph.Typed.Loader.Preflight.AuthorizePath = func(_ context.Context, _, path, _ string, _ int64) error {
		if path == "p.go" {
			checks++
			if checks == 2 {
				return errors.New("private authorization detail")
			}
		}
		return nil
	}
	if got, err := AnalyzeCapturedCoverage(ctx, revoked); !errors.Is(err, ErrLoaderAuthority) || len(got.Diagnostics) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked fallback returned data: %v", err)
	}
}

func TestCoverageTestReferencesAreSameFileAndDoNotProveExecution(t *testing.T) {
	ctx := preflightContext(t)
	const internal = `package fixture
func helper() {}
func TestSame() {
 helper()
 helper()
 other := helper
 _ = other
 helper := func() {}
 helper()
 Prod()
}
`
	const external = `package fixture_test
import alias "example.test/fixture"
func helper() {}
func TestExternal() { helper(); alias.Prod() }
`
	const dotImport = "package fixture\nimport . \"example.test/dependency\"\nfunc helper() {}\nfunc TestDot() { helper() }\n"
	const implicitImport = "package fixture\nimport \"example.test/dependency\"\nfunc helper() {}\nfunc TestImplicit() { helper() }\n"
	const conflictingAlias = "package fixture\nimport helper \"example.test/dependency\"\nfunc helper() {}\nfunc TestAlias() { helper() }\n"
	preflight := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "prod.go": "package fixture\nfunc Prod() {}\n", "internal_test.go": internal, "external_test.go": external,
		"dot_test.go": dotImport, "implicit_test.go": implicitImport, "alias_test.go": conflictingAlias})
	preflight.Profile.TestsEnabled = true
	resetLoaderProfile(t, &preflight)
	fallback := fallbackRequest(preflight)
	base := SyntaxInput{ModuleKey: "fixture-module", PackageName: "fixture", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOutputBytes: 65536}
	internalInput := base
	internalInput.Path, internalInput.PackageKey, internalInput.ImportPath, internalInput.Variant = "internal_test.go", "fixture-internal", "example.test/fixture", "internal_test"
	externalInput := base
	externalInput.Path, externalInput.PackageKey, externalInput.ImportPath, externalInput.Variant = "external_test.go", "fixture-external", "example.test/fixture_test", "external_test"
	var requests = []SyntaxInput{internalInput, externalInput}
	for _, filename := range []string{"dot_test.go", "implicit_test.go", "alias_test.go"} {
		request := internalInput
		request.Path = filename
		requests = append(requests, request)
	}
	input := CoverageInput{Graph: GraphInput{Typed: TypedInput{Loader: fallback.Loader, PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000, MaxOutputBytes: 65536}, MaxRelations: 100, MaxOutputBytes: 65536}, FallbackFiles: requests, ProviderID: "go-fixture", ProviderVersion: "1", MaxOutputBytes: 262144}
	result, err := AnalyzeCapturedCoverage(ctx, input)
	if err != nil || result.Coverage.Status != "partial" || len(result.FallbackSyntax) != 5 {
		t.Fatalf("test syntax coverage failed: %v", err)
	}
	var partial bool
	for _, capability := range result.Coverage.Capabilities {
		partial = partial || capability.Name == "go_test_references" && capability.Status == "partial"
	}
	if !partial || len(result.TestReferences) != 4 {
		t.Fatalf("same-file test references missing or overclaimed: partial=%v refs=%d", partial, len(result.TestReferences))
	}
	owners, targets := map[string]string{}, map[string]string{}
	for _, file := range result.FallbackSyntax {
		for _, symbol := range file.Symbols {
			if symbol.Kind == "function" && (symbol.Name == "TestSame" || symbol.Name == "TestExternal") {
				owners[file.Path] = symbol.SymbolID
			}
			if symbol.Kind == "function" && symbol.Name == "helper" {
				targets[file.Path] = symbol.SymbolID
			}
		}
	}
	want := map[string]map[int64]bool{
		"internal_test.go": {
			int64(strings.Index(internal, " helper()\n")) + 1:                                        true,
			int64(strings.Index(internal, " helper()\n helper()\n")) + int64(len(" helper()\n")) + 1: true,
			int64(strings.Index(internal, "other := helper")) + int64(len("other := ")):              true,
		},
		"external_test.go": {int64(strings.Index(external, "{ helper();")) + 2: true},
	}
	ids := map[string]bool{}
	for _, relation := range result.TestReferences {
		if relation.Kind != "TEST_REF" || relation.EvidenceKind != "syntax_exact" || relation.TargetSymbolID == nil || *relation.TargetSymbolID != targets[relation.SitePath] || relation.SourceSymbolID != owners[relation.SitePath] || relation.Reason != "same_file_lexical_binding" || !want[relation.SitePath][relation.Range.StartByte] || ids[relation.RelationID] {
			t.Fatalf("unsafe test relation: %+v", relation)
		}
		ids[relation.RelationID] = true
		delete(want[relation.SitePath], relation.Range.StartByte)
	}
	if len(want["internal_test.go"]) != 0 || len(want["external_test.go"]) != 0 {
		t.Fatalf("test-reference sites were not preserved: %+v", want)
	}
	partialGap, importsGap := false, false
	for _, gap := range result.Coverage.Gaps {
		partialGap = partialGap || gap.Code == "GO_TEST_REFERENCES_PARTIAL" && strings.Contains(gap.Reason, "do not prove behavioral coverage")
		importsGap = importsGap || gap.Code == "GO_TEST_IMPORT_BINDINGS_UNVERIFIED" && gap.Count != nil && *gap.Count == 3
	}
	if !partialGap || !importsGap {
		t.Fatal("test-reference or uncertain import limitations were omitted")
	}
}
