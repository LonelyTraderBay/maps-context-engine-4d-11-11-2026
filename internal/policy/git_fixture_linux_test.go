//go:build linux

package policy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This synthetic fixture demonstrates why the future Git adapter must override
// repository fsmonitor settings; it does not implement the adapter.
func TestGitFixtureFsmonitorIsSuppressedByExplicitOverride(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	template := filepath.Join(dir, "template")
	repo := filepath.Join(dir, "repo")
	for _, p := range []string{template, repo} {
		if err = os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(canary string, args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		flags := []string{"-C", repo, "-c", "init.templateDir=" + template}
		cmd := exec.CommandContext(ctx, git, append(flags, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "MCE_FIXTURE_CANARY=" + canary}
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Logf("fixture git error: %s", out)
		}
		return e
	}
	if err = run("", "init"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(repo, "source.txt"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = run("", "add", "source.txt"); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, "synthetic-fsmonitor.sh")
	if err = os.WriteFile(hook, []byte("#!/bin/sh\nprintf fixture > \"$MCE_FIXTURE_CANARY\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = run("", "config", "core.fsmonitor", hook); err != nil {
		t.Fatal(err)
	}
	unsafeMarker := filepath.Join(dir, "unsafe-marker")
	if err = run(unsafeMarker, "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(unsafeMarker); err != nil {
		t.Fatalf("fixture did not demonstrate unsanitized execution: %v", err)
	}
	safeMarker := filepath.Join(dir, "safe-marker")
	if err = run(safeMarker, "-c", "core.fsmonitor=false", "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(safeMarker); !os.IsNotExist(err) {
		t.Fatalf("fsmonitor ran despite override: %v", err)
	}
}
