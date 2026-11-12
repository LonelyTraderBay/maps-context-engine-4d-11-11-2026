//go:build linux

package runtime

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestLinkedGitBindingRejectsChangedPointers(t *testing.T) {
	base := t.TempDir()
	worktree := filepath.Join(base, "worktree")
	common := filepath.Join(base, "common")
	metadata := filepath.Join(common, "worktrees", "one")
	for _, dir := range []string{worktree, metadata} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	contents := map[string][]byte{
		filepath.Join(worktree, ".git"):      []byte("gitdir: " + metadata + "\n"),
		filepath.Join(metadata, "gitdir"):    []byte(filepath.Join(worktree, ".git") + "\n"),
		filepath.Join(metadata, "commondir"): []byte("../..\n"),
		filepath.Join(metadata, "index"):     []byte("fixture index"),
	}
	for name, content := range contents {
		if err := os.WriteFile(name, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	worktreeRoot, err := paths.OpenRoot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer worktreeRoot.Close()
	commonRoot, err := paths.OpenRoot(common)
	if err != nil {
		t.Fatal(err)
	}
	defer commonRoot.Close()
	binding, err := BindLinkedGitMetadata(worktreeRoot, commonRoot, "worktrees/one")
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	if got, err := binding.ReadIndex(1024); err != nil || !bytes.Equal(got, []byte("fixture index")) {
		t.Fatalf("valid fixture: %v", err)
	}
	for _, name := range []string{filepath.Join(worktree, ".git"), filepath.Join(metadata, "gitdir"), filepath.Join(metadata, "commondir")} {
		t.Run(filepath.Base(name), func(t *testing.T) {
			if err := os.WriteFile(name, []byte("/outside/fixture\n"), 0600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.WriteFile(name, contents[name], 0600); err != nil {
					t.Error(err)
				}
			}()
			data, err := binding.ReadIndex(1024)
			if !errors.Is(err, ErrGitMetadataUnverified) || data != nil {
				t.Fatalf("changed pointer yielded data: %v", err)
			}
		})
		t.Run(filepath.Base(name)+"-symlink", func(t *testing.T) {
			saved := name + ".saved"
			outside := filepath.Join(base, "pointer-copy-"+filepath.Base(name))
			if err := os.WriteFile(outside, contents[name], 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(name, saved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, name); err != nil {
				_ = os.Rename(saved, name)
				t.Fatal(err)
			}
			defer func() {
				if err := os.Remove(name); err != nil {
					t.Error(err)
				}
				if err := os.Rename(saved, name); err != nil {
					t.Error(err)
				}
			}()
			data, err := binding.ReadIndex(1024)
			if !errors.Is(err, ErrGitMetadataUnverified) || data != nil {
				t.Fatalf("symlinked pointer yielded data: %v", err)
			}
		})
	}
	if err := os.Symlink(metadata, filepath.Join(common, "worktrees", "other")); err != nil {
		t.Fatal(err)
	}
	if other, err := BindLinkedGitMetadata(worktreeRoot, commonRoot, "worktrees/other"); err == nil {
		other.Close()
		t.Fatal("symlinked metadata directory accepted")
	}
}

func TestGitMetadataDirectoryRejectsLinksAndReplacement(t *testing.T) {
	base := t.TempDir()
	worktree := filepath.Join(base, "worktree")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{worktree, outside} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(worktree, ".git")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if child, err := OpenLocalGitMetadata(root); err == nil {
		child.Close()
		t.Fatal("symlink metadata was accepted")
	}
	if err := os.Remove(filepath.Join(worktree, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(worktree, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	child, err := OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := os.Rename(filepath.Join(worktree, ".git"), filepath.Join(worktree, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(worktree, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := child.Verify(); !errors.Is(err, paths.ErrRootChanged) {
		t.Fatalf("replaced metadata root accepted: %v", err)
	}
}

func TestGitPointerRejectsAmbiguousTraversal(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "tmp", "worktree")
	expected := filepath.Join(string(filepath.Separator), "tmp", "common", "worktrees", "one")
	for _, pointer := range []string{
		"gitdir: " + filepath.Join(base, "jump") + "/../../common/worktrees/one\n",
		"gitdir: jump/../../common/worktrees/one\n",
		"gitdir: ../common/worktrees/one\nextra",
	} {
		if pointerMatches([]byte(pointer), "gitdir: ", base, expected) {
			t.Fatal("ambiguous pointer accepted")
		}
	}
	if !pointerMatches([]byte("gitdir: ../common/worktrees/one\n"), "gitdir: ", base, expected) {
		t.Fatal("valid relative pointer rejected")
	}
}
