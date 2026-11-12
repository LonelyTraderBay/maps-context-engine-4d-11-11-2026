//go:build linux

package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestGitInventoryRejectsWorktreeRedirection(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{repo, outside} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, git, append([]string{"-C", repo}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git fixture: %v %s", e, out)
		}
	}
	run("init")
	if err = os.WriteFile(filepath.Join(repo, "inside.go"), []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(outside, "outside.go"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	run("config", "core.worktree", outside)
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := GitInventory(ctx, root, git, 1024)
	if errors.Is(err, ErrGitMetadataUnverified) {
		if result != nil {
			t.Fatal("unverified metadata returned data")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result, []byte("inside.go\x00")) || bytes.Contains(result, []byte("outside.go\x00")) {
		t.Fatalf("untrusted Git config changed worktree: %q", result)
	}
}

func TestGitInventoryRejectsSymlinkedMetadata(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{repo, outside} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, git, append([]string{"-C", outside}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git fixture: %v %s", e, out)
		}
	}
	run("init")
	if err = os.WriteFile(filepath.Join(outside, "outside-tracked.go"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "outside-tracked.go")
	if err = os.Symlink(filepath.Join(outside, ".git"), filepath.Join(repo, ".git")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := GitInventory(ctx, root, git, 1024)
	if err == nil || len(out) != 0 {
		t.Fatalf("untrusted metadata accepted: %q %v", out, err)
	}
}

func TestGitInventoryRejectsSymlinkedIndex(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{repo, outside} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(dir string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, git, append([]string{"-C", dir}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git fixture: %v %s", e, out)
		}
	}
	run(repo, "init")
	run(outside, "init")
	if err = os.WriteFile(filepath.Join(outside, "outside-index.go"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	run(outside, "add", "outside-index.go")
	if err = os.Symlink(filepath.Join(outside, ".git", "index"), filepath.Join(repo, ".git", "index")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := GitInventory(ctx, root, git, 1024)
	if err == nil || len(out) != 0 {
		t.Fatalf("untrusted index accepted: %q %v", out, err)
	}
}
