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

func TestReadGitIndexFromLinkedWorktreeFixture(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	linked := filepath.Join(base, "linked")
	template := filepath.Join(base, "empty-template")
	for _, dir := range []string{repo, template} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, out)
		}
	}
	run("init", "--template="+template, repo)
	if err := os.WriteFile(filepath.Join(repo, "file.go"), []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("-C", repo, "add", "file.go")
	run("-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
	run("-C", repo, "worktree", "add", "-b", "fixture-linked", linked)
	// The fixture explicitly supplies both roots. A production caller must
	// establish their authorization through registration.
	mainRoot, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer mainRoot.Close()
	common, err := OpenLocalGitMetadata(mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	linkedRoot, err := paths.OpenRoot(linked)
	if err != nil {
		t.Fatal(err)
	}
	defer linkedRoot.Close()
	binding, err := BindLinkedGitMetadata(linkedRoot, common, "worktrees/linked")
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	data, err := binding.ReadIndex(1024 * 1024)
	if err != nil || !bytes.HasPrefix(data, []byte("DIRC")) {
		t.Fatalf("linked fixture index: %v", err)
	}
}

func TestReadGitIndexUsesOnlyExplicitMetadataRoot(t *testing.T) {
	base := t.TempDir()
	worktree := filepath.Join(base, "worktree")
	metadata := filepath.Join(base, "authorized-metadata")
	for _, dir := range []string{worktree, metadata} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Like a linked worktree, the worktree can refer to a separate metadata
	// directory. This primitive never parses or trusts that reference itself.
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /untrusted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	want := []byte("synthetic index bytes")
	if err := os.WriteFile(filepath.Join(metadata, "index"), want, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	got, err := ReadGitIndex(root, 1024)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("explicit metadata: %q %v", got, err)
	}
	if data, err := ReadGitIndex(root, len(want)-1); !errors.Is(err, ErrGitIndexLimit) || data != nil {
		t.Fatalf("index limit: %q %v", data, err)
	}
	if data, err := ReadGitIndex(root, 0); !errors.Is(err, ErrGitIndexLimit) || data != nil {
		t.Fatalf("invalid index limit: %q %v", data, err)
	}
}

func TestReadGitIndexRejectsMetadataAndIndexSymlinks(t *testing.T) {
	base := t.TempDir()
	metadata := filepath.Join(base, "metadata")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(metadata, filepath.Join(base, "metadata-link")); err != nil {
		t.Fatal(err)
	}
	if root, err := paths.OpenRoot(filepath.Join(base, "metadata-link")); err == nil {
		root.Close()
		t.Fatal("metadata root symlink accepted")
	}
	if err := os.Symlink(outside, filepath.Join(metadata, "index")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if data, err := ReadGitIndex(root, 1024); err == nil || data != nil {
		t.Fatalf("symlinked index yielded bytes: %q %v", data, err)
	}
}

func TestReadGitIndexRejectsChangedRoot(t *testing.T) {
	base := t.TempDir()
	metadata := filepath.Join(base, "metadata")
	if err := os.Mkdir(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "index"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(metadata, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadGitIndex(root, 1024); !errors.Is(err, paths.ErrRootChanged) || data != nil {
		t.Fatalf("changed metadata root yielded bytes: %q %v", data, err)
	}
}

func TestReadGitIndexRejectsHardLinkedIndex(t *testing.T) {
	base := t.TempDir()
	metadata := filepath.Join(base, "metadata")
	if err := os.Mkdir(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside-index")
	if err := os.WriteFile(outside, []byte("outside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(metadata, "index")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if data, err := ReadGitIndex(root, 1024); !errors.Is(err, ErrGitIndexLinked) || data != nil {
		t.Fatalf("hard-linked index yielded bytes: %q %v", data, err)
	}
}

type modifyingIndexReader struct {
	file   *os.File
	change func() error
	done   bool
}

func (r *modifyingIndexReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		if err := r.change(); err != nil {
			return 0, err
		}
	}
	return r.file.Read(p)
}

func TestReadGitIndexRejectsChangeDuringRead(t *testing.T) {
	for _, mode := range []string{"replace", "modify", "modify-restore-mtime"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			index := filepath.Join(base, "index")
			if err := os.WriteFile(index, []byte("original index"), 0600); err != nil {
				t.Fatal(err)
			}
			root, err := paths.OpenRoot(base)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			file, err := root.OpenRegular("index")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			initial, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			reader := &modifyingIndexReader{file: file, change: func() error {
				if mode == "replace" {
					if err := os.Rename(index, filepath.Join(base, "moved")); err != nil {
						return err
					}
					return os.WriteFile(index, []byte("different data"), 0600)
				}
				if mode == "modify-restore-mtime" {
					if err := os.WriteFile(index, []byte("changed! index"), 0600); err != nil {
						return err
					}
					return os.Chtimes(index, initial.ModTime(), initial.ModTime())
				}
				return os.WriteFile(index, []byte("modified contents longer"), 0600)
			}}
			data, err := readGitIndex(root, file, reader, 1024)
			if !errors.Is(err, ErrGitIndexChanged) || data != nil {
				t.Fatalf("changed index yielded data: %v", err)
			}
		})
	}
}
