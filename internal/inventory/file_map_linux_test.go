//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestBuildFileMapMergesTrackedAndIgnoredCandidates(t *testing.T) {
	f := newGitFixture(t, "sha1")
	for _, name := range []string{"tracked.go", "gone.go", "tracked.log", "new.go", "ignored.log", ".env", "vendor/lib.go", "nested/hidden.go"} {
		f.write(t, name)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".gitignore"), []byte("*.log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.run(t, nil, "add", "--", "tracked.go", "gone.go")
	f.run(t, nil, "add", "-f", "--", "tracked.log")
	if err := os.Remove(filepath.Join(f.root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.root, "nested", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tracked.go", filepath.Join(f.root, "link.go")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	allowRoot := func(context.Context) error { return nil }
	allowPath := func(context.Context, string, bool) (bool, error) { return true, nil }
	index, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), allowRoot)
	if err != nil {
		t.Fatal(err)
	}
	walk, err := WalkCandidates(ctx, root, walkBudget(), allowRoot, allowPath, func(name string, dir bool) bool { return dir && name == "vendor" })
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := CaptureDirectoryIgnoreSources(ctx, root, walk, scanner, ignoreCaptureBudget(), func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := runtime.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := CaptureInfoExclude(ctx, metadata, scanner, 4096, func(context.Context, string) error { return nil })
	if closeErr := metadata.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		sources = append(sources, *info)
	}
	queries := make([]IgnoreQuery, len(walk.Candidates))
	for i, candidate := range walk.Candidates {
		queries[i] = IgnoreQuery{Path: candidate.Path, IsDirectory: candidate.Directory}
	}
	sandbox, _ := ignoreSandbox(t)
	ignored, err := EvaluateIgnoreRules(ctx, sandbox, f.git, sources, queries, scanner, IgnoreOptions{}, ignoreBudget(), allowRoot)
	if err != nil {
		t.Fatal(err)
	}
	got, err := BuildFileMap(ctx, root, index, walk, ignored, 1000, allowRoot, allowPath)
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]FileMapEntry{}
	for _, item := range got.Entries {
		items[item.Path] = item
	}
	for name, want := range map[string]struct{ origin, state string }{
		".gitignore": {"untracked", "candidate"}, "tracked.go": {"tracked", "candidate"}, "tracked.log": {"tracked", "candidate"},
		"gone.go": {"tracked", "missing"}, "new.go": {"untracked", "candidate"}, "nested": {"untracked", "boundary"},
		"link.go": {"untracked", "excluded"}, "vendor": {"untracked", "excluded"},
	} {
		item, ok := items[name]
		if !ok || item.Origin != want.origin || item.State != want.state {
			t.Fatalf("%s: got %+v want %+v", name, item, want)
		}
	}
	if _, ok := items["ignored.log"]; ok {
		t.Fatal("ignored untracked file was emitted")
	}
	if _, ok := items[".env"]; ok {
		t.Fatal("denied path was emitted")
	}
	if _, ok := items["nested/hidden.go"]; ok {
		t.Fatal("nested root was traversed")
	}
	if _, ok := items["vendor/lib.go"]; ok {
		t.Fatal("excluded vendor content was emitted")
	}
	if got.DeniedCount == 0 || got.IgnoredCount == 0 {
		t.Fatalf("coverage counts missing: %+v", got)
	}
}

func TestBuildFileMapRejectsChangedMembership(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	if err := os.Remove(filepath.Join(f.root, "tracked.go")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowRoot := func(context.Context) error { return nil }
	allowPath := func(context.Context, string, bool) (bool, error) { return true, nil }
	index, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), allowRoot)
	if err != nil {
		t.Fatal(err)
	}
	walk, err := WalkCandidates(ctx, root, walkBudget(), allowRoot, allowPath, func(string, bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "tracked.go"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := BuildFileMap(ctx, root, index, walk, make([]bool, len(walk.Candidates)), 1000, allowRoot, allowPath); !errors.Is(err, ErrWalkChanged) || len(result.Entries) != 0 {
		t.Fatalf("changed membership accepted: %+v %v", result, err)
	}
}

func TestBuildFileMapRejectsAuthorityRevocation(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowRoot := func(context.Context) error { return nil }
	allowPath := func(context.Context, string, bool) (bool, error) { return true, nil }
	index, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), allowRoot)
	if err != nil {
		t.Fatal(err)
	}
	walk, err := WalkCandidates(ctx, root, walkBudget(), allowRoot, allowPath, func(string, bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := BuildFileMap(ctx, root, index, walk, make([]bool, len(walk.Candidates)), 1000, allowRoot, func(_ context.Context, name string, _ bool) (bool, error) {
		if name == "tracked.go" {
			calls++
			return calls < 2, nil
		}
		return true, nil
	})
	if !errors.Is(err, ErrWalkAuthorityChanged) || len(result.Entries) != 0 {
		t.Fatalf("revoked file map returned data: %+v %v", result, err)
	}
}

func TestBuildFileMapTracksUnmergedAndSparseEntries(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "sparse.go")
	f.run(t, nil, "add", "--", "sparse.go")
	oid := strings.TrimSpace(string(f.run(t, nil, "hash-object", "-w", "sparse.go")))
	f.run(t, nil, "update-index", "--skip-worktree", "--", "sparse.go")
	input := []byte("100644 " + oid + " 1\tconflict.go\n100644 " + oid + " 2\tconflict.go\n")
	f.run(t, input, "update-index", "--index-info")
	if err := os.Remove(filepath.Join(f.root, "sparse.go")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowRoot := func(context.Context) error { return nil }
	allowPath := func(context.Context, string, bool) (bool, error) { return true, nil }
	index, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), allowRoot)
	if err != nil {
		t.Fatal(err)
	}
	walk, err := WalkCandidates(ctx, root, walkBudget(), allowRoot, allowPath, func(string, bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	result, err := BuildFileMap(ctx, root, index, walk, make([]bool, len(walk.Candidates)), 1000, allowRoot, allowPath)
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]FileMapEntry{}
	for _, item := range result.Entries {
		items[item.Path] = item
	}
	if items["sparse.go"].State != "sparse" || !items["sparse.go"].SkipWorktree {
		t.Fatalf("sparse status: %+v", items["sparse.go"])
	}
	if items["conflict.go"].State != "unmerged" || !reflect.DeepEqual(items["conflict.go"].Stages, []uint8{1, 2}) {
		t.Fatalf("unmerged status: %+v", items["conflict.go"])
	}
}

func TestIndexGitlinkStopsTraversalBeforeWalking(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "base.go")
	f.run(t, nil, "add", "--", "base.go")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	oid := strings.TrimSpace(string(f.run(t, nil, "rev-parse", "HEAD")))
	f.write(t, "sub/hidden.go")
	f.run(t, nil, "update-index", "--add", "--cacheinfo", "160000,"+oid+",sub")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowRoot := func(context.Context) error { return nil }
	index, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), allowRoot)
	if err != nil {
		t.Fatal(err)
	}
	exclude, err := ExcludeIndexBoundaries(index.Index, func(string, bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	entered := false
	allowPath := func(_ context.Context, name string, _ bool) (bool, error) {
		if name == "sub/hidden.go" {
			entered = true
		}
		return true, nil
	}
	walk, err := WalkCandidates(ctx, root, walkBudget(), allowRoot, allowPath, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if entered {
		t.Fatal("gitlink contents were traversed")
	}
	result, err := BuildFileMap(ctx, root, index, walk, make([]bool, len(walk.Candidates)), 1000, allowRoot, allowPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range result.Entries {
		if item.Path == "sub" && item.Origin == "tracked" && item.State == "boundary" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gitlink boundary missing: %+v", result.Entries)
	}
}

func TestBuildFileMapRejectsDuplicateNormalizedIndexPaths(t *testing.T) {
	dir := t.TempDir()
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	index := IndexObservation{Index: GitIndex{SparseDirectories: true, Entries: []IndexEntry{
		{Path: []byte("same"), Mode: 0100644},
		{Path: []byte("same/"), Mode: 0040000, SkipWorktree: true},
	}}}
	result, err := BuildFileMap(ctx, root, index, CandidateWalk{}, nil, 10, func(context.Context) error { return nil }, func(context.Context, string, bool) (bool, error) { return true, nil })
	if !errors.Is(err, ErrIndexCorrupt) || len(result.Entries) != 0 {
		t.Fatalf("duplicate index path accepted: %+v %v", result, err)
	}
}
