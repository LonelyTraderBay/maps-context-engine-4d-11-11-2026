//go:build linux

package inventory

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func indexFixtureBudget() GitIndexBudget {
	return GitIndexBudget{MaxBytes: 1 << 20, MaxEntries: 1000, MaxPathBytes: 4096, MaxDecodedPathBytes: 1 << 20}
}

func TestReadLocalGitIndexFromAuthorizedRoot(t *testing.T) {
	f := newGitFixture(t, "sha1")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	calls := 0
	authorize := func(context.Context) error { calls++; return nil }
	missing, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), authorize)
	if err != nil || !missing.IndexMissing || missing.Index.Entries == nil || calls != 2 {
		t.Fatalf("new repo index state: %+v %d %v", missing, calls, err)
	}
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	calls = 0
	got, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), authorize)
	if err != nil || got.IndexMissing || len(got.Index.Entries) != 1 || string(got.Index.Entries[0].Path) != "tracked.go" || calls != 2 {
		t.Fatalf("tracked snapshot: %+v %d %v", got, calls, err)
	}
	revoked := errors.New("fixture authority revoked")
	calls = 0
	got, err = ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), func(context.Context) error {
		calls++
		if calls == 2 {
			return revoked
		}
		return nil
	})
	if !errors.Is(err, revoked) || len(got.Index.Entries) != 0 {
		t.Fatalf("revoked authority returned data: %+v %v", got, err)
	}
	if _, err := ReadLocalGitIndex(context.Background(), root, "sha1", indexFixtureBudget(), authorize); !errors.Is(err, ErrIndexBudget) {
		t.Fatalf("unbounded operation accepted: %v", err)
	}
	budget := indexFixtureBudget()
	budget.MaxBytes = 1
	if _, err := ReadLocalGitIndex(ctx, root, "sha1", budget, authorize); !errors.Is(err, runtime.ErrGitIndexLimit) {
		t.Fatalf("index byte limit ignored: %v", err)
	}
}

func TestReadLocalGitIndexRejectsRedirectedMetadata(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(filepath.Join(f.root, ".git"), filepath.Join(f.root, "moved-git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.root, "moved-git"), filepath.Join(f.root, ".git")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if result, err := ReadLocalGitIndex(ctx, root, "sha1", indexFixtureBudget(), func(context.Context) error { return nil }); err == nil || len(result.Index.Entries) != 0 {
		t.Fatalf("redirected .git yielded data: %+v %v", result, err)
	}
}

func TestReadLinkedGitIndexUsesExplicitBinding(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "tracked.go")
	f.run(t, nil, "add", "--", "tracked.go")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	linkedPath := filepath.Join(f.home, "linked")
	f.run(t, nil, "worktree", "add", "-q", "--detach", linkedPath, "HEAD")
	worktree, err := paths.OpenRoot(linkedPath)
	if err != nil {
		t.Fatal(err)
	}
	defer worktree.Close()
	common, err := paths.OpenRoot(filepath.Join(f.root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	binding, err := runtime.BindLinkedGitMetadata(worktree, common, "worktrees/linked")
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := ReadLinkedGitIndex(ctx, binding, "sha1", indexFixtureBudget(), func(context.Context) error { return nil })
	if err != nil || got.IndexMissing || len(got.Index.Entries) != 1 || string(got.Index.Entries[0].Path) != "tracked.go" {
		t.Fatalf("linked index: %+v %v", got, err)
	}
}

type changingIndexReader struct {
	first, second []byte
	reads         int
}

func (r *changingIndexReader) ReadIndexContext(context.Context, int) ([]byte, error) {
	r.reads++
	if r.reads == 1 {
		return r.first, nil
	}
	return r.second, nil
}
func (*changingIndexReader) Verify() error { return nil }

func TestObserveGitIndexRejectsChange(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "one.go")
	f.run(t, nil, "add", "--", "one.go")
	first := f.index(t)
	f.write(t, "two.go")
	f.run(t, nil, "add", "--", "two.go")
	second := f.index(t)
	if bytes.Equal(first, second) {
		t.Fatal("fixture indexes unexpectedly match")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reader := &changingIndexReader{first: first, second: second}
	result, err := observeGitIndex(ctx, reader, "sha1", indexFixtureBudget(), func(context.Context) error { return nil })
	if !errors.Is(err, ErrIndexChanged) || len(result.Index.Entries) != 0 {
		t.Fatalf("changed index returned data: %+v %v", result, err)
	}
}
