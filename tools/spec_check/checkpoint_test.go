package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckpointReceiptUsesActualGitObjects(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "empty-template")
	if err := os.Mkdir(template, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "-c", "init.templateDir=" + template}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(dir, "missing-gitconfig"), "GIT_TERMINAL_PROMPT=0", "TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir}
		if systemRoot := os.Getenv("SYSTEMROOT"); systemRoot != "" {
			cmd.Env = append(cmd.Env, "SYSTEMROOT="+systemRoot)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "source.txt")
	tree := run("write-tree")
	tasks := "T001,T002,T003,T105,T106,T107,T108,T005,T004,T006"
	tag := "mce4d/checkpoints/B001"
	message := fmt.Sprintf("fixture checkpoint\n\nMCE4D-Batch: B001\nMCE4D-Batch-Type: full\nMCE4D-Task-Count: 10\nMCE4D-Tasks: %s\nMCE4D-Spec-Version: 1.6.2\nMCE4D-Tree: %s\nMCE4D-Checkpoint: %s\nMCE4D-Verification: fixture", tasks, tree, tag)
	run("commit", "-m", message)
	oid := run("rev-parse", "HEAD")
	run("tag", "-a", tag, "-m", "tree="+tree+" tasks="+tasks+" verification=fixture")
	if err := checkCheckpoint(dir, oid, tag); err != nil {
		t.Fatalf("valid checkpoint: %v", err)
	}
	if err := checkCheckpoint(dir, strings.Repeat("0", len(oid)), tag); err == nil {
		t.Fatal("wrong commit should be rejected")
	}
	lightweight := "mce4d/checkpoints/B002"
	run("tag", lightweight)
	if err := checkCheckpoint(dir, oid, lightweight); err == nil {
		t.Fatal("lightweight tag should not count as a verified checkpoint")
	}
	if err := checkCheckpoint(dir, oid, "../../invalid"); err == nil {
		t.Fatal("invalid tag name should be rejected")
	}
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), []byte("changed fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "source.txt")
	tree2 := run("write-tree")
	tag2 := "mce4d/checkpoints/B003"
	badMessage := fmt.Sprintf("fixture bad checkpoint\n\nMCE4D-Batch: B003\nMCE4D-Batch-Type: full\nMCE4D-Task-Count: 10\nMCE4D-Tasks: %s\nMCE4D-Spec-Version: 1.6.2\nMCE4D-Tree: %s\nMCE4D-Checkpoint: %s\nMCE4D-Verification: fixture", tasks, tree, tag2)
	run("commit", "-m", badMessage)
	badOID := run("rev-parse", "HEAD")
	run("tag", "-a", tag2, "-m", "tree="+tree2+" tasks="+tasks+" verification=fixture")
	if err := checkCheckpoint(dir, badOID, tag2); err == nil {
		t.Fatal("commit trailer with wrong tree should be rejected")
	}
}
