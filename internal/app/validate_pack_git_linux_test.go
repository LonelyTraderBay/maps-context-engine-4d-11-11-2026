//go:build linux

package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// All Git and filesystem writes in this test are confined to a new temporary
// fixture. Current scope, dependency and actor proofs are fixture callbacks.
func TestFreshnessPackWithSyntheticGitWorkspace(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	repo, sandboxDir, templateDir, stateDir := filepath.Join(base, "repo"), filepath.Join(base, "sandbox"), filepath.Join(base, "template"), filepath.Join(base, "state")
	for _, dir := range []string{repo, sandboxDir, templateDir, stateDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(git, "init", "-q", repo)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TEMPLATE_DIR=" + templateDir, "GIT_TERMINAL_PROMPT=0"}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture git init: %v %s", err, output)
	}
	sourcePath := filepath.Join(repo, "main.go")
	if err := os.WriteFile(sourcePath, f.live.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	openRoot := func(name string) *paths.Root {
		t.Helper()
		root, err := paths.OpenRoot(name)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = root.Close() })
		return root
	}
	root, metadata, home, sandbox, state := openRoot(repo), openRoot(filepath.Join(repo, ".git")), openRoot(base), openRoot(sandboxDir), openRoot(stateDir)
	shared, err := runtimeadapter.AcquireLifecycle(f.ctx, state, f.scope.Members[0].WorktreeID, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtimeadapter.AcquireWriter(f.ctx, shared, "66666666-6666-4666-8666-666666666666", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = shared.Close() })
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	allow := func(context.Context) error { return nil }
	authorize := func(context.Context, string) error { return nil }
	if err := inventory.PrepareIgnoreSandbox(f.ctx, sandbox, allow); err != nil {
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
	config, err := inventory.InspectGitIgnoreConfig(f.ctx, git, plan, scanner, inventory.GitConfigBudget{MaxFileBytes: 4096, MaxTotalBytes: 8192, MaxOutputBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	inv := inventory.LocalInventoryRequest{Root: root, Config: config, Scanner: scanner, Sandbox: sandbox, GitBinary: git,
		IndexBudget:         inventory.GitIndexBudget{MaxBytes: 1 << 20, MaxEntries: 1000, MaxPathBytes: 4096, MaxDecodedPathBytes: 1 << 20},
		WalkBudget:          inventory.WalkBudget{MaxVisited: 1000, MaxDirectories: 100, MaxDepth: 16, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20},
		IgnoreCaptureBudget: inventory.IgnoreCaptureBudget{MaxFiles: 100, MaxFileBytes: 4096, MaxTotalBytes: 65536},
		IgnoreBudget:        inventory.IgnoreBudget{MaxSources: 100, MaxRules: 1000, MaxRuleBytes: 65536, MaxQueries: 1000, MaxQueryBytes: 65536, MaxOutputBytes: 65536},
		MaxFileMapEntries:   1000, AuthorizeRoot: allow, AuthorizePath: func(context.Context, string, bool) (bool, error) { return true, nil }, AuthorizeMetadata: authorize, AuthorizeSandbox: allow, Exclude: func(string, bool) bool { return false }}
	expected, err := inventory.InventoryLocal(f.ctx, inv)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := f.input.Validation.ResolveCapture(f.ctx, f.scope.Members[0])
	if err != nil {
		t.Fatal(err)
	}
	revalidate := indexer.RevalidateRequest{Inventory: inv, Expected: expected, Snapshot: captured, MaxManifestBytes: 65536, MaxFileBytes: 65536, MaxSourceBytes: 1024 * 1024,
		VerifyAuthority: func(context.Context, model.CaptureManifest) error { return nil }, VerifyDependencies: func(context.Context, []string) error { return nil }}
	f.input.Validation.VerifyWorkspace = func(ctx context.Context, _ model.ScopeMember, snap *snapshot.Snapshot) (WorkspaceObservation, error) {
		request := revalidate
		request.Snapshot = snap
		err := indexer.RevalidateLocal(ctx, request)
		if errors.Is(err, indexer.ErrSnapshotChanged) {
			return WorkspaceChanged, nil
		}
		if err != nil {
			return WorkspaceUnknown, err
		}
		return WorkspaceObservedCurrent, nil
	}
	f.input.CaptureLive = func(ctx context.Context, _ model.ScopeMember, path string) (snapshot.File, error) {
		return inventory.CaptureFile(ctx, root, scanner, path, 65536, authorize)
	}
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pack.First(f.ctx, freshnessOptions())
	if err != nil || page.Response.Pagination.NextCursor == nil {
		t.Fatalf("full workspace observation: %v", err)
	}
	requireFreshnessPage(t, page, "observed_current", "workspace")
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("package p\nfunc Alterd() { Target() }\n")
	if len(changed) != f.live.Len() {
		t.Fatal("fixture must preserve file size")
	}
	if err := os.WriteFile(sourcePath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sourcePath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got, err := pack.Continue(f.ctx, token, *page.Response.Pagination.NextCursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions()); !errors.Is(err, ErrFreshnessChanged) || got.Response.Data != nil {
		t.Fatalf("same-size same-mtime change leaked page: %v", err)
	}
	if err := os.WriteFile(sourcePath, f.live.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); err != nil || !result.Data.EditPreconditionValidated {
		t.Fatalf("restored workspace did not validate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.go"), []byte("package p\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrFreshnessChanged) || result.Data.EditPreconditionValidated {
		t.Fatalf("new untracked member was not detected: %v", err)
	}
}
