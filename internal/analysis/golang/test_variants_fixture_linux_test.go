//go:build linux

package golang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestGuardedTestVariantsSynthetic(t *testing.T) {
	if os.Getenv("MCE_VARIANT_FIXTURE_CHILD") == "1" {
		runTestVariantsFixture(t, os.Getenv("MCE_VARIANT_FIXTURE_DIR"))
		return
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestGuardedTestVariantsSynthetic$")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + filepath.Join(runtime.GOROOT(), "bin"), "GOMAXPROCS=4", "MCE_VARIANT_FIXTURE_CHILD=1", "MCE_VARIANT_FIXTURE_DIR=" + dir,
		"GOPACKAGESDRIVER=/untrusted/driver", "GOFLAGS=-toolexec=/untrusted/tool", "GOPROXY=https://untrusted.invalid"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("test-variant fixture: %v: %s", err, output)
	}
	if strings.Contains(string(output), dir) {
		t.Fatal("variant loader output leaked scratch path")
	}
}

func runTestVariantsFixture(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	const production = "package fixture\n//go:generate touch generator-marker\nfunc Exported() int { return 1 }\n"
	const internal = "package fixture\nimport (\"testing\"; \"os\")\nfunc TestInternal(t *testing.T) { _ = os.WriteFile(\"test-execution-marker\", []byte(\"x\"), 0600); _ = Exported() }\n"
	const external = "package fixture_test\nimport (\"testing\"; pkg \"example.test/fixture\")\nfunc TestExternal(t *testing.T) { _ = pkg.Exported() }\n"
	contents := map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "p.go": production, "a_test.go": internal, "x_test.go": external,
		"special_linux_test.go":   "package fixture\nfunc linuxHelper() { _ = Exported() }\n",
		"special_windows_test.go": "package fixture\nfunc windowsHelper() { _ = Exported() }\n",
		"tagged_test.go":          "//go:build feature\n\npackage fixture\nfunc taggedHelper() { _ = Exported() }\n"}
	workspace := filepath.Join(dir, "input")
	for name, value := range contents {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	preflight := loaderPreflightFixture(t, contents)
	preflight.Profile.TestsEnabled = true
	resetLoaderProfile(t, &preflight)
	digest := strings.Repeat("d", 64)
	addLoaderFixtureDependency(t, &preflight, digest)
	goRoot := runtime.GOROOT()
	binRoot, err := paths.OpenRoot(filepath.Join(goRoot, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer binRoot.Close()
	goBytes, err := os.ReadFile(filepath.Join(goRoot, "bin", "go"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(goBytes)
	tool, err := runtimeadapter.OpenGoExecutable(ctx, runtimeadapter.GoExecutableInput{BinRoot: binRoot, ExpectedSHA256: hex.EncodeToString(hash[:]), MaxBytes: 128 * 1024 * 1024, Authorize: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	preflight.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	preflight.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
	preflight.VerifyDependencies = func(_ context.Context, _ *model.GoProfile, values []string) (bool, error) {
		return len(values) == 1 && values[0] == digest, nil
	}
	env, _, err := LoaderEnvironment(preflight.Profile, goRoot, dir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	list := exec.CommandContext(ctx, filepath.Join(goRoot, "bin", "go"), "list", "-deps", "-f", "{{if .Standard}}{{.ImportPath}}{{end}}", "testing", "testing/internal/testdeps", "os", "reflect")
	list.Dir, list.Env = workspace, env
	output, err := list.Output()
	if err != nil || len(output) > 65536 {
		t.Fatalf("fixture stdlib inventory failed: %v", err)
	}
	approved := strings.Fields(string(output))
	stdlibRoot, err := paths.OpenRoot(filepath.Join(goRoot, "src"))
	if err != nil {
		t.Fatal(err)
	}
	defer stdlibRoot.Close()
	scanner, err := policy.NewScanner(2 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	newScope := func(input LoaderPreflightInput) *TrustedStdlib {
		t.Helper()
		scope, err := NewTrustedStdlib(ctx, TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: goRoot, Profile: input.Profile, Snapshot: input.Snapshot, DependencyDigest: digest, AllowedPackages: approved,
			Scanner: scanner, MaxManifestBytes: 32768, MaxFileBytes: 2 * 1024 * 1024, MaxFiles: 8192, MaxSourceBytes: 64 * 1024 * 1024,
			AuthorizePath: func(context.Context, string) error { return nil }, VerifyAuthority: func(context.Context, string, string, string, []string) (bool, error) { return true, nil }})
		if err != nil {
			t.Fatal(err)
		}
		return scope
	}
	scratchRoot, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer scratchRoot.Close()
	loader := CapturedLoaderInput{Preflight: preflight, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace, Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: LoaderScratchVerifier(scratchRoot, func(context.Context) error { return nil }), TestVariant: "internal_test"}
	without, err := LoadCapturedPackages(ctx, loader)
	if err != nil || without.BuildSelectionVerified || !containsReason(without.Reasons, "GO_TEST_STDLIB_UNVERIFIED") {
		t.Fatalf("missing test stdlib proof accepted: %v", err)
	}
	minimal, err := NewTrustedStdlib(ctx, TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: goRoot, Profile: preflight.Profile, Snapshot: preflight.Snapshot, DependencyDigest: digest, AllowedPackages: []string{"testing", "os", "reflect"},
		Scanner: scanner, MaxManifestBytes: 32768, MaxFileBytes: 2 * 1024 * 1024, MaxFiles: 8192, MaxSourceBytes: 64 * 1024 * 1024,
		AuthorizePath: func(context.Context, string) error { return nil }, VerifyAuthority: func(context.Context, string, string, string, []string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	incomplete := loader
	incomplete.Stdlib = minimal
	if got, err := LoadCapturedPackages(ctx, incomplete); err != nil || got.BuildSelectionVerified || !containsReason(got.Reasons, "GO_TEST_STDLIB_UNVERIFIED") {
		t.Fatalf("incomplete synthetic test-main stdlib proof accepted: %v", err)
	}
	denied := loader
	denied.Preflight.AuthorizePath = func(_ context.Context, _, path, _ string, _ int64) error {
		if path == "a_test.go" {
			return errors.New("private policy detail")
		}
		return nil
	}
	if got, err := LoadCapturedPackages(ctx, denied); !errors.Is(err, ErrLoaderAuthority) || got.BuildSelectionVerified || strings.Contains(err.Error(), "private") {
		t.Fatalf("denied test path was exposed: %v", err)
	}
	loader.Stdlib = newScope(preflight)
	internalInput := TypedInput{Loader: loader, PackageKey: "fixture-internal", ModuleKey: "fixture-module", MaxNodes: 20000, MaxDepth: 256, MaxFacts: 2000, MaxOccurrences: 2000, MaxOutputBytes: 512 * 1024}
	internalGraph, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: internalInput, MaxRelations: 4000, MaxOutputBytes: 1024 * 1024})
	if err != nil || !internalGraph.Typed.Loader.BuildSelectionVerified {
		t.Fatalf("internal test graph unavailable: reasons=%v err=%v", internalGraph.Reasons, err)
	}
	var target string
	selectedFiles := map[string]bool{}
	for _, syntax := range internalGraph.Typed.Syntax {
		selectedFiles[syntax.Path] = true
		if syntax.Path == "p.go" && syntax.Package.Variant == "internal_test" {
			for _, symbol := range syntax.Symbols {
				if symbol.Kind == "function" && symbol.Name == "Exported" {
					target = symbol.SymbolID
				}
			}
		}
	}
	if !selectedFiles["p.go"] || !selectedFiles["a_test.go"] || !selectedFiles["special_linux_test.go"] || selectedFiles["special_windows_test.go"] || selectedFiles["tagged_test.go"] || selectedFiles["x_test.go"] {
		t.Fatalf("wrong internal variant or Linux profile selection: %+v", selectedFiles)
	}
	findRef := func(graph GraphResult, path string, offset int, target string) string {
		for _, relation := range graph.Relations {
			if relation.Kind == "TEST_REF" && relation.EvidenceKind == "type_resolved" && relation.SitePath == path && relation.Range.StartByte == int64(offset) && relation.TargetSymbolID != nil && *relation.TargetSymbolID == target {
				return relation.RelationID
			}
		}
		return ""
	}
	internalSite := strings.Index(internal, "Exported()")
	internalRef := findRef(internalGraph, "a_test.go", internalSite, target)
	if target == "" || internalRef == "" {
		t.Fatal("internal test reference to production declaration missing")
	}
	loader.TestVariant, loader.Stdlib = "external_test", newScope(preflight)
	externalInput := internalInput
	externalInput.Loader = loader
	externalInput.PackageKey = "fixture-external"
	externalInput.OtherPackages = []TypedPackageIdentity{{ImportPath: "example.test/fixture", PackageKey: "fixture-internal", ModuleKey: "fixture-module"}}
	result, err := AnalyzeCapturedCoverage(ctx, CoverageInput{Graph: GraphInput{Typed: externalInput, MaxRelations: 4000, MaxOutputBytes: 1024 * 1024}, ProviderID: "go-fixture", ProviderVersion: "1", MaxOutputBytes: 2 * 1024 * 1024})
	if err != nil || !result.Graph.Typed.Loader.BuildSelectionVerified || result.Coverage.Status != "partial" {
		t.Fatalf("external test graph unavailable: %v", err)
	}
	externalSite := strings.Index(external, "pkg.Exported()") + len("pkg.")
	if findRef(result.Graph, "x_test.go", externalSite, target) == "" || findRef(result.Graph, "a_test.go", internalSite, target) != internalRef {
		t.Fatal("external test reference or physical occurrence dedup missing")
	}
	for _, capability := range result.Coverage.Capabilities {
		if capability.Name == "go_test_references" && capability.Status != "partial" {
			t.Fatal("test references were not reported as partial")
		}
	}
	if !containsReason(result.Graph.Reasons, "GRAPH_COVERAGE_PARTIAL") {
		t.Fatal("test graph claimed complete runtime coverage")
	}
	runGraphPublicationFixture(t, ctx, externalInput, result, "x_test.go", external)
	badContents := make(map[string]string, len(contents)+1)
	for name, value := range contents {
		badContents[name] = value
	}
	badContents["denied_windows_test.go"] = "package fixture\nimport _ \"example.invalid/unapproved\"\n"
	if err := os.WriteFile(filepath.Join(workspace, "denied_windows_test.go"), []byte(badContents["denied_windows_test.go"]), 0600); err != nil {
		t.Fatal(err)
	}
	badPreflight := loaderPreflightFixture(t, badContents)
	badPreflight.Profile.TestsEnabled = true
	resetLoaderProfile(t, &badPreflight)
	addLoaderFixtureDependency(t, &badPreflight, digest)
	badPreflight.VerifyClosure, badPreflight.VerifyToolchain, badPreflight.VerifyDependencies = preflight.VerifyClosure, preflight.VerifyToolchain, preflight.VerifyDependencies
	badLoader := loader
	badLoader.Preflight, badLoader.Stdlib = badPreflight, newScope(badPreflight)
	if got, err := LoadCapturedPackages(ctx, badLoader); err != nil || got.BuildSelectionVerified || !containsReason(got.Reasons, "GO_IMPORTS_UNVERIFIED") {
		t.Fatalf("unapproved test import did not remain a boundary: %v", err)
	}
	for _, marker := range []string{"generator-marker", "test-execution-marker"} {
		if _, err := os.Stat(filepath.Join(workspace, marker)); !os.IsNotExist(err) {
			t.Fatal("test fixture unexpectedly executed source")
		}
	}
}
