package contract

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type isolatedGit struct {
	t                    *testing.T
	root, home, template string
}

func newIsolatedGit(t *testing.T) isolatedGit {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	template := filepath.Join(root, "empty-template")
	for _, p := range []string{home, template} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return isolatedGit{t, root, home, template}
}
func (g isolatedGit) git(dir string, args ...string) (string, error) {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	opts := []string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "-c", "init.templateDir=" + g.template}
	cmd := exec.CommandContext(ctx, "git", append(opts, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + g.home, "XDG_CONFIG_HOME=" + g.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(g.home, "missing"), "GIT_TERMINAL_PROMPT=0", "TMPDIR=" + g.root, "TMP=" + g.root, "TEMP=" + g.root}
	if v := os.Getenv("SYSTEMROOT"); v != "" {
		cmd.Env = append(cmd.Env, "SYSTEMROOT="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func (g isolatedGit) must(dir string, args ...string) string {
	g.t.Helper()
	out, err := g.git(dir, args...)
	if err != nil {
		g.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(out)
}
func writeFixture(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// stagedFixtureComplete tests exact required bytes and allowlisted paths; it is
// a contract oracle, not a secret scanner or production checkpoint guard.
func stagedFixtureComplete(g isolatedGit, dir string, required map[string][]byte) bool {
	g.t.Helper()
	out := g.must(dir, "diff", "--cached", "--name-only", "-z")
	paths := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	seen := map[string]bool{}
	for _, p := range paths {
		if _, ok := required[p]; !ok {
			return false
		}
		seen[p] = true
	}
	if len(seen) != len(required) {
		return false
	}
	for p, want := range required {
		staged, err := g.git(dir, "show", ":"+p)
		if err != nil || !bytes.Equal([]byte(staged), want) {
			return false
		}
		onDisk, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil || !bytes.Equal(onDisk, want) {
			return false
		}
	}
	return true
}
func TestGitFixtureStageAndCombinedTree(t *testing.T) {
	g := newIsolatedGit(t)
	repo := filepath.Join(g.root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	g.must(repo, "init")
	writeFixture(t, filepath.Join(repo, "source.go"), "source\n")
	writeFixture(t, filepath.Join(repo, "schema.json"), "schema\n")
	writeFixture(t, filepath.Join(repo, "go.sum"), "lock\n")
	writeFixture(t, filepath.Join(repo, ".gitignore"), "schema.json\n")
	required := map[string][]byte{"source.go": []byte("source\n"), "schema.json": []byte("schema\n"), "go.sum": []byte("lock\n"), ".gitignore": []byte("schema.json\n")}
	g.must(repo, "add", "source.go", ".gitignore")
	if stagedFixtureComplete(g, repo, required) {
		t.Fatal("missing ignored schema and lock accepted")
	}
	g.must(repo, "add", "go.sum")
	g.must(repo, "add", "-f", "schema.json")
	if !stagedFixtureComplete(g, repo, required) {
		t.Fatal("complete fixture rejected")
	}
	writeFixture(t, filepath.Join(repo, "source.go"), "after stage\n")
	if stagedFixtureComplete(g, repo, required) {
		t.Fatal("post-stage modification accepted")
	}
	writeFixture(t, filepath.Join(repo, "source.go"), "source\n")
	writeFixture(t, filepath.Join(repo, "secret.txt"), "synthetic secret marker\n")
	g.must(repo, "add", "secret.txt")
	if stagedFixtureComplete(g, repo, required) {
		t.Fatal("out of scope file accepted")
	}
}
func TestGitFixtureRetryRestoreAndLocalRemote(t *testing.T) {
	g := newIsolatedGit(t)
	repo := filepath.Join(g.root, "repo")
	bare := filepath.Join(g.root, "remote.git")
	restored := filepath.Join(g.root, "restored")
	for _, p := range []string{repo, bare} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	g.must(repo, "init")
	g.must(bare, "init", "--bare")
	writeFixture(t, filepath.Join(repo, "fixture.txt"), "fixture contents\n")
	g.must(repo, "add", "fixture.txt")
	tree := g.must(repo, "write-tree")
	g.must(repo, "commit", "-m", "fixture checkpoint")
	oid := g.must(repo, "rev-parse", "HEAD")
	if got := g.must(repo, "rev-parse", "HEAD^{tree}"); got != tree {
		t.Fatalf("tree mismatch: %s", got)
	}
	// Retry after an uncertain response reconciles HEAD first, and makes no new commit.
	if got := g.must(repo, "rev-parse", "HEAD"); got != oid {
		t.Fatal("commit not reconciled")
	}
	tag := "mce4d/checkpoints/B001"
	g.must(repo, "tag", "-a", tag, "-m", "fixture receipt")
	if _, err := g.git(repo, "tag", "-a", tag, "-m", "collision"); err == nil {
		t.Fatal("tag collision accepted")
	}
	g.must(repo, "-c", "protocol.file.allow=always", "push", bare, "HEAD:refs/heads/fixture", "refs/tags/"+tag)
	remoteOID := g.must(repo, "--git-dir", bare, "rev-parse", "refs/heads/fixture")
	if remoteOID != oid {
		t.Fatal("local remote branch mismatch")
	}
	g.must(g.root, "-c", "protocol.file.allow=always", "clone", "--branch", "fixture", bare, restored)
	data, err := os.ReadFile(filepath.Join(restored, "fixture.txt"))
	if err != nil || string(data) != "fixture contents\n" {
		t.Fatalf("restore: %s %v", data, err)
	}
	if got := g.must(restored, "rev-parse", "HEAD^{tree}"); got != tree {
		t.Fatalf("restored tree %s != %s", got, tree)
	}
	if got := g.must(repo, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatal(fmt.Sprintf("duplicate commit: %s", got))
	}
}

func TestGitFixtureHookTreeChangeDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hook fixture; native hook execution is not proven")
	}
	g := newIsolatedGit(t)
	repo := filepath.Join(g.root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	g.must(repo, "init")
	writeFixture(t, filepath.Join(repo, "fixture.txt"), "before\n")
	g.must(repo, "add", "fixture.txt")
	tree := g.must(repo, "write-tree")
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.Mkdir(filepath.Dir(hook), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, hook, "#!/bin/sh\nprintf 'hook changed\\n' > fixture.txt\ngit add fixture.txt\n")
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	g.must(repo, "commit", "-m", "synthetic hook fixture")
	actual := g.must(repo, "rev-parse", "HEAD^{tree}")
	if actual == tree {
		t.Fatal("hook mutation not detected by tree comparison")
	}
}

func TestGitFixtureDeletionModeAndMissingIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode fixture; native mode behavior is not proven")
	}
	g := newIsolatedGit(t)
	repo := filepath.Join(g.root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	g.must(repo, "init")
	old := filepath.Join(repo, "old.txt")
	script := filepath.Join(repo, "script.sh")
	writeFixture(t, old, "old\n")
	writeFixture(t, script, "script\n")
	g.must(repo, "add", "old.txt", "script.sh")
	g.must(repo, "commit", "-m", "fixture base")
	g.must(repo, "rm", "old.txt")
	if err := os.Chmod(script, 0700); err != nil {
		t.Fatal(err)
	}
	g.must(repo, "add", "script.sh")
	status := g.must(repo, "diff", "--cached", "--name-status")
	raw := g.must(repo, "diff", "--cached", "--raw")
	if !strings.Contains(status, "D\told.txt") || !strings.Contains(raw, "100644 100755") {
		t.Fatalf("deletion or mode missing: %q %q", status, raw)
	}
	before := g.must(repo, "rev-parse", "HEAD")
	if _, err := g.git(repo, "-c", "user.name=", "-c", "user.email=", "-c", "user.useConfigOnly=true", "commit", "-m", "must fail"); err == nil {
		t.Fatal("commit succeeded without identity")
	}
	if g.must(repo, "rev-parse", "HEAD") != before || g.must(repo, "diff", "--cached", "--name-status") != status {
		t.Fatal("missing identity changed HEAD or stage")
	}
}

func TestGitFixtureInterimSnapshotIncludesUntrackedAndDeletion(t *testing.T) {
	g := newIsolatedGit(t)
	repo := filepath.Join(g.root, "repo")
	restore := filepath.Join(g.root, "restore")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	g.must(repo, "init")
	writeFixture(t, filepath.Join(repo, "tracked.txt"), "base\n")
	writeFixture(t, filepath.Join(repo, "removed.txt"), "remove me\n")
	g.must(repo, "add", "tracked.txt", "removed.txt")
	g.must(repo, "commit", "-m", "base")
	writeFixture(t, filepath.Join(repo, "tracked.txt"), "staged\n")
	g.must(repo, "add", "tracked.txt")
	writeFixture(t, filepath.Join(repo, "tracked.txt"), "staged and unstaged\n")
	if err := os.Remove(filepath.Join(repo, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(repo, "untracked.txt"), "new file\n")
	patch, err := g.git(repo, "diff", "--binary", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	list, err := g.git(repo, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		t.Fatal(err)
	}
	if list != "untracked.txt\x00" {
		t.Fatalf("unexpected untracked set: %q", list)
	}
	untracked, err := os.ReadFile(filepath.Join(repo, "untracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	patchPath := filepath.Join(g.root, "owned-fixture.patch")
	writeFixture(t, patchPath, patch)
	g.must(g.root, "-c", "protocol.file.allow=always", "clone", repo, restore)
	g.must(restore, "apply", patchPath)
	writeFixture(t, filepath.Join(restore, "untracked.txt"), string(untracked))
	for name, want := range map[string]string{"tracked.txt": "staged and unstaged\n", "untracked.txt": "new file\n"} {
		got, err := os.ReadFile(filepath.Join(restore, name))
		if err != nil || string(got) != want {
			t.Fatalf("restored %s: %q %v", name, got, err)
		}
	}
	if _, err = os.Stat(filepath.Join(restore, "removed.txt")); !os.IsNotExist(err) {
		t.Fatalf("deletion not restored: %v", err)
	}
}

func TestGitFixtureDivergedRemoteDoesNotAdvanceLocal(t *testing.T) {
	g := newIsolatedGit(t)
	a := filepath.Join(g.root, "a")
	bare := filepath.Join(g.root, "remote.git")
	b := filepath.Join(g.root, "b")
	for _, p := range []string{a, bare} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	g.must(a, "init")
	g.must(bare, "init", "--bare")
	writeFixture(t, filepath.Join(a, "file"), "base\n")
	g.must(a, "add", "file")
	g.must(a, "commit", "-m", "base")
	g.must(a, "-c", "protocol.file.allow=always", "push", bare, "HEAD:refs/heads/fixture")
	g.must(g.root, "-c", "protocol.file.allow=always", "clone", "--branch", "fixture", bare, b)
	writeFixture(t, filepath.Join(b, "file"), "remote change\n")
	g.must(b, "add", "file")
	g.must(b, "commit", "-m", "remote change")
	g.must(b, "-c", "protocol.file.allow=always", "push", bare, "HEAD:refs/heads/fixture")
	remote := g.must(a, "--git-dir", bare, "rev-parse", "refs/heads/fixture")
	writeFixture(t, filepath.Join(a, "file"), "local change\n")
	g.must(a, "add", "file")
	g.must(a, "commit", "-m", "local change")
	local := g.must(a, "rev-parse", "HEAD")
	if _, err := g.git(a, "-c", "protocol.file.allow=always", "push", bare, "HEAD:refs/heads/fixture"); err == nil {
		t.Fatal("diverged push accepted")
	}
	if g.must(a, "--git-dir", bare, "rev-parse", "refs/heads/fixture") != remote || g.must(a, "rev-parse", "HEAD") != local {
		t.Fatal("failed push changed local or remote")
	}
}

func TestGitFixtureMissingExternalMaterialIsNotRestore(t *testing.T) {
	g := newIsolatedGit(t)
	repo := filepath.Join(g.root, "repo")
	restore := filepath.Join(g.root, "restore")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	g.must(repo, "init")
	writeFixture(t, filepath.Join(repo, "source.txt"), "requires private material\n")
	writeFixture(t, filepath.Join(repo, ".gitignore"), "local-material.bin\n")
	writeFixture(t, filepath.Join(repo, "local-material.bin"), "synthetic dependency\n")
	g.must(repo, "add", "source.txt", ".gitignore")
	g.must(repo, "commit", "-m", "fixture")
	g.must(g.root, "-c", "protocol.file.allow=always", "clone", repo, restore)
	if _, err := os.Stat(filepath.Join(restore, "source.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(restore, "local-material.bin")); !os.IsNotExist(err) {
		t.Fatalf("missing material check: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "local-material.bin")); err != nil {
		t.Fatal("original material changed")
	}
}
