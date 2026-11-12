package golang

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type CoverageInput struct {
	Graph           GraphInput
	FallbackFiles   []SyntaxInput // explicit identities; syntax only if the loader is partial
	ProviderID      string
	ProviderVersion string
	MaxOutputBytes  int
}

type CoverageResult struct {
	WorktreeID     string                 `json:"worktree_id"`
	ProfileID      string                 `json:"profile_id"`
	SnapshotDigest string                 `json:"snapshot_digest"`
	Graph          GraphResult            `json:"graph"`
	Coverage       model.Coverage         `json:"coverage"`
	Limits         []string               `json:"limits"`
	Diagnostics    []model.DiagnosticFact `json:"diagnostics"`
	GeneratedPaths []string               `json:"generated_paths"`
	FallbackSyntax []SyntaxResult         `json:"fallback_syntax"`
	TestReferences []model.RelationFact   `json:"test_references"`
}

// AnalyzeCapturedCoverage reports only what the guarded graph load actually
// established. No result from this adapter proves workspace completeness,
// runtime behavior, behavioral test coverage, or the absence of impact.
func AnalyzeCapturedCoverage(ctx context.Context, input CoverageInput) (CoverageResult, error) {
	if ctx == nil || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 16*1024*1024 || len(input.ProviderID) > 768 || len(input.ProviderVersion) > 512 || len(input.FallbackFiles) > 128 {
		return CoverageResult{}, ErrLoaderInput
	}
	if err := ctx.Err(); err != nil {
		return CoverageResult{}, err
	}
	preflight, err := copyLoaderPreflight(input.Graph.Typed.Loader.Preflight)
	if err != nil {
		return CoverageResult{}, err
	}
	input.Graph.Typed.Loader.Preflight = preflight
	if input.Graph.MaxOutputBytes > input.MaxOutputBytes {
		input.Graph.MaxOutputBytes = input.MaxOutputBytes
	}
	if preflight.Scanner == nil {
		return CoverageResult{}, ErrLoaderInput
	}
	for _, value := range []string{input.ProviderID, input.ProviderVersion} {
		if err := preflight.Scanner.CheckContent([]byte(value)); err != nil {
			return CoverageResult{}, err
		}
	}
	probe := model.Capability{Name: "go_syntax", ProviderID: input.ProviderID, ProviderVersion: input.ProviderVersion, Status: "unknown"}
	raw, err := json.Marshal(probe)
	if err != nil {
		return CoverageResult{}, ErrLoaderInput
	}
	if _, err := model.DecodeCapabilityJSON(raw, 4096); err != nil {
		return CoverageResult{}, ErrLoaderInput
	}
	graph, err := AnalyzeCapturedGraph(ctx, input.Graph)
	if err != nil {
		return CoverageResult{}, err
	}
	manifest := preflight.Snapshot.Manifest()
	result := CoverageResult{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, SnapshotDigest: manifest.SnapshotDigest, Graph: graph, Diagnostics: []model.DiagnosticFact{}, GeneratedPaths: []string{}, FallbackSyntax: []SyntaxResult{}, TestReferences: []model.RelationFact{},
		Limits: []string{"selected captured package and profile only", "dynamic behavior and runtime execution are not complete", "static test references do not prove behavioral coverage", "generators are not run", "current workspace freshness and production authority require separate proof"}}
	testFilesAnalyzed, testFilesSkipped, typedTestFiles := 0, 0, 0
	if !graph.Typed.Loader.BuildSelectionVerified {
		requestedFiles := append([]SyntaxInput(nil), input.FallbackFiles...)
		sort.Slice(requestedFiles, func(i, j int) bool {
			if requestedFiles[i].Path != requestedFiles[j].Path {
				return requestedFiles[i].Path < requestedFiles[j].Path
			}
			return requestedFiles[i].Variant < requestedFiles[j].Variant
		})
		prior, used := "", 0
		for _, requested := range requestedFiles {
			key := requested.Path + "\x00" + requested.Variant
			if key == prior {
				return CoverageResult{}, ErrLoaderInput
			}
			prior = key
			if _, ok := preflight.Snapshot.File(requested.Path); !ok {
				return CoverageResult{}, ErrInputMismatch
			}
			if err := preflight.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
				return CoverageResult{}, loaderAuthorityError(ctx)
			}
			if err := preflight.AuthorizePath(ctx, manifest.WorktreeID, requested.Path, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
				return CoverageResult{}, loaderAuthorityError(ctx)
			}
			requested.Snapshot, requested.Profile, requested.Scanner = preflight.Snapshot, preflight.Profile, preflight.Scanner
			requested.MaxManifestBytes, requested.MaxFileBytes = preflight.MaxManifestBytes, preflight.MaxFileBytes
			if requested.MaxOutputBytes > input.MaxOutputBytes {
				requested.MaxOutputBytes = input.MaxOutputBytes
			}
			parsed, err := AnalyzeSyntaxFile(ctx, requested)
			if err != nil {
				return CoverageResult{}, err
			}
			data, err := json.Marshal(parsed)
			if err != nil || len(data)+1 > input.MaxOutputBytes-used {
				return CoverageResult{}, ErrSyntaxLimit
			}
			used += len(data) + 1
			result.FallbackSyntax = append(result.FallbackSyntax, parsed)
			if strings.HasSuffix(parsed.Path, "_test.go") && (parsed.Package.Variant == "internal_test" || parsed.Package.Variant == "external_test") && len(parsed.Diagnostics) == 0 {
				if !syntaxTestImportsEligible(parsed) {
					testFilesSkipped++
					continue
				}
				references, err := syntaxTestReferences(ctx, preflight, parsed, requested.MaxNodes, requested.MaxDepth, input.Graph.MaxRelations)
				if err != nil {
					return CoverageResult{}, err
				}
				if len(references) > input.Graph.MaxRelations-len(result.TestReferences) {
					return CoverageResult{}, ErrSyntaxLimit
				}
				result.TestReferences = append(result.TestReferences, references...)
				testFilesAnalyzed++
			}
		}
	}
	result.Coverage = model.Coverage{Status: "partial", ScopeDescription: "selected captured Go package and profile", Capabilities: []model.Capability{}, Gaps: []model.Gap{}}
	for _, syntax := range graph.Typed.Syntax {
		if strings.HasSuffix(syntax.Path, "_test.go") && (syntax.Package.Variant == "internal_test" || syntax.Package.Variant == "external_test") && syntax.BuildSelectionVerified {
			typedTestFiles++
		}
	}
	syntaxStatus, typedStatus := "unknown", "unknown"
	if len(graph.Typed.Syntax) > 0 || len(result.FallbackSyntax) > 0 {
		syntaxStatus = "partial"
	}
	if graph.Typed.Loader.BuildSelectionVerified {
		typedStatus = "partial"
	}
	testStatus := "unsupported"
	if testFilesAnalyzed > 0 || typedTestFiles > 0 {
		testStatus = "partial"
	}
	for _, entry := range []struct{ name, status string }{
		{"go_syntax", syntaxStatus},
		{"go_typed_references", typedStatus},
		{"go_static_calls", typedStatus},
		{"go_test_references", testStatus},
	} {
		result.Coverage.Capabilities = append(result.Coverage.Capabilities, model.Capability{Name: entry.name, ProviderID: input.ProviderID, ProviderVersion: input.ProviderVersion, Status: entry.status})
	}
	seen := map[string]bool{}
	addGap := func(code, reason string, count int64) {
		if seen[code] {
			return
		}
		seen[code] = true
		gap := model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{}}
		if count > 0 {
			value := count
			gap.Count = &value
		}
		result.Coverage.Gaps = append(result.Coverage.Gaps, gap)
	}
	addGap("GO_SCOPE_PARTIAL", "Only a selected captured Go package and profile were considered; an empty result does not prove no dependencies or impact.", 0)
	addGap("DYNAMIC_BEHAVIOR_UNRESOLVED", "Reflection, dynamic invocation, function values, plugins and runtime configuration are not a complete runtime graph.", 0)
	if testFilesAnalyzed == 0 && typedTestFiles == 0 {
		addGap("GO_TEST_REFERENCES_UNSUPPORTED", "Test variants and static test references have not been verified; test links would not prove behavioral coverage.", 0)
	} else if typedTestFiles > 0 {
		addGap("GO_TEST_REFERENCES_PARTIAL", "Typed static test references cover only the selected test variant and captured packages; links do not prove behavioral coverage.", int64(typedTestFiles))
	} else {
		addGap("GO_TEST_REFERENCES_PARTIAL", "Only same-file lexical function references were linked; cross-file bindings and test variant selection remain unverified, and links do not prove behavioral coverage.", int64(testFilesAnalyzed))
	}
	if testFilesSkipped > 0 {
		addGap("GO_TEST_IMPORT_BINDINGS_UNVERIFIED", "Some test files were not linked because imported names could shadow same-file declarations.", int64(testFilesSkipped))
	}
	addGap("GENERATOR_NOT_EXECUTED", "Generators were not run and generated inputs outside the capture are not inferred.", 0)
	for _, reason := range graph.Reasons {
		addGap(reason, "Go analysis reported an unresolved or unverified boundary in the selected scope.", 0)
	}
	selected := map[string]bool{}
	generated := map[string]bool{}
	for _, syntax := range graph.Typed.Syntax {
		selected[syntax.Path] = true
		if syntax.Generated != nil && *syntax.Generated {
			generated[syntax.Path] = true
		}
	}
	var parseErrors int64
	for _, syntax := range result.FallbackSyntax {
		if syntax.Generated != nil && *syntax.Generated {
			generated[syntax.Path] = true
		}
		parseErrors += int64(len(syntax.Diagnostics))
	}
	for filename := range generated {
		result.GeneratedPaths = append(result.GeneratedPaths, filename)
	}
	sort.Strings(result.GeneratedPaths)
	if len(result.GeneratedPaths) > 0 {
		addGap("GENERATED_CAPTURED_SOURCE", "Generated source was identified by its Go marker; it was not generated or changed by analysis.", int64(len(result.GeneratedPaths)))
	}
	if parseErrors > 0 {
		addGap("GO_PARSE_ERRORS", "Some captured Go files could not be parsed; unavailable facts are not evidence of no dependencies.", parseErrors)
	}
	var unselected, uncaptured, unsupported int64
	for _, item := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return CoverageResult{}, err
		}
		if item.Status != "captured" {
			uncaptured++
		}
		if strings.HasSuffix(item.Path, ".go") && !selected[item.Path] {
			unselected++
		}
		switch strings.ToLower(path.Ext(item.Path)) {
		case ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".sql", ".json", ".yaml", ".yml", ".toml":
			unsupported++
		}
	}
	if unselected > 0 {
		addGap("GO_FILES_NOT_SELECTED", "Some captured or unavailable Go files were not analyzed in the selected package variant.", unselected)
	}
	if uncaptured > 0 {
		addGap("CAPTURE_FILES_UNAVAILABLE", "Some manifest entries have no captured bytes and cannot establish semantic facts.", uncaptured)
	}
	if unsupported > 0 {
		addGap("NON_GO_SEMANTICS_UNSUPPORTED", "Other languages and configuration may have lexical search only; text matches are not semantic relations.", unsupported)
	}
	if len(preflight.Inventory.Entries) > 100000 || preflight.Inventory.DeniedCount > 100000 || preflight.Inventory.InvalidPathCount > 100000 || preflight.Inventory.IgnoredCount > 100000 {
		return CoverageResult{}, ErrSyntaxLimit
	}
	var boundaryCount int64
	for _, item := range preflight.Inventory.Entries {
		if item.State != "candidate" || item.Directory {
			boundaryCount++
		}
	}
	boundaryCount += int64(preflight.Inventory.DeniedCount) + int64(preflight.Inventory.InvalidPathCount) + int64(preflight.Inventory.IgnoredCount)
	if boundaryCount > 0 {
		addGap("INVENTORY_BOUNDARIES", "Excluded, sparse, nested, inaccessible, denied or ignored inventory entries are not assumed analyzed.", boundaryCount)
	}
	sort.Slice(result.Coverage.Gaps, func(i, j int) bool { return result.Coverage.Gaps[i].Code < result.Coverage.Gaps[j].Code })
	if len(result.Coverage.Gaps) > 64 {
		return CoverageResult{}, ErrSyntaxLimit
	}
	for _, gap := range result.Coverage.Gaps {
		id, err := identityHash("go-coverage-diagnostic-v1", manifest.ProfileID, "", "", gap.Code, manifest.SnapshotDigest, 0, 0)
		if err != nil {
			return CoverageResult{}, err
		}
		diagnostic := model.DiagnosticFact{DiagnosticID: id, Code: gap.Code, Severity: "warning", Message: gap.Reason, ScopeDescription: "selected captured Go package and profile"}
		encoded, err := json.Marshal(diagnostic)
		if err != nil {
			return CoverageResult{}, ErrInputMismatch
		}
		if _, err := model.DecodeDiagnosticFactJSON(encoded, 8192); err != nil {
			return CoverageResult{}, ErrInputMismatch
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic)
	}
	for _, syntax := range result.FallbackSyntax {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostics...)
	}
	encodedCoverage, err := json.Marshal(result.Coverage)
	if err != nil {
		return CoverageResult{}, ErrInputMismatch
	}
	if len(encodedCoverage) > input.MaxOutputBytes {
		return CoverageResult{}, ErrSyntaxLimit
	}
	if _, err := model.DecodeCoverageJSON(encodedCoverage, input.MaxOutputBytes); err != nil {
		return CoverageResult{}, ErrInputMismatch
	}
	if graph.Typed.Loader.BuildSelectionVerified {
		if err := graphAuthorityRecheck(ctx, input.Graph.Typed); err != nil {
			return CoverageResult{}, err
		}
	} else {
		post, err := PreflightLoader(ctx, preflight)
		if err != nil {
			return CoverageResult{}, err
		}
		if post.SnapshotDigest != manifest.SnapshotDigest || post.ProfileID != manifest.ProfileID {
			return CoverageResult{}, ErrLoaderAuthority
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > input.MaxOutputBytes {
		return CoverageResult{}, ErrSyntaxLimit
	}
	if err := ctx.Err(); err != nil {
		return CoverageResult{}, err
	}
	return result, nil
}
