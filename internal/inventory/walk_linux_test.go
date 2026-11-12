//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

func walkBudget() WalkBudget {
	return WalkBudget{MaxVisited: 1000, MaxDirectories: 100, MaxDepth: 20, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20}
}

func TestWalkCandidatesRespectsBoundariesAndPolicy(t *testing.T) {
	base := t.TempDir()
	repo, outside := filepath.Join(base, "repo"), filepath.Join(base, "outside")
	for _, dir := range []string{repo, outside, filepath.Join(repo, ".git"), filepath.Join(repo, "nested", ".git"), filepath.Join(repo, "vendor"), filepath.Join(repo, ".aws")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"a.go": "package a\n", "new file.go": "package next\n", "nested/hidden.go": "nested",
		"vendor/library.go": "vendor", ".aws/credentials": "secret fixture", ".env": "secret fixture",
		"private.go": "private", "line\nbreak.go": "invalid path", "original.go": "linked",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(path)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "outside.go"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(repo, "original.go"), filepath.Join(repo, "hard.go")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(repo, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	scopeChecks := 0
	result, err := WalkCandidates(ctx, root, walkBudget(),
		func(context.Context) error { scopeChecks++; return nil },
		func(_ context.Context, path string, _ bool) (bool, error) { return path != "private.go", nil },
		func(path string, directory bool) bool { return directory && path == "vendor" })
	if err != nil {
		t.Fatal(err)
	}
	want := []Candidate{{Path: "a.go", Kind: "regular"}, {Path: "hard.go", Kind: "hardlink"}, {Path: "link", Kind: "symlink"}, {Path: "nested", Kind: "nested_root", Directory: true}, {Path: "new file.go", Kind: "regular"}, {Path: "original.go", Kind: "hardlink"}, {Path: "pipe", Kind: "unsupported"}, {Path: "vendor", Kind: "excluded", Directory: true}}
	if !reflect.DeepEqual(result.Candidates, want) {
		t.Fatalf("candidates: got %+v want %+v", result.Candidates, want)
	}
	if result.DeniedCount != 3 || result.InvalidPathCount != 1 || scopeChecks != 2 {
		t.Fatalf("coverage: denied=%d invalid=%d scope_checks=%d", result.DeniedCount, result.InvalidPathCount, scopeChecks)
	}
	content, err := os.ReadFile(filepath.Join(outside, "outside.go"))
	if err != nil || string(content) != "outside" {
		t.Fatalf("outside file changed: %q %v", content, err)
	}
}

func TestWalkCandidatesFailsClosedOnRevocationAndLimits(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := map[string]int{}
	result, err := WalkCandidates(ctx, root, walkBudget(), func(context.Context) error { return nil },
		func(_ context.Context, path string, _ bool) (bool, error) {
			calls[path]++
			return path != "a.go" || calls[path] == 1, nil
		},
		func(string, bool) bool { return false })
	if !errors.Is(err, ErrWalkAuthorityChanged) || len(result.Candidates) != 0 {
		t.Fatalf("revoked enumeration returned data: %+v %v", result, err)
	}
	budget := walkBudget()
	budget.MaxVisited = 1
	result, err = WalkCandidates(ctx, root, budget, func(context.Context) error { return nil }, func(context.Context, string, bool) (bool, error) { return true, nil }, func(string, bool) bool { return false })
	if !errors.Is(err, ErrWalkBudget) || len(result.Candidates) != 0 {
		t.Fatalf("budget returned partial data: %+v %v", result, err)
	}
	_, err = WalkCandidates(context.Background(), root, walkBudget(), func(context.Context) error { return nil }, func(context.Context, string, bool) (bool, error) { return true, nil }, func(string, bool) bool { return false })
	if !errors.Is(err, ErrWalkBudget) {
		t.Fatalf("unbounded walk accepted: %v", err)
	}
}

func TestWalkCandidatesRejectsRootReplacement(t *testing.T) {
	base := t.TempDir()
	repo, moved, outside := filepath.Join(base, "repo"), filepath.Join(base, "moved"), filepath.Join(base, "outside")
	for _, dir := range []string{repo, outside} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	changed := false
	result, err := WalkCandidates(ctx, root, walkBudget(), func(context.Context) error { return nil },
		func(_ context.Context, path string, _ bool) (bool, error) {
			if !changed {
				changed = true
				if err := os.Rename(repo, moved); err != nil {
					return false, err
				}
				if err := os.Symlink(outside, repo); err != nil {
					return false, err
				}
			}
			return true, nil
		}, func(string, bool) bool { return false })
	if !errors.Is(err, paths.ErrRootChanged) || len(result.Candidates) != 0 {
		t.Fatalf("replaced root returned data: %+v %v", result, err)
	}
}

func TestWalkCandidatesRejectsDirectoryReplacementBeforeTraversal(t *testing.T) {
	base := t.TempDir()
	repo, moved := filepath.Join(base, "repo"), filepath.Join(base, "old-sub")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", "inside.go"), []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	changed, enteredReplacement := false, false
	result, err := WalkCandidates(ctx, root, walkBudget(), func(context.Context) error { return nil },
		func(_ context.Context, path string, _ bool) (bool, error) {
			if path == "sub" && !changed {
				changed = true
				if err := os.Rename(filepath.Join(repo, "sub"), moved); err != nil {
					return false, err
				}
				if err := os.Mkdir(filepath.Join(repo, "sub"), 0700); err != nil {
					return false, err
				}
				if err := os.WriteFile(filepath.Join(repo, "sub", "replacement.go"), []byte("replacement"), 0600); err != nil {
					return false, err
				}
			}
			if path == "sub/replacement.go" {
				enteredReplacement = true
			}
			return true, nil
		}, func(string, bool) bool { return false })
	if !errors.Is(err, ErrWalkChanged) || len(result.Candidates) != 0 || enteredReplacement {
		t.Fatalf("replaced directory was traversed: %+v entered=%v error=%v", result, enteredReplacement, err)
	}
}

func TestWalkCandidatesRejectsNestedRootCreatedDuringScan(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", "inside.go"), []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	created := false
	result, err := WalkCandidates(ctx, root, walkBudget(), func(context.Context) error { return nil },
		func(_ context.Context, path string, _ bool) (bool, error) {
			if path == "sub/inside.go" && !created {
				created = true
				if err := os.Mkdir(filepath.Join(repo, "sub", ".git"), 0700); err != nil {
					return false, err
				}
			}
			return true, nil
		}, func(string, bool) bool { return false })
	if !errors.Is(err, ErrWalkChanged) || len(result.Candidates) != 0 {
		t.Fatalf("new nested root returned data: %+v %v", result, err)
	}
}
