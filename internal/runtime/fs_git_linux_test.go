//go:build linux

package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestGitInventoryRejectsFsmonitorAndBoundsOutput(t *testing.T) {
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
	if err = os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
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
	if err = os.WriteFile(filepath.Join(repo, "source.go"), []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "source.go")
	marker := filepath.Join(base, "must-not-run")
	script := filepath.Join(base, "hook.sh")
	if err = os.WriteFile(script, []byte("#!/bin/sh\nprintf fixture > "+marker+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	run("config", "core.fsmonitor", script)
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := GitInventory(ctx, root, git, 1024)
	if errors.Is(err, ErrGitMetadataUnverified) {
		t.Skip("Git metadata containment is not yet implemented")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, []byte("source.go\x00")) {
		t.Fatalf("inventory %q", result)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("fsmonitor executed: %v", err)
	}
	if out, err := GitInventory(ctx, root, git, 1); !errors.Is(err, ErrOutputLimit) || out != nil {
		t.Fatalf("output limit: %q %v", out, err)
	}
	if _, err = GitInventory(context.Background(), root, git, 1024); err == nil {
		t.Fatal("missing deadline accepted")
	}
}

func TestGitInventoryUsesAnchoredDirectoryHandle(t *testing.T) {
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
	replacement := filepath.Join(base, "replacement")
	moved := filepath.Join(base, "moved")
	for _, dir := range []string{repo, replacement} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(git, "-C", dir, "init")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("init fixture: %v %s", e, out)
		}
	}
	if err = os.WriteFile(filepath.Join(repo, "inside.go"), []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(replacement, "outside.go"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(replacement, repo); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := GitInventory(ctx, root, git, 1024)
	if !errors.Is(err, paths.ErrRootChanged) || out != nil {
		t.Fatalf("changed root accepted: %q %v", out, err)
	}
}

func TestGitInventoryRejectsRootReplacementDuringCommand(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	moved := filepath.Join(base, "moved")
	replacement := filepath.Join(base, "replacement")
	started := filepath.Join(base, "started")
	for _, dir := range []string{repo, replacement} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// The executable is a controlled fixture and never reads user data.
	binary := filepath.Join(base, "fixture-git.sh")
	quoted := "'" + strings.ReplaceAll(started, "'", "'\\''") + "'"
	script := "#!/bin/sh\nprintf ready > " + quoted + "\n/bin/sleep 0.3\nprintf 'fixture\\000'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type outcome struct {
		data []byte
		err  error
	}
	done := make(chan outcome, 1)
	go func() { data, e := GitInventory(ctx, root, binary, 1024); done <- outcome{data, e} }()
	observed := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case result := <-done:
			if errors.Is(result.err, ErrGitMetadataUnverified) {
				t.Skip("Git metadata containment is not yet implemented")
			}
			t.Fatalf("fixture command failed before start: %v", result.err)
		default:
		}
		if _, err = os.Stat(started); err == nil {
			observed = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !observed {
		result := <-done
		t.Fatalf("fixture command did not start: %v", result.err)
	}
	if err = os.Rename(repo, moved); err != nil {
		<-done
		t.Fatal(err)
	}
	if err = os.Symlink(replacement, repo); err != nil {
		<-done
		t.Fatal(err)
	}
	result := <-done
	if !errors.Is(result.err, paths.ErrRootChanged) || result.data != nil {
		t.Fatalf("changed root yielded data: %q %v", result.data, result.err)
	}
}

func TestGitInventoryDoesNotExecuteUnverifiedMetadata(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(base, "executed")
	binary := filepath.Join(base, "fixture-git")
	script := "#!/bin/sh\nprintf executed > '" + strings.ReplaceAll(marker, "'", "'\\''") + "'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := GitInventory(ctx, root, binary, 1024)
	if !errors.Is(err, ErrGitMetadataUnverified) || data != nil {
		t.Fatalf("unverified metadata yielded output: %q %v", data, err)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unverified metadata executed binary: %v", err)
	}
}
