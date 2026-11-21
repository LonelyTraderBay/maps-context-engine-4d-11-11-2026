package paths

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsOwnedTreeCleanupKeepsSiblingAndHardlinkTarget(t *testing.T) {
	parent := t.TempDir()
	root, err := OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	job := filepath.Join(parent, "owned")
	protected := filepath.Join(parent, "canary.txt")
	expected := []byte("protected fixture")
	if err := os.WriteFile(protected, expected, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(job, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(job, "alias.txt")
	if err := os.Link(protected, alias); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWindowsOwnedTree(ctx, root, "owned"); err == nil {
		t.Fatal("owned-tree cleanup accepted a hardlink")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"input", "cache"} {
		child := filepath.Join(job, dir)
		if err := os.Mkdir(child, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(child, "buffer"), []byte(dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveWindowsOwnedTree(ctx, root, "owned"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(job); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned scratch still present: %v", err)
	}
	if data, err := os.ReadFile(protected); err != nil || string(data) != string(expected) {
		t.Fatalf("protected sibling changed: %v", err)
	}
}
