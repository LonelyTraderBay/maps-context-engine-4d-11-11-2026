package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestWindowsNativeDirectoryIdentity(t *testing.T) {
	open := func(path string) *paths.Root {
		t.Helper()
		root, err := paths.OpenRoot(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { root.Close() })
		return root
	}
	firstPath, secondPath := filepath.Join(t.TempDir(), "same"), filepath.Join(t.TempDir(), "same")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	first, reopened, second := open(firstPath), open(firstPath), open(secondPath)
	if same, err := sameDirectory(first, reopened); !same || err != nil {
		t.Fatalf("reopening the same NTFS directory changed its identity: %v", err)
	}
	if same, err := sameDirectory(first, second); same || err != nil {
		t.Fatalf("different NTFS directories with the same name returned %v, %v", same, err)
	}
}
