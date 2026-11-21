//go:build linux

package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func resultFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "scratch"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "scratch", "rename"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
