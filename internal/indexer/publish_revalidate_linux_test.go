//go:build linux

package indexer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func revalidateFixture(t *testing.T) (RevalidateRequest, string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	sandboxPath := filepath.Join(base, "sandbox")
	for _, name := range []string{repo, sandboxPath} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(git, "init", "-q", repo)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture git init: %v %s", err, output)
	}
	path := filepath.Join(repo, "main.go")
	if err := os.WriteFile(path, []byte("package old\n"), 0600); err != nil {
		t.Fatal(err)
	}
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
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	allow := func(context.Context) error { return nil }
	authorize := func(context.Context, string) error { return nil }
	if err := inventory.PrepareIgnoreSandbox(ctx, sandbox, allow); err != nil {
		t.Fatal(err)
	}
	source := func(root *paths.Root, name string) inventory.GitConfigSource {
		return inventory.GitConfigSource{Root: root, RelativePath: name, Authorize: authorize}
	}
	plan := inventory.GitConfigPlan{System: source(home, "system"), XDG: source(home, "xdg"), Global: source(home, "global"), Repository: source(metadata, "config"),
		VerifyDiscovery: func(context.Context) error { return errors.Join(root.Verify(), home.Verify(), metadata.Verify()) },
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
	config, err := inventory.InspectGitIgnoreConfig(ctx, git, plan, scanner, inventory.GitConfigBudget{MaxFileBytes: 4096, MaxTotalBytes: 8192, MaxOutputBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	in := inventory.LocalInventoryRequest{Root: root, Config: config, Scanner: scanner, Sandbox: sandbox, GitBinary: git,
		IndexBudget:         inventory.GitIndexBudget{MaxBytes: 1 << 20, MaxEntries: 1000, MaxPathBytes: 4096, MaxDecodedPathBytes: 1 << 20},
		WalkBudget:          inventory.WalkBudget{MaxVisited: 1000, MaxDirectories: 100, MaxDepth: 16, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20},
		IgnoreCaptureBudget: inventory.IgnoreCaptureBudget{MaxFiles: 100, MaxFileBytes: 4096, MaxTotalBytes: 65536},
		IgnoreBudget:        inventory.IgnoreBudget{MaxSources: 100, MaxRules: 1000, MaxRuleBytes: 65536, MaxQueries: 1000, MaxQueryBytes: 65536, MaxOutputBytes: 65536},
		MaxFileMapEntries:   1000, AuthorizeRoot: allow, AuthorizePath: func(context.Context, string, bool) (bool, error) { return true, nil }, AuthorizeMetadata: authorize, AuthorizeSandbox: allow, Exclude: func(string, bool) bool { return false }}
	fileMap, err := inventory.InventoryLocal(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	file, err := snapshot.NewFile("main.go", []byte("package old\n"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{{Path: "main.go", Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	frozen, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 4096)
	if err != nil {
		t.Fatal(err)
	}
	return RevalidateRequest{Inventory: in, Expected: fileMap, Snapshot: frozen, MaxManifestBytes: 8192, MaxFileBytes: 4096, MaxSourceBytes: 4096,
		VerifyAuthority: func(context.Context, model.CaptureManifest) error { return nil }, VerifyDependencies: func(context.Context, []string) error { return nil }}, path
}

func TestRevalidateLocalDetectsChangedBytesWithSameSizeAndMtime(t *testing.T) {
	request, path := revalidateFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RevalidateLocal(ctx, request); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := RevalidateLocal(ctx, request); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("changed bytes accepted: %v", err)
	}
}

func TestRevalidateLocalRejectsNewUntrackedMember(t *testing.T) {
	request, path := revalidateFixture(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "new.go"), []byte("package new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RevalidateLocal(ctx, request); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("new member accepted: %v", err)
	}
}

func TestRevalidateLocalRejectsRevokedAuthority(t *testing.T) {
	request, _ := revalidateFixture(t)
	calls := 0
	request.VerifyAuthority = func(context.Context, model.CaptureManifest) error {
		calls++
		if calls == 2 {
			return inventory.ErrWalkAuthorityChanged
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RevalidateLocal(ctx, request); !errors.Is(err, inventory.ErrWalkAuthorityChanged) {
		t.Fatalf("revoked scope accepted: %v", err)
	}
}

func TestRevalidateLocalDetectsChangeBetweenHashPasses(t *testing.T) {
	request, path := revalidateFixture(t)
	calls := 0
	request.VerifyDependencies = func(context.Context, []string) error {
		calls++
		if calls == 2 {
			return os.WriteFile(path, []byte("package new\n"), 0600)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RevalidateLocal(ctx, request); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("change between hash passes accepted: %v", err)
	}
}

func TestObserveCapturedLocalChecksSourceAuthorityAfterRead(t *testing.T) {
	request, _ := revalidateFixture(t)
	checks := 0
	request.AuthorizeSourceRoot = func(context.Context) error {
		checks++
		if checks == 2 {
			return inventory.ErrWalkAuthorityChanged
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ObserveCapturedLocal(ctx, request); !errors.Is(err, inventory.ErrWalkAuthorityChanged) || checks != 2 {
		t.Fatalf("source proof must check both sides of the read: checks=%d err=%v", checks, err)
	}
	if err := RevalidateLocal(ctx, request); err != nil || checks != 2 {
		t.Fatalf("publication must use its own authority: checks=%d err=%v", checks, err)
	}
}

func TestObserveCapturedLocalUsesPinnedInventoryDigest(t *testing.T) {
	request, path := revalidateFixture(t)
	raw, ok := request.Snapshot.File("main.go")
	if !ok {
		t.Fatal("fixture capture missing")
	}
	manifest := request.Snapshot.Manifest()
	manifest.DependencyInputDigests = []string{request.Expected.InputDigest}
	manifest.SnapshotDigest = ""
	frozen, err := snapshot.Freeze(manifest, []snapshot.File{raw}, request.MaxManifestBytes, request.MaxSourceBytes)
	if err != nil {
		t.Fatal(err)
	}
	request.Snapshot = frozen
	request.Expected = inventory.FileMap{}
	request.MatchManifestInventory = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ObserveCapturedLocal(ctx, request); err != nil {
		t.Fatalf("matching pinned inventory refused: %v", err)
	}
	if err := RevalidateLocal(ctx, request); !errors.Is(err, ErrSnapshotUnverifiable) {
		t.Fatalf("publication accepted derived inventory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".git", "info", "exclude"), []byte("# changed input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ObserveCapturedLocal(ctx, request); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("changed inventory input accepted: %v", err)
	}
}
