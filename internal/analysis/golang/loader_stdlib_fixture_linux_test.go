//go:build linux

package golang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// This synthetic fixture exercises the actual pinned local toolchain. The
// dependency and toolchain proof callbacks are test doubles, not attestations.
func TestGuardedPackagesLoadTrustedStdlibFixture(t *testing.T) {
	if os.Getenv("MCE_STDLIB_FIXTURE_CHILD") == "1" {
		runStdlibFixture(t, os.Getenv("MCE_STDLIB_FIXTURE_DIR"))
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestGuardedPackagesLoadTrustedStdlibFixture$")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + filepath.Join(runtime.GOROOT(), "bin"), "GOMAXPROCS=4", "MCE_STDLIB_FIXTURE_CHILD=1", "MCE_STDLIB_FIXTURE_DIR=" + dir,
		"GOPACKAGESDEBUG=1", "GOPACKAGESDRIVER=/untrusted/driver", "GOFLAGS=-toolexec=/untrusted/tool", "GOPROXY=https://untrusted.invalid"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stdlib fixture: %v: %s", err, output)
	}
	if strings.Contains(string(output), dir) {
		t.Fatal("loader debug output leaked fixture path")
	}
}

func runStdlibFixture(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	contents := map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "p.go": "package fixture\nimport \"fmt\"\nvar Value = fmt.Sprint(1)\n"}
	for name, content := range contents {
		if err := os.WriteFile(filepath.Join(dir, "input", name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := loaderPreflightFixture(t, contents)
	digest := strings.Repeat("d", 64)
	addLoaderFixtureDependency(t, &input, digest)
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
	input.VerifyDependencies = func(_ context.Context, _ *model.GoProfile, digests []string) (bool, error) {
		return len(digests) == 1 && digests[0] == digest, nil
	}
	workspace := filepath.Join(dir, "input")
	env, _, err := LoaderEnvironment(input.Profile, goRoot, dir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	list := exec.CommandContext(ctx, filepath.Join(goRoot, "bin", "go"), "list", "-deps", "-f", "{{if .Standard}}{{.ImportPath}}{{end}}", "fmt")
	list.Dir, list.Env = workspace, env
	output, err := list.Output()
	if err != nil {
		t.Fatalf("fixture package inventory: %v", err)
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
	stdlib, err := NewTrustedStdlib(ctx, TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: goRoot, Profile: input.Profile, Snapshot: input.Snapshot, DependencyDigest: digest, AllowedPackages: approved,
		Scanner: scanner, MaxManifestBytes: 32768, MaxFileBytes: 2 * 1024 * 1024, MaxFiles: 8192, MaxSourceBytes: 64 * 1024 * 1024,
		AuthorizePath:   func(context.Context, string) error { return nil },
		VerifyAuthority: func(context.Context, string, string, string, []string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	scratchRoot, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer scratchRoot.Close()
	request := CapturedLoaderInput{Preflight: input, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace, Tool: tool, Stdlib: stdlib, MaxOutputBytes: 65536,
		VerifyScratch: LoaderScratchVerifier(scratchRoot, func(context.Context) error { return nil })}
	result, err := LoadCapturedPackages(ctx, request)
	if err != nil || !result.BuildSelectionVerified || len(result.Packages) != 1 || result.Packages[0].ImportPath != "example.test/fixture" || len(result.Packages[0].Files) != 1 || result.Packages[0].Files[0] != "p.go" {
		t.Fatalf("stdlib load: %+v %v", result, err)
	}
	result, err = LoadCapturedPackages(ctx, request)
	if err != nil || result.BuildSelectionVerified || !containsReason(result.Reasons, "STDLIB_SCOPE_REUSED") {
		t.Fatalf("reused stdlib scope: %+v %v", result, err)
	}
	checks := 0
	revoked, err := NewTrustedStdlib(ctx, TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: goRoot, Profile: input.Profile, Snapshot: input.Snapshot, DependencyDigest: digest, AllowedPackages: approved,
		Scanner: scanner, MaxManifestBytes: 32768, MaxFileBytes: 2 * 1024 * 1024, MaxFiles: 8192, MaxSourceBytes: 64 * 1024 * 1024,
		AuthorizePath: func(context.Context, string) error { return nil },
		VerifyAuthority: func(context.Context, string, string, string, []string) (bool, error) {
			checks++
			return checks < 3, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	request.Stdlib = revoked
	result, err = LoadCapturedPackages(ctx, request)
	if err != nil || result.BuildSelectionVerified || len(result.Packages) != 0 || !containsReason(result.Reasons, "STDLIB_CHANGED") {
		t.Fatalf("revoked stdlib returned facts: %+v %v", result, err)
	}
	missing, err := NewTrustedStdlib(ctx, TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: goRoot, Profile: input.Profile, Snapshot: input.Snapshot, DependencyDigest: digest, AllowedPackages: []string{"unsafe"},
		Scanner: scanner, MaxManifestBytes: 32768, MaxFileBytes: 2 * 1024 * 1024, MaxFiles: 8192, MaxSourceBytes: 64 * 1024 * 1024,
		AuthorizePath:   func(context.Context, string) error { return nil },
		VerifyAuthority: func(context.Context, string, string, string, []string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	request.Stdlib = missing
	result, err = LoadCapturedPackages(ctx, request)
	if err != nil || result.BuildSelectionVerified || !containsReason(result.Reasons, "GO_IMPORTS_UNVERIFIED") {
		t.Fatalf("unapproved standard import accepted: %+v %v", result, err)
	}
	newScope := func(authority StdlibAuthority) *TrustedStdlib {
		t.Helper()
		value, err := NewTrustedStdlib(ctx, TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: goRoot, Profile: input.Profile, Snapshot: input.Snapshot, DependencyDigest: digest, AllowedPackages: approved,
			Scanner: scanner, MaxManifestBytes: 32768, MaxFileBytes: 2 * 1024 * 1024, MaxFiles: 8192, MaxSourceBytes: 64 * 1024 * 1024,
			AuthorizePath: func(context.Context, string) error { return nil }, VerifyAuthority: authority})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	request.Stdlib = newScope(func(context.Context, string, string, string, []string) (bool, error) { return true, nil })
	typedRequest := TypedInput{Loader: request, PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000, MaxOutputBytes: 65536}
	typed, err := AnalyzeTypedCaptured(ctx, typedRequest)
	if err != nil || !typed.Loader.BuildSelectionVerified || len(typed.Syntax) != 1 || containsReason(typed.Reasons, "LOCAL_BINDING_UNMAPPED") {
		t.Fatalf("typed stdlib fixture: %+v %v", typed, err)
	}
	foundExternal := false
	for _, occurrence := range typed.Occurrences {
		if occurrence.Name == "Sprint" && occurrence.Kind == "use" {
			foundExternal = occurrence.Resolution == "external" && occurrence.SymbolID == nil
		}
	}
	raw, err := json.Marshal(typed)
	if err != nil || !foundExternal || strings.Contains(string(raw), goRoot) {
		t.Fatal("stdlib boundary exposed private toolchain source or lost the reference")
	}
	checks = 0
	typedRequest.Loader.Stdlib = newScope(func(context.Context, string, string, string, []string) (bool, error) {
		checks++
		return checks < 4, nil
	})
	if got, err := AnalyzeTypedCaptured(ctx, typedRequest); err != nil || got.Loader.BuildSelectionVerified || len(got.Occurrences) != 0 || !containsReason(got.Reasons, "STDLIB_CHANGED") {
		t.Fatalf("stdlib revocation after typed extraction released facts: %+v %v", got, err)
	}
}
