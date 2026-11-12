//go:build linux

package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// The Git mutations below are confined to revalidateFixture's synthetic repo.
func TestInvalidationSyntheticGitMembershipAndHeadChanges(t *testing.T) {
	base, sourcePath := revalidateFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := filepath.Dir(sourcePath)
	git := base.Inventory.GitBinary
	runGit := func(args ...string) {
		t.Helper()
		prefix := []string{"-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-C", repo}
		cmd := exec.CommandContext(ctx, git, append(prefix, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Dir(repo), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("synthetic Git command: %v %s", err, out)
		}
	}
	capture := func() *snapshot.Snapshot {
		t.Helper()
		fileMap, err := inventory.InventoryLocal(ctx, base.Inventory)
		if err != nil {
			t.Fatal(err)
		}
		manifest := base.Snapshot.Manifest()
		manifest.SnapshotDigest = ""
		manifest.Files = []model.ManifestFile{}
		var captured []snapshot.File
		for _, entry := range fileMap.Entries {
			switch entry.State {
			case "candidate":
				file, err := inventory.CaptureFile(ctx, base.Inventory.Root, base.Inventory.Scanner, entry.Path, 4096, func(ctx context.Context, path string) error { return authorizeSource(ctx, base.Inventory, path) })
				if err != nil {
					t.Fatal(err)
				}
				hash, size := file.ContentHash(), int64(file.Len())
				manifest.Files = append(manifest.Files, model.ManifestFile{Path: entry.Path, Status: "captured", ContentHash: &hash, ByteLength: &size})
				captured = append(captured, file)
			case "missing":
				reason := "tracked_file_missing"
				manifest.Files = append(manifest.Files, model.ManifestFile{Path: entry.Path, Status: "missing", ReasonCode: &reason})
			default:
				t.Fatalf("unexpected synthetic inventory state: %s", entry.State)
			}
		}
		result, err := snapshot.Freeze(manifest, captured, 8192, 65536)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := capture() // initial unborn HEAD
	runGit("add", "--", "main.go")
	runGit("commit", "-q", "-m", "synthetic fixture")
	committed := capture()
	_, request := invalidationRequest(t, before, committed)
	request.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) { return true, nil }
	if plan, err := PlanInvalidation(ctx, request); err != nil || plan.Action != "noop" {
		t.Fatalf("HEAD alone altered captured identity: %+v %v", plan, err)
	}
	runGit("switch", "-q", "--detach", "HEAD")
	detached := capture()
	request.Before, request.After = committed, detached
	if plan, err := PlanInvalidation(ctx, request); err != nil || plan.Action != "noop" {
		t.Fatalf("detached HEAD alone altered captured identity: %+v %v", plan, err)
	}
	runGit("update-index", "--assume-unchanged", "--", "main.go")
	observed, err := inventory.InventoryLocal(ctx, base.Inventory)
	if err != nil {
		t.Fatal(err)
	}
	assumeUnchanged := false
	for _, item := range observed.Entries {
		assumeUnchanged = assumeUnchanged || item.Path == "main.go" && item.AssumeUnchanged
	}
	if !assumeUnchanged {
		t.Fatal("synthetic Git index did not record assume-unchanged")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("package new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sourcePath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "added.go"), []byte("package added\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dirty := capture()
	request.Before, request.After = detached, dirty
	plan, err := PlanInvalidation(ctx, request)
	if err != nil || plan.Action != "rebuild" || len(plan.Changes) != 2 || plan.Changes[0].Path != "added.go" || plan.Changes[0].Kind != "added" || plan.Changes[1].Path != "main.go" || plan.Changes[1].Kind != "modified" {
		t.Fatalf("dirty bytes or untracked membership missed: %+v %v", plan, err)
	}
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo, "added.go"), filepath.Join(repo, "moved.go")); err != nil {
		t.Fatal(err)
	}
	removed := capture()
	request.Before, request.After = dirty, removed
	plan, err = PlanInvalidation(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var changes []string
	for _, item := range plan.Changes {
		changes = append(changes, item.Path+":"+item.Kind)
	}
	if !reflect.DeepEqual(changes, []string{"added.go:removed", "main.go:modified", "moved.go:added"}) || plan.Action != "rebuild" {
		t.Fatalf("delete/rename membership missed: %+v", changes)
	}
}
