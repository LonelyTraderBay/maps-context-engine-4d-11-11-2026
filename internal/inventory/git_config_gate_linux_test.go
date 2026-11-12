//go:build linux

package inventory

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestVerifiedGitConfigDetectsChangedOrRevokedSources(t *testing.T) {
	f := newGitFixture(t, "sha1")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := request.Config.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(f.home, "xdg-config")
	if err := os.WriteFile(newPath, []byte("[core]\n ignorecase = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := request.Config.Verify(ctx); !errors.Is(err, ErrGitConfigChanged) {
		t.Fatalf("new configuration accepted: %v", err)
	}
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}
	if err := request.Config.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(f.root, ".git", "config")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("\n[core]\n ignorecase = true\n"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := request.Config.Verify(ctx); !errors.Is(err, ErrGitConfigChanged) {
		t.Fatalf("changed repository configuration accepted: %v", err)
	}
}

func TestVerifiedGitConfigProtectsEffectiveFieldsFromCallbacks(t *testing.T) {
	f := newGitFixture(t, "sha1")
	if err := os.WriteFile(filepath.Join(f.home, "global-config"), []byte("[core]\n ignorecase = true\n excludesfile = approved-global/ignore\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	plan := request.Config.plan
	plan.ValidateWorktree = func(_ context.Context, fields GitConfigFields) error {
		if fields.IgnoreCase != nil {
			*fields.IgnoreCase = false
		}
		if fields.ObjectFormat != nil {
			*fields.ObjectFormat = "sha256"
		}
		return nil
	}
	approved := request.Config.global
	plan.ResolveGlobalIgnore = func(_ context.Context, value *string) (*ApprovedGlobalIgnore, error) {
		if value != nil {
			*value = "changed-by-callback"
		}
		copy := approved
		return &copy, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gate, err := InspectGitIgnoreConfig(ctx, f.git, plan, request.Scanner, request.Config.budget)
	if err != nil {
		t.Fatal(err)
	}
	if !gate.ignoreOptions.IgnoreCase || gate.objectHash != "sha1" || gate.effective.ExcludesFile == nil || *gate.effective.ExcludesFile != "approved-global/ignore" {
		t.Fatal("callback changed effective configuration")
	}
	if err := gate.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestVerifiedGitConfigRechecksCurrentGlobalAuthority(t *testing.T) {
	f := newGitFixture(t, "sha1")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	denied := errors.New("fixture global authority revoked")
	previous := request.Config.plan.ResolveGlobalIgnore
	request.Config.plan.ResolveGlobalIgnore = func(ctx context.Context, value *string) (*ApprovedGlobalIgnore, error) {
		global, err := previous(ctx, value)
		if err != nil {
			return nil, err
		}
		global.Authorize = func(context.Context, string) error { return denied }
		return global, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := request.Config.Verify(ctx); !errors.Is(err, denied) {
		t.Fatalf("revoked global authority accepted: %v", err)
	}
	if err := request.Config.authorizeGlobal(ctx, "approved-global/ignore"); !errors.Is(err, denied) {
		t.Fatalf("stale global authority used: %v", err)
	}
}

func TestInventoryRejectsConfigFromDifferentRepository(t *testing.T) {
	a := newGitFixture(t, "sha1")
	b := newGitFixture(t, "sha1")
	rootA, err := paths.OpenRoot(a.root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootA.Close()
	rootB, err := paths.OpenRoot(b.root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootB.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, a, rootA, sandbox)
	request.Root = rootB
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := InventoryLocal(ctx, request)
	if !errors.Is(err, ErrGitConfigUnsupported) || len(result.Entries) != 0 {
		t.Fatalf("different repository config accepted: %+v %v", result, err)
	}
}

func TestInventoryLocalUsesSha256Config(t *testing.T) {
	f := newGitFixture(t, "sha256")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
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
	if request.Config.objectHash != "sha256" || len(result.Entries) != 1 || result.Entries[0].Path != "tracked.go" {
		t.Fatalf("sha256 inventory: %+v", result)
	}
}

func TestInventoryLinkedBindsWorktreeConfig(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	linkedPath := filepath.Join(f.home, "linked-config")
	f.run(t, nil, "worktree", "add", "-q", "--detach", linkedPath, "HEAD")
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
	binding, err := runtime.BindLinkedGitMetadata(worktree, common, "worktrees/linked-config")
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	sandbox, _ := ignoreSandbox(t)
	request := localInventoryFixtureRequest(t, f, worktree, sandbox)
	f.run(t, nil, "config", "extensions.worktreeConfig", "true")
	worktreeConfigRoot, err := paths.OpenRoot(filepath.Join(f.root, ".git", "worktrees", "linked-config"))
	if err != nil {
		t.Fatal(err)
	}
	defer worktreeConfigRoot.Close()
	plan := request.Config.plan
	plan.Worktree = &GitConfigSource{Root: worktreeConfigRoot, RelativePath: "config.worktree", Authorize: func(context.Context, string) error { return nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gate, err := InspectGitIgnoreConfig(ctx, f.git, plan, request.Scanner, request.Config.budget)
	if err != nil {
		t.Fatal(err)
	}
	request.Config = gate
	result, err := InventoryLinked(ctx, request, binding, common)
	if err != nil || len(result.Entries) != 1 {
		t.Fatalf("valid linked config failed: %+v %v", result, err)
	}
	gate.plan.Worktree.Root = common
	if result, err := InventoryLinked(ctx, request, binding, common); !errors.Is(err, runtime.ErrGitMetadataUnverified) || len(result.Entries) != 0 {
		t.Fatalf("wrong worktree config root accepted: %+v %v", result, err)
	}
}

func TestInventoryEffectiveConfigMatchesGitWithAuthorizedFixtureSources(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	f.write(t, "keep.go")
	f.write(t, "UPPER.TMP")
	f.run(t, nil, "config", "core.ignorecase", "true")
	if err := os.MkdirAll(filepath.Join(f.home, ".config", "git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.home, "approved-global"), 0700); err != nil {
		t.Fatal(err)
	}
	ignorePath := filepath.Join(f.home, "approved-global", "ignore")
	files := map[string]string{
		"system-config":          "[core]\n ignorecase = false\n",
		".config/git/config":     "[core]\n ignorecase = true\n",
		".gitconfig":             "[core]\n ignorecase = false\n excludesfile = " + ignorePath + "\n",
		"approved-global/ignore": "*.tmp\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(f.home, filepath.FromSlash(name)), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	home, err := paths.OpenRoot(f.home)
	if err != nil {
		t.Fatal(err)
	}
	defer home.Close()
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	metadata, err := paths.OpenRoot(filepath.Join(f.root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	sandbox, _ := ignoreSandbox(t)
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string) error { return nil }
	source := func(base *paths.Root, name string) GitConfigSource {
		return GitConfigSource{Root: base, RelativePath: name, Authorize: authorize}
	}
	plan := GitConfigPlan{
		System: source(home, "system-config"), XDG: source(home, ".config/git/config"), Global: source(home, ".gitconfig"), Repository: source(metadata, "config"),
		VerifyDiscovery: func(context.Context) error { return errors.Join(home.Verify(), root.Verify(), metadata.Verify()) },
		ResolveGlobalIgnore: func(_ context.Context, value *string) (*ApprovedGlobalIgnore, error) {
			if value == nil || *value != ignorePath {
				return nil, ErrGitConfigUnsupported
			}
			return &ApprovedGlobalIgnore{Root: home, RelativePath: "approved-global/ignore", Authorize: authorize}, nil
		},
		ValidateWorktree: func(_ context.Context, fields GitConfigFields) error {
			if fields.Worktree != nil || fields.Bare != nil && *fields.Bare {
				return ErrGitConfigUnsupported
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gate, err := InspectGitIgnoreConfig(ctx, f.git, plan, scanner, GitConfigBudget{MaxFileBytes: 4096, MaxTotalBytes: 8192, MaxOutputBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	request := localInventoryFixtureRequest(t, f, root, sandbox)
	request.Config = gate
	result, err := InventoryLocal(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, item := range result.Entries {
		if item.Origin == "untracked" && item.State == "candidate" {
			actual = append(actual, item.Path)
		}
	}
	cmd := exec.CommandContext(ctx, f.git, "-C", f.root, "-c", "core.fsmonitor=false", "ls-files", "--others", "--exclude-standard", "-z")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + f.home, "XDG_CONFIG_HOME=" + filepath.Join(f.home, ".config"), "GIT_CONFIG_SYSTEM=" + filepath.Join(f.home, "system-config"), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0"}
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var expected []string
	for _, item := range bytes.Split(output, []byte{0}) {
		if len(item) > 0 {
			expected = append(expected, string(item))
		}
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if !gate.ignoreOptions.IgnoreCase || !reflect.DeepEqual(actual, expected) || strings.Join(actual, ",") != "keep.go" {
		t.Fatalf("effective ignore mismatch: got %v Git %v", actual, expected)
	}
}
