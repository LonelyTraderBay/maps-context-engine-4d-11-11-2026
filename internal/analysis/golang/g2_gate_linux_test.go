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

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// The gate fixture uses a disposable Git repository and state directory. It
// never executes the captured program, tests, generators, or target hooks.
type g2Fixture struct {
	base, repo, git, goRoot string
	root                    *paths.Root
	inventory               inventory.LocalInventoryRequest
	profile                 *model.GoProfile
	scanner                 *policy.Scanner
	tool                    *runtimeadapter.GoExecutable
}

func newG2Fixture(t *testing.T, ctx context.Context, contents map[string]string) *g2Fixture {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	sandboxPath := filepath.Join(base, "inventory-sandbox")
	for _, name := range []string{repo, sandboxPath} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("synthetic Git: %v %s", err, output)
		}
	}
	run("init", "-q", repo)
	for name, body := range contents {
		filename := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run("-c", "core.hooksPath=/dev/null", "-C", repo, "add", "--", "go.mod", "target.go", "sub/same.go")
	run("-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-C", repo, "commit", "-q", "-m", "synthetic gate fixture")
	run("-c", "core.hooksPath=/dev/null", "-C", repo, "update-index", "--assume-unchanged", "--", "target.go")
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	metadata, err := paths.OpenRoot(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metadata.Close() })
	home, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = home.Close() })
	sandbox, err := paths.OpenRoot(sandboxPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sandbox.Close() })
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	allow := func(context.Context) error { return nil }
	authorize := func(context.Context, string) error { return nil }
	if err := inventory.PrepareIgnoreSandbox(ctx, sandbox, allow); err != nil {
		t.Fatal(err)
	}
	source := func(root *paths.Root, name string) inventory.GitConfigSource {
		return inventory.GitConfigSource{Root: root, RelativePath: name, Authorize: authorize}
	}
	configPlan := inventory.GitConfigPlan{System: source(home, "system"), XDG: source(home, "xdg"), Global: source(home, "global"), Repository: source(metadata, "config"), VerifyDiscovery: func(context.Context) error { return errors.Join(root.Verify(), home.Verify(), metadata.Verify()) },
		ResolveGlobalIgnore: func(_ context.Context, value *string) (*inventory.ApprovedGlobalIgnore, error) {
			if value != nil {
				return nil, inventory.ErrGitConfigUnsupported
			}
			return &inventory.ApprovedGlobalIgnore{Root: home, RelativePath: "ignore", Authorize: authorize}, nil
		},
		ValidateWorktree: func(_ context.Context, fields inventory.GitConfigFields) error {
			if fields.Worktree != nil || fields.Bare != nil && *fields.Bare {
				return inventory.ErrGitConfigUnsupported
			}
			return nil
		}}
	config, err := inventory.InspectGitIgnoreConfig(ctx, git, configPlan, scanner, inventory.GitConfigBudget{MaxFileBytes: 4096, MaxTotalBytes: 8192, MaxOutputBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	in := inventory.LocalInventoryRequest{Root: root, Config: config, Scanner: scanner, Sandbox: sandbox, GitBinary: git,
		IndexBudget: inventory.GitIndexBudget{MaxBytes: 1 << 20, MaxEntries: 1000, MaxPathBytes: 4096, MaxDecodedPathBytes: 1 << 20}, WalkBudget: inventory.WalkBudget{MaxVisited: 1000, MaxDirectories: 100, MaxDepth: 16, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20},
		IgnoreCaptureBudget: inventory.IgnoreCaptureBudget{MaxFiles: 100, MaxFileBytes: 4096, MaxTotalBytes: 65536}, IgnoreBudget: inventory.IgnoreBudget{MaxSources: 100, MaxRules: 1000, MaxRuleBytes: 65536, MaxQueries: 1000, MaxQueryBytes: 65536, MaxOutputBytes: 65536}, MaxFileMapEntries: 1000,
		AuthorizeRoot: allow, AuthorizePath: func(context.Context, string, bool) (bool, error) { return true, nil }, AuthorizeMetadata: authorize, AuthorizeSandbox: allow, Exclude: func(string, bool) bool { return false }}
	goRoot := runtime.GOROOT()
	bin, err := paths.OpenRoot(filepath.Join(goRoot, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bin.Close() })
	goBytes, err := os.ReadFile(filepath.Join(goRoot, "bin", "go"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(goBytes)
	toolHash := hex.EncodeToString(hash[:])
	tool, err := runtimeadapter.OpenGoExecutable(ctx, runtimeadapter.GoExecutableInput{BinRoot: bin, ExpectedSHA256: toolHash, MaxBytes: 128 * 1024 * 1024, Authorize: allow})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tool.Close() })
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: toolHash, ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{}}
	return &g2Fixture{base: base, repo: repo, git: git, goRoot: goRoot, root: root, inventory: in, profile: profile, scanner: scanner, tool: tool}
}

func (f *g2Fixture) capture(t *testing.T, ctx context.Context) (*snapshot.Snapshot, inventory.FileMap) {
	t.Helper()
	fileMap, err := inventory.InventoryLocal(ctx, f.inventory)
	if err != nil {
		t.Fatal(err)
	}
	profileID, err := model.ProfileDigest(f.profile, 32768)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: profileID, PolicyDigest: strings.Repeat("c", 64), ProviderDigest: f.profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	var files []snapshot.File
	for _, entry := range fileMap.Entries {
		if entry.State != "candidate" || entry.Directory {
			t.Fatalf("unexpected fixture inventory state: %s", entry.State)
		}
		file, err := inventory.CaptureFile(ctx, f.root, f.scanner, entry.Path, 65536, func(context.Context, string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		hash, size := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: entry.Path, Status: "captured", ContentHash: &hash, ByteLength: &size})
		files = append(files, file)
	}
	captured, err := snapshot.Freeze(manifest, files, 32768, 65536)
	if err != nil {
		t.Fatal(err)
	}
	return captured, fileMap
}

func (f *g2Fixture) revalidate(captured *snapshot.Snapshot, files inventory.FileMap) indexer.RevalidateRequest {
	return indexer.RevalidateRequest{Inventory: f.inventory, Expected: files, Snapshot: captured, MaxManifestBytes: 32768, MaxFileBytes: 65536, MaxSourceBytes: 65536, VerifyAuthority: func(context.Context, model.CaptureManifest) error { return nil }, VerifyDependencies: func(context.Context, []string) error { return nil }}
}

func (f *g2Fixture) analyze(t *testing.T, ctx context.Context, captured *snapshot.Snapshot, fileMap inventory.FileMap, scratchName string, cache *SyntaxCache) GraphResult {
	t.Helper()
	scratchPath := filepath.Join(f.base, scratchName)
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.MkdirAll(filepath.Join(scratchPath, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range captured.Manifest().Files {
		file, ok := captured.File(item.Path)
		if !ok {
			t.Fatal("missing captured file")
		}
		filename := filepath.Join(scratchPath, "input", filepath.FromSlash(item.Path))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, file.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scratch, err := paths.OpenRoot(scratchPath)
	if err != nil {
		t.Fatal(err)
	}
	defer scratch.Close()
	preflight := LoaderPreflightInput{Snapshot: captured, Profile: f.profile, Inventory: fileMap, PolicyEpoch: 1, Scanner: f.scanner, MaxManifestBytes: 32768, MaxFileBytes: 65536, MaxSourceBytes: 65536,
		AuthorizeScope: func(context.Context, string, string, string, int64) error { return nil }, AuthorizePath: func(context.Context, string, string, string, int64) error { return nil },
		VerifyClosure: func(ctx context.Context, s *snapshot.Snapshot, _ inventory.FileMap) (bool, error) {
			request := f.revalidate(s, fileMap)
			if err := indexer.RevalidateLocal(ctx, request); err != nil {
				return false, err
			}
			return true, nil
		},
		VerifyToolchain: LoaderToolchainVerifier(f.tool, f.goRoot, func(_ context.Context, p *model.GoProfile) (bool, error) {
			return p.ToolchainDigest == f.profile.ToolchainDigest, nil
		})}
	request := TypedInput{Loader: CapturedLoaderInput{Preflight: preflight, GoRoot: f.goRoot, ScratchRoot: scratchPath, WorkspaceRoot: filepath.Join(scratchPath, "input"), Tool: f.tool, MaxOutputBytes: 65536, VerifyScratch: LoaderScratchVerifier(scratch, func(context.Context) error { return nil })},
		PackageKey: "root-package", ModuleKey: "fixture-module", OtherPackages: []TypedPackageIdentity{{ImportPath: "example.test/g2/sub", PackageKey: "sub-package", ModuleKey: "fixture-module"}}, SyntaxCache: cache, MaxNodes: 20000, MaxDepth: 256, MaxFacts: 3000, MaxOccurrences: 3000, MaxOutputBytes: 512 * 1024}
	graph, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: request, MaxRelations: 3000, MaxOutputBytes: 1024 * 1024})
	if err != nil || !graph.Typed.Loader.BuildSelectionVerified {
		t.Fatalf("gate graph analysis status=%s reasons=%v: %v", graph.Typed.Loader.Status, graph.Typed.Loader.Reasons, err)
	}
	return graph
}
