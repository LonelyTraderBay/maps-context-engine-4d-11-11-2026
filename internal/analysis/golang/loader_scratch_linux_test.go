//go:build linux

package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestLoaderScratchVerifierRequiresExactOwnedWorkspace(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.Chmod(rootPath, 0700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(rootPath, "input")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "pkg"), 0700); err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n", "pkg/y.go": "package pkg\n"}
	for name, value := range contents {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := loaderPreflightFixture(t, contents)
	root, err := paths.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := checkLoaderDirectory(root); err != nil {
		info, statErr := os.Stat(rootPath)
		t.Fatalf("scratch owner verification: %v; stat=%v mode=%v", err, statErr, info.Mode())
	}
	workspaceRoot, err := root.OpenDirectory("input")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkLoaderDirectory(workspaceRoot); err != nil {
		t.Fatalf("workspace owner verification: %v", err)
	}
	if err := workspaceRoot.Close(); err != nil {
		t.Fatal(err)
	}
	verify := LoaderScratchVerifier(root, func(context.Context) error { return nil })
	ctx := preflightContext(t)
	check := func(want bool) {
		t.Helper()
		got, err := verify(ctx, rootPath, workspace, input.Snapshot)
		if err != nil || got != want {
			t.Fatalf("scratch verification=%v want=%v err=%v", got, want, err)
		}
	}
	check(true)
	if err := os.WriteFile(filepath.Join(workspace, "x.go"), []byte("package y\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.WriteFile(filepath.Join(workspace, "x.go"), []byte(contents["x.go"]), 0600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(workspace, "extra.go")
	if err := os.WriteFile(extra, []byte("package x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("x.go", extra); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Remove(filepath.Join(workspace, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "pkg", ".git"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestLoaderScratchVerifierRejectsLoosePermissions(t *testing.T) {
	rootPath := t.TempDir()
	workspace := filepath.Join(rootPath, "input")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	input := loaderPreflightFixture(t, map[string]string{})
	root, err := paths.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Chmod(rootPath, 0755); err != nil {
		t.Fatal(err)
	}
	verify := LoaderScratchVerifier(root, func(context.Context) error { return nil })
	if ok, err := verify(preflightContext(t), rootPath, workspace, input.Snapshot); err != nil || ok {
		t.Fatalf("loose scratch accepted: %v %v", ok, err)
	}
}
