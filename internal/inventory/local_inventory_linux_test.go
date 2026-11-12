//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func localInventoryFixtureRequest(t *testing.T, f *gitFixture, root, sandbox *paths.Root) LocalInventoryRequest {
	t.Helper()
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	home, err := paths.OpenRoot(f.home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = home.Close() })
	metadata, err := paths.OpenRoot(filepath.Join(f.root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metadata.Close() })
	authorize := func(context.Context, string) error { return nil }
	source := func(base *paths.Root, path string) GitConfigSource {
		return GitConfigSource{Root: base, RelativePath: path, Authorize: authorize}
	}
	plan := GitConfigPlan{
		System: source(home, "system-config"), XDG: source(home, "xdg-config"), Global: source(home, "global-config"), Repository: source(metadata, "config"),
		Worktree:        &GitConfigSource{Root: metadata, RelativePath: "config.worktree", Authorize: authorize},
		VerifyDiscovery: func(context.Context) error { return nil },
		ResolveGlobalIgnore: func(_ context.Context, value *string) (*ApprovedGlobalIgnore, error) {
			if value != nil && *value != "approved-global/ignore" {
				return nil, ErrGitConfigUnsupported
			}
			return &ApprovedGlobalIgnore{Root: home, RelativePath: "approved-global/ignore", Authorize: authorize}, nil
		},
		ValidateWorktree: func(_ context.Context, value GitConfigFields) error {
			if value.Worktree != nil || value.Bare != nil && *value.Bare {
				return ErrGitConfigUnsupported
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config, err := InspectGitIgnoreConfig(ctx, f.git, plan, scanner, GitConfigBudget{MaxFileBytes: 4096, MaxTotalBytes: 8192, MaxOutputBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	return LocalInventoryRequest{
		Root: root, Config: config, IndexBudget: indexFixtureBudget(), WalkBudget: walkBudget(), IgnoreCaptureBudget: ignoreCaptureBudget(), IgnoreBudget: ignoreBudget(), MaxFileMapEntries: 1000,
		Scanner: scanner, Sandbox: sandbox, GitBinary: f.git,
		AuthorizeRoot:     func(context.Context) error { return nil },
		AuthorizePath:     func(context.Context, string, bool) (bool, error) { return true, nil },
		AuthorizeMetadata: func(context.Context, string) error { return nil },
		AuthorizeSandbox:  func(context.Context) error { return nil },
		Exclude:           func(path string, dir bool) bool { return dir && path == "vendor" },
	}
}

func TestInventoryLocalCombinesGitIgnoreAndDiskWithoutTargetConfigExecution(t *testing.T) {
	f := newGitFixture(t, "sha1")
	for _, name := range []string{"tracked.go", "tracked.log", "gone.go", "new.go", "ignored.log", "ignored.tmp", "vendor/lib.go", ".env"} {
		f.write(t, name)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".gitignore"), []byte("*.log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.run(t, nil, "add", "--", "tracked.go", "gone.go")
	f.run(t, nil, "add", "-f", "--", "tracked.log")
	if err := os.Remove(filepath.Join(f.root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(f.home, "must-not-run")
	hook := filepath.Join(f.home, "fsmonitor.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf x > '"+strings.ReplaceAll(marker, "'", "'\\''")+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	f.run(t, nil, "config", "core.fsmonitor", hook)
	globalDir := filepath.Join(f.home, "approved-global")
	if err := os.Mkdir(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "ignore"), []byte("*.tmp\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.home, "global-config"), []byte("[core]\n excludesfile = approved-global/ignore\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := InventoryLocal(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]FileMapEntry{}
	for _, item := range result.Entries {
		items[item.Path] = item
	}
	for name, state := range map[string]string{"tracked.go": "candidate", "tracked.log": "candidate", "gone.go": "missing", "new.go": "candidate", "vendor": "excluded", ".gitignore": "candidate"} {
		if item, ok := items[name]; !ok || item.State != state {
			t.Fatalf("%s: %+v", name, item)
		}
	}
	for _, name := range []string{"ignored.log", "ignored.tmp", ".env", "vendor/lib.go"} {
		if _, ok := items[name]; ok {
			t.Fatalf("excluded path appeared: %s", name)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target fsmonitor executed: %v", err)
	}
}

func TestInventoryLocalRejectsChangedMembershipBetweenPasses(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	checks := 0
	request.Config.plan.VerifyDiscovery = func(context.Context) error {
		checks++
		if checks == 7 {
			return os.WriteFile(filepath.Join(f.root, "new.go"), []byte("fixture"), 0600)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := InventoryLocal(ctx, request)
	if !errors.Is(err, ErrWalkChanged) || len(result.Entries) != 0 {
		t.Fatalf("changed membership accepted: %+v %v", result, err)
	}
}

func TestInventoryLocalRequiresVerifiedConfigAndIgnoreAuthority(t *testing.T) {
	f := newGitFixture(t, "sha1")
	if err := os.WriteFile(filepath.Join(f.root, ".gitignore"), []byte("*.log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	config := request.Config
	request.Config = nil
	if _, err := InventoryLocal(ctx, request); err == nil {
		t.Fatal("missing config verification accepted")
	}
	request.Config = config
	request.AuthorizePath = func(_ context.Context, name string, _ bool) (bool, error) { return name != ".gitignore", nil }
	if result, err := InventoryLocal(ctx, request); !errors.Is(err, ErrIgnoreUnsupported) || len(result.Entries) != 0 {
		t.Fatalf("denied ignore source accepted: %+v %v", result, err)
	}
}

func TestInventoryLinkedUsesBoundWorktreeAndCommonIgnore(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	linkedPath := filepath.Join(f.home, "linked")
	f.run(t, nil, "worktree", "add", "-q", "--detach", linkedPath, "HEAD")
	if err := os.WriteFile(filepath.Join(linkedPath, "new.go"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedPath, "ignored.tmp"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".git", "info", "exclude"), []byte("*.tmp\n"), 0600); err != nil {
		t.Fatal(err)
	}
	worktree, err := paths.OpenRoot(linkedPath)
	if err != nil {
		t.Fatal(err)
	}
	defer worktree.Close()
	common, err := paths.OpenRoot(filepath.Join(f.root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	binding, err := runtime.BindLinkedGitMetadata(worktree, common, "worktrees/linked")
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, worktree, sandbox)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := InventoryLinked(ctx, request, binding, common)
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]FileMapEntry{}
	for _, item := range result.Entries {
		items[item.Path] = item
	}
	if items["tracked.go"].Origin != "tracked" || items["new.go"].Origin != "untracked" {
		t.Fatalf("linked inventory: %+v", result.Entries)
	}
	if _, ok := items["ignored.tmp"]; ok {
		t.Fatal("common info/exclude was ignored")
	}
	other, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if result, err := InventoryLinked(ctx, request, binding, other); !errors.Is(err, runtime.ErrGitMetadataUnverified) || len(result.Entries) != 0 {
		t.Fatalf("unbound common root accepted: %+v %v", result, err)
	}
}

func TestInventoryObservesUnbornRenameAndDetachedReset(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "old.go")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assertMap := func(want map[string]string) {
		t.Helper()
		result, err := InventoryLocal(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, item := range result.Entries {
			got[item.Path] = item.Origin + ":" + item.State
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("inventory got %v, want %v", got, want)
		}
	}
	assertMap(map[string]string{"old.go": "untracked:candidate"})
	f.run(t, nil, "add", "--", "old.go")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "first")
	first := strings.TrimSpace(string(f.run(t, nil, "rev-parse", "HEAD")))
	if err := os.Rename(filepath.Join(f.root, "old.go"), filepath.Join(f.root, "new.go")); err != nil {
		t.Fatal(err)
	}
	assertMap(map[string]string{"old.go": "tracked:missing", "new.go": "untracked:candidate"})
	f.run(t, nil, "add", "-A")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "second")
	second := strings.TrimSpace(string(f.run(t, nil, "rev-parse", "HEAD")))
	assertMap(map[string]string{"new.go": "tracked:candidate"})
	f.run(t, nil, "checkout", "-q", "--detach", first)
	assertMap(map[string]string{"old.go": "tracked:candidate"})
	f.run(t, nil, "reset", "-q", "--hard", second)
	assertMap(map[string]string{"new.go": "tracked:candidate"})
}

func TestInventorySparseIndexKeepsDirectoryBoundary(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "a/inside.go")
	f.write(t, "b/outside.go")
	f.run(t, nil, "add", "--", "a/inside.go", "b/outside.go")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	f.run(t, nil, "sparse-checkout", "init", "--cone", "--sparse-index")
	f.run(t, nil, "sparse-checkout", "set", "a")
	f.write(t, "b/must-not-traverse.go")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	traversed := false
	request.AuthorizePath = func(_ context.Context, name string, _ bool) (bool, error) {
		if name == "b/must-not-traverse.go" {
			traversed = true
		}
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := InventoryLocal(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]FileMapEntry{}
	for _, item := range result.Entries {
		items[item.Path] = item
	}
	if traversed || items["b"].State != "sparse" || items["a/inside.go"].State != "candidate" {
		t.Fatalf("sparse boundary traversed=%v entries=%+v", traversed, result.Entries)
	}
}
