//go:build linux

package golang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/tools/go/packages"
)

// This is a synthetic integration probe, not a production loader or proof of
// filesystem/network isolation. It executes no source, tests, or generators.
func TestGuardedPackagesLoadSyntheticOverlay(t *testing.T) {
	if os.Getenv("MCE_LOADER_FIXTURE_CHILD") == "1" {
		runGuardedLoaderFixture(t, os.Getenv("MCE_LOADER_FIXTURE_DIR"))
		return
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "input", "go.mod"), []byte("module example.test/fixture\n\ngo 1.27.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The on-disk file differs from capture. The loader must consume overlay.
	if err := os.WriteFile(filepath.Join(dir, "input", "p.go"), []byte("package fixture\nvar Changed int\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestGuardedPackagesLoadSyntheticOverlay$")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + filepath.Join(runtime.GOROOT(), "bin"), "GOMAXPROCS=4", "MCE_LOADER_FIXTURE_CHILD=1", "MCE_LOADER_FIXTURE_DIR=" + dir,
		"GOPACKAGESDEBUG=1", "GOPACKAGESDRIVER=/untrusted/driver", "GOFLAGS=-toolexec=/untrusted/tool", "GOPROXY=https://untrusted.invalid"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("guarded fixture: %v: %s", err, output)
	}
	if strings.Contains(string(output), dir) {
		t.Fatal("loader debug output leaked fixture path")
	}
	if _, err := os.Stat(filepath.Join(dir, "input", "generator-marker")); !os.IsNotExist(err) {
		t.Fatal("fixture generator was run")
	}
}

func runGuardedLoaderFixture(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const source = "package fixture\n//go:generate touch generator-marker\nvar Original string\n"
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "p.go": source})
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
	input.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	input.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
	plan, err := PreflightLoader(ctx, input)
	if err != nil || plan.Status != "eligible_for_guarded_load" {
		t.Fatalf("fixture preflight: %v", err)
	}
	workspace := filepath.Join(dir, "input")
	if err := os.WriteFile(filepath.Join(workspace, "p.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	scratchRoot, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer scratchRoot.Close()
	productionResult, err := LoadCapturedPackages(ctx, CapturedLoaderInput{Preflight: input, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace, Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: LoaderScratchVerifier(scratchRoot, func(context.Context) error { return nil })})
	if err != nil || !productionResult.BuildSelectionVerified || productionResult.Status != "partial" || len(productionResult.Packages) != 1 || productionResult.Packages[0].ImportPath != "example.test/fixture" || len(productionResult.Packages[0].Files) != 1 || productionResult.Packages[0].Files[0] != "p.go" {
		t.Fatalf("bounded loader fixture: %+v %v", productionResult, err)
	}
	checks := 0
	changed, err := LoadCapturedPackages(ctx, CapturedLoaderInput{Preflight: input, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace, Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
			checks++
			return checks == 1, nil
		}})
	if err != nil || changed.BuildSelectionVerified || len(changed.Packages) != 0 || !containsReason(changed.Reasons, "SCRATCH_CHANGED") {
		t.Fatalf("changed scratch accepted: %+v %v", changed, err)
	}
	revoked := false
	revokedInput := input
	revokedInput.AuthorizeScope = func(context.Context, string, string, string, int64) error {
		if revoked {
			return errors.New("private scope detail")
		}
		return nil
	}
	checks = 0
	revokedResult, err := LoadCapturedPackages(ctx, CapturedLoaderInput{Preflight: revokedInput, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace, Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
			checks++
			if checks == 2 {
				revoked = true
				return false, nil
			}
			return true, nil
		}})
	if !errors.Is(err, ErrLoaderAuthority) || revokedResult.SnapshotDigest != "" || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked scope leaked: %+v %v", revokedResult, err)
	}
	broken := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "p.go": "package fixture\nvar =\n"})
	broken.VerifyClosure, broken.VerifyToolchain = input.VerifyClosure, input.VerifyToolchain
	checks = 0
	brokenResult, err := LoadCapturedPackages(ctx, CapturedLoaderInput{Preflight: broken, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace, Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: func(context.Context, string, string, *snapshot.Snapshot) (bool, error) { checks++; return true, nil }})
	if err != nil || brokenResult.BuildSelectionVerified || !containsReason(brokenResult.Reasons, "GO_PACKAGE_LOAD_PARTIAL") || checks != 2 {
		t.Fatalf("broken source skipped postchecks: %+v checks=%d %v", brokenResult, checks, err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "p.go"), []byte("package fixture\nvar Changed int\n"), 0600); err != nil {
		t.Fatal(err)
	}
	overlay, err := NewCapturedOverlay(ctx, CapturedOverlayInput{Snapshot: input.Snapshot, Profile: input.Profile, SyntheticRoot: workspace, PolicyEpoch: 1, Scanner: input.Scanner, AuthorizeScope: input.AuthorizeScope, AuthorizePath: input.AuthorizePath, MaxManifestBytes: 32768, MaxFileBytes: 65536, MaxOverlayBytes: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := overlay.Overlay()
	if err != nil {
		t.Fatal(err)
	}
	env, flags, err := LoaderEnvironment(input.Profile, goRoot, dir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := tool.CheckForLoad(ctx, goRoot); err != nil {
		t.Fatal(err)
	}
	loaded, err := packages.Load(&packages.Config{Context: ctx, Dir: workspace, Env: env, BuildFlags: flags, Tests: false, Overlay: bytes, ParseFile: overlay.ParseFile, Logf: func(string, ...any) {},
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}, ".")
	if err != nil || len(loaded) != 1 {
		t.Fatalf("package load: %v", err)
	}
	if len(loaded[0].Errors) != 0 || loaded[0].Types == nil || loaded[0].TypesInfo == nil {
		t.Fatalf("partial fixture package: %v", loaded[0].Errors)
	}
	if loaded[0].Types.Scope().Lookup("Changed") != nil {
		t.Fatal("live disk bytes overrode capture")
	}
	original := loaded[0].Types.Scope().Lookup("Original")
	if original == nil || original.Type() != types.Typ[types.String] {
		t.Fatal("captured declaration not loaded")
	}
	if err := tool.CheckForLoad(ctx, goRoot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(env, "\n"), "GOPROXY=https") {
		t.Fatal("network configuration inherited")
	}
	runGuardedLocalImportFixture(t, ctx, dir, goRoot, tool)
	runSubpackageOnlyFixture(t, ctx, dir, goRoot, tool)
	runGuardedBuildSelectionFixture(t, ctx, dir, goRoot, tool)
	runTypedBindingsFixture(t, ctx, dir, goRoot, tool)
	runTypedDispatchFixture(t, ctx, dir, goRoot, tool)
	runCacheDifferentialFixture(t, ctx, dir, goRoot, tool)
}

func runSubpackageOnlyFixture(t *testing.T, ctx context.Context, parent, goRoot string, tool *runtimeadapter.GoExecutable) {
	t.Helper()
	dir := filepath.Join(parent, "subpackage-only")
	for _, name := range []string{"input/pkg", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	contents := map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "pkg/value.go": "package pkg\nvar Value int\n"}
	for name, value := range contents {
		if err := os.WriteFile(filepath.Join(dir, "input", filepath.FromSlash(name)), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := loaderPreflightFixture(t, contents)
	input.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	input.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	result, err := LoadCapturedPackages(ctx, CapturedLoaderInput{Preflight: input, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: filepath.Join(dir, "input"), PackageDir: "pkg", Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: LoaderScratchVerifier(root, func(context.Context) error { return nil })})
	if err != nil || !result.BuildSelectionVerified || len(result.Packages) != 1 || result.Packages[0].ImportPath != "example.test/fixture/pkg" {
		t.Fatalf("module with no root Go files: %+v %v", result, err)
	}
}

func runGuardedLocalImportFixture(t *testing.T, ctx context.Context, parent, goRoot string, tool *runtimeadapter.GoExecutable) {
	t.Helper()
	dir := filepath.Join(parent, "local-import")
	for _, name := range []string{"input/sub", "input/other", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	contents := map[string]string{
		"go.mod":         "module example.test/fixture\n\ngo 1.27.4\n",
		"p.go":           "package fixture\nimport \"example.test/fixture/sub\"\nvar Value = sub.Value\nvar BoxValue = sub.Box[int]{}\nvar FieldValue = BoxValue.Item\nfunc Called() int { return sub.Fn(BoxValue.Get()) }\n",
		"sub/value.go":   "package sub\nvar Value string\ntype Box[T any] struct { Item T }\nfunc (b Box[T]) Get() T { return b.Item }\nfunc Fn(x int) int { return x }\n",
		"other/other.go": "package other\nimport _ \"example.invalid/missing\"\n",
	}
	for name, value := range contents {
		if err := os.WriteFile(filepath.Join(dir, "input", filepath.FromSlash(name)), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := loaderPreflightFixture(t, contents)
	input.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	input.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
	scratchRoot, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer scratchRoot.Close()
	result, err := LoadCapturedPackages(ctx, CapturedLoaderInput{Preflight: input, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: filepath.Join(dir, "input"), Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: LoaderScratchVerifier(scratchRoot, func(context.Context) error { return nil })})
	if err != nil || !result.BuildSelectionVerified || len(result.Packages) != 1 || result.Packages[0].ImportPath != "example.test/fixture" {
		t.Fatalf("local dependency load: %+v %v", result, err)
	}
	typedRequest := TypedInput{Loader: CapturedLoaderInput{Preflight: input, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: filepath.Join(dir, "input"), Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: LoaderScratchVerifier(scratchRoot, func(context.Context) error { return nil })},
		PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000, MaxOutputBytes: 65536}
	withoutIdentity, err := AnalyzeTypedCaptured(ctx, typedRequest)
	if err != nil || !containsReason(withoutIdentity.Reasons, "CROSS_PACKAGE_BINDING_PENDING") {
		t.Fatalf("missing dependency identity was not a boundary: %+v %v", withoutIdentity, err)
	}
	typedRequest.OtherPackages = []TypedPackageIdentity{{ImportPath: "example.test/fixture/sub", PackageKey: "fixture-sub", ModuleKey: "fixture-module"}}
	typed, err := AnalyzeTypedCaptured(ctx, typedRequest)
	if err != nil || len(typed.Syntax) != 2 || containsReason(typed.Reasons, "CROSS_PACKAGE_BINDING_PENDING") || containsReason(typed.Reasons, "LOCAL_BINDING_UNMAPPED") {
		t.Fatalf("cross-package type load: %+v %v", typed, err)
	}
	targets := map[string]string{}
	for _, file := range typed.Syntax {
		if file.Path == "sub/value.go" {
			for _, symbol := range file.Symbols {
				if symbol.Kind == "variable" && symbol.Name == "Value" || symbol.Kind == "field" && symbol.Name == "Item" || symbol.Kind == "method" && symbol.Name == "Get" || symbol.Kind == "function" && symbol.Name == "Fn" {
					targets[symbol.Name] = symbol.SymbolID
				}
			}
		}
	}
	for _, want := range []struct{ name, fragment, prefix, selection string }{{"Value", "sub.Value", "sub.", ""}, {"Item", "BoxValue.Item", "BoxValue.", "field"}, {"Get", "BoxValue.Get", "BoxValue.", "method_value"}, {"Fn", "sub.Fn", "sub.", ""}} {
		wantOffset := strings.Index(contents["p.go"], want.fragment) + len(want.prefix)
		bound := false
		for _, occurrence := range typed.Occurrences {
			if occurrence.Path == "p.go" && occurrence.Kind == "use" && occurrence.Name == want.name && occurrence.Range.StartByte == int64(wantOffset) {
				bound = targets[want.name] != "" && occurrence.SymbolID != nil && *occurrence.SymbolID == targets[want.name] && occurrence.Resolution == "captured" && occurrence.Selection == want.selection
			}
		}
		if !bound {
			t.Fatalf("local dependency reference did not bind: %s", want.name)
		}
	}
	graphRequest := typedRequest
	graphRequest.MaxOutputBytes = 256 * 1024
	graph, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: graphRequest, MaxRelations: 2000, MaxOutputBytes: 256 * 1024})
	if err != nil {
		t.Fatalf("cross-package graph: %v", err)
	}
	callSite := int64(strings.Index(contents["p.go"], "BoxValue.Get()"))
	functionSite := int64(strings.Index(contents["p.go"], "sub.Fn("))
	importSite := int64(strings.Index(contents["p.go"], `"example.test/fixture/sub"`))
	gotCall, gotFunction, gotImport := false, false, false
	for _, relation := range graph.Relations {
		gotCall = gotCall || relation.Kind == "CALLS_DIRECT" && relation.SitePath == "p.go" && relation.Range.StartByte == callSite && relation.TargetSymbolID != nil && *relation.TargetSymbolID == targets["Get"]
		gotFunction = gotFunction || relation.Kind == "CALLS_DIRECT" && relation.SitePath == "p.go" && relation.Range.StartByte == functionSite && relation.TargetSymbolID != nil && *relation.TargetSymbolID == targets["Fn"]
		gotImport = gotImport || relation.Kind == "IMPORTS" && relation.SitePath == "p.go" && relation.Range.StartByte == importSite && relation.TargetSymbolID != nil
	}
	if !gotCall || !gotFunction || !gotImport {
		t.Fatalf("captured cross-package graph is missing call/import: method=%v function=%v import=%v", gotCall, gotFunction, gotImport)
	}
	runOverviewCapturedGraphFixture(t, ctx, typedRequest, contents)
	graphRequest.OtherPackages = nil
	boundary, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: graphRequest, MaxRelations: 2000, MaxOutputBytes: 256 * 1024})
	if err != nil {
		t.Fatalf("unselected dependency graph: %v", err)
	}
	unresolvedCall, unresolvedFunction, unresolvedImport := false, false, false
	for _, relation := range boundary.Relations {
		unresolvedCall = unresolvedCall || relation.Kind == "UNRESOLVED_CALL" && relation.SitePath == "p.go" && relation.Range.StartByte == callSite && relation.TargetSymbolID == nil
		unresolvedFunction = unresolvedFunction || relation.Kind == "UNRESOLVED_CALL" && relation.SitePath == "p.go" && relation.Range.StartByte == functionSite && relation.TargetSymbolID == nil
		unresolvedImport = unresolvedImport || relation.Kind == "UNRESOLVED_REF" && relation.SitePath == "p.go" && relation.Range.StartByte == importSite && relation.TargetSymbolID == nil
	}
	if !unresolvedCall || !unresolvedFunction || !unresolvedImport {
		t.Fatalf("unselected dependency did not remain a boundary: method=%v function=%v import=%v", unresolvedCall, unresolvedFunction, unresolvedImport)
	}
	again, err := AnalyzeTypedCaptured(ctx, typedRequest)
	if err != nil || !reflect.DeepEqual(typed, again) {
		t.Fatalf("local package bindings differ across loads: %v", err)
	}
	subRequest := typedRequest
	subRequest.Loader.PackageDir = "sub"
	subRequest.PackageKey = "fixture-sub"
	subRequest.OtherPackages = nil
	subResult, err := AnalyzeTypedCaptured(ctx, subRequest)
	if err != nil || !subResult.Loader.BuildSelectionVerified || len(subResult.Loader.Packages) != 1 || subResult.Loader.Packages[0].ImportPath != "example.test/fixture/sub" || len(subResult.Syntax) != 1 || subResult.Syntax[0].Path != "sub/value.go" {
		t.Fatalf("explicit subpackage selection: %+v %v", subResult, err)
	}
	subRequest.Loader.PackageDir = "other"
	if got, err := AnalyzeTypedCaptured(ctx, subRequest); err != nil || got.Loader.BuildSelectionVerified || len(got.Occurrences) != 0 || !containsReason(got.Reasons, "GO_IMPORTS_UNVERIFIED") {
		t.Fatalf("unapproved selected import was not partial: %+v %v", got, err)
	}
	subRequest.Loader.PackageDir = "../outside"
	if got, err := AnalyzeTypedCaptured(ctx, subRequest); !errors.Is(err, ErrLoaderInput) || len(got.Occurrences) != 0 {
		t.Fatalf("traversing package selection accepted: %+v %v", got, err)
	}
	typedRequest.OtherPackages[0].PackageKey = "fixture-package"
	if got, err := AnalyzeTypedCaptured(ctx, typedRequest); !errors.Is(err, ErrInputMismatch) || len(got.Occurrences) != 0 {
		t.Fatalf("duplicate package identity accepted: %+v %v", got, err)
	}
}

func runGuardedBuildSelectionFixture(t *testing.T, ctx context.Context, parent, goRoot string, tool *runtimeadapter.GoExecutable) {
	t.Helper()
	dir := filepath.Join(parent, "build-selection")
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	contents := map[string]string{
		"go.mod":              "module example.test/fixture\n\ngo 1.27.4\n",
		"base.go":             "package fixture\nvar Base int\n",
		"feature.go":          "//go:build demo\n\npackage fixture\nvar Feature int\n",
		"platform_linux.go":   "package fixture\nvar Linux int\n",
		"platform_windows.go": "package fixture\nvar Windows int\n",
		"ignored_test.go":     "package fixture\nimport _ \"example.test/never\"\n",
	}
	for name, value := range contents {
		if err := os.WriteFile(filepath.Join(dir, "input", name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := loaderPreflightFixture(t, contents)
	input.Profile.BuildTags = []string{"demo"}
	resetLoaderProfile(t, &input)
	input.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	input.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
	scratchRoot, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer scratchRoot.Close()
	request := CapturedLoaderInput{Preflight: input, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: filepath.Join(dir, "input"), Tool: tool, MaxOutputBytes: 65536,
		VerifyScratch: LoaderScratchVerifier(scratchRoot, func(context.Context) error { return nil })}
	result, err := LoadCapturedPackages(ctx, request)
	if err != nil || !result.BuildSelectionVerified || len(result.Packages) != 1 || strings.Join(result.Packages[0].Files, ",") != "base.go,feature.go,platform_linux.go" {
		t.Fatalf("Linux tagged selection: %+v %v", result, err)
	}
	request.Preflight.Profile.Goos = "windows"
	request.Preflight.Profile.BuildTags = []string{}
	resetLoaderProfile(t, &request.Preflight)
	result, err = LoadCapturedPackages(ctx, request)
	if err != nil || !result.BuildSelectionVerified || len(result.Packages) != 1 || strings.Join(result.Packages[0].Files, ",") != "base.go,platform_windows.go" {
		t.Fatalf("Windows profile selection: %+v %v", result, err)
	}
	fallback := fallbackRequest(request.Preflight)
	fallback.Loader = request
	fallback.Files[0].Path = "base.go"
	parsed, err := LoadOrAnalyzeCaptured(ctx, fallback)
	if err != nil || len(parsed.Syntax) != 1 || !parsed.Syntax[0].BuildSelectionVerified || parsed.Syntax[0].Package.SemanticStatus != "partial" {
		t.Fatalf("verified selection and syntax: %+v %v", parsed, err)
	}
	fallback.Files[0].Path = "platform_linux.go"
	if parsed, err := LoadOrAnalyzeCaptured(ctx, fallback); !errors.Is(err, ErrInputMismatch) || len(parsed.Syntax) != 0 {
		t.Fatalf("unselected file reported as selected: %+v %v", parsed, err)
	}
	typed := TypedInput{Loader: request, PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000, MaxOutputBytes: 65536}
	typedResult, err := AnalyzeTypedCaptured(ctx, typed)
	if err != nil || !typedResult.Loader.BuildSelectionVerified || len(typedResult.Syntax) != 2 || typedResult.Syntax[0].Path != "base.go" || typedResult.Syntax[1].Path != "platform_windows.go" {
		t.Fatalf("typed Windows profile selected wrong files: %+v %v", typedResult, err)
	}
	request.Preflight.Profile.TestsEnabled = true
	resetLoaderProfile(t, &request.Preflight)
	result, err = LoadCapturedPackages(ctx, request)
	if err != nil || result.BuildSelectionVerified || !containsReason(result.Reasons, "GO_TEST_VARIANTS_UNVERIFIED") {
		t.Fatalf("unverified test variants accepted: %+v %v", result, err)
	}
	typed.Loader = request
	if got, err := AnalyzeTypedCaptured(ctx, typed); err != nil || got.Loader.BuildSelectionVerified || len(got.Occurrences) != 0 || !containsReason(got.Reasons, "GO_TEST_VARIANTS_UNVERIFIED") {
		t.Fatalf("unverified test variants released typed facts: %+v %v", got, err)
	}
}
