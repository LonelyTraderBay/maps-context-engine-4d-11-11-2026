package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestOpenApprovedLinkedGitMetadataWindows(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "primary")
	checkout := filepath.Join(dir, "linked")
	slot := filepath.Join(primary, ".git", "worktrees", "linked")
	for _, name := range []string{slot, checkout} {
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	index := []byte("fixture Git index bytes")
	files := map[string][]byte{
		filepath.Join(checkout, ".git"):  []byte("gitdir: " + filepath.ToSlash(slot) + "\n"),
		filepath.Join(slot, "gitdir"):    []byte(filepath.ToSlash(filepath.Join(checkout, ".git")) + "\n"),
		filepath.Join(slot, "commondir"): []byte("../..\n"),
		filepath.Join(slot, "index"):     index,
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := paths.OpenRoot(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	root, err := paths.OpenRoot(checkout)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	common, relative, err := OpenApprovedGitMetadata(root, func(candidate string) (*paths.Root, error) {
		if candidate != primary {
			t.Fatalf("requested common directory owner %q, want %q", candidate, primary)
		}
		return OpenLocalGitMetadata(owner)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	if relative != "worktrees/linked" {
		t.Fatalf("wrong metadata slot %q", relative)
	}
	binding, err := BindLinkedGitMetadata(root, common, relative)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	if got, err := binding.ReadIndexContext(context.Background(), 4096); err != nil || !bytes.Equal(got, index) {
		t.Fatalf("wrong linked index bytes %q: %v", got, err)
	}
}
