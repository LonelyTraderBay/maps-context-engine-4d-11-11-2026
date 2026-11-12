//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func ignoreCaptureBudget() IgnoreCaptureBudget {
	return IgnoreCaptureBudget{MaxFiles: 100, MaxFileBytes: 4096, MaxTotalBytes: 16384}
}

func TestCaptureDirectoryIgnoreSourcesUsesSecureSnapshots(t *testing.T) {
	f := newGitFixture(t, "sha1")
	if err := os.WriteFile(filepath.Join(f.root, ".gitignore"), []byte("*.log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.root, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "sub", ".gitignore"), []byte("*.tmp\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	allowScope := func(context.Context) error { return nil }
	walk, err := WalkCandidates(ctx, root, walkBudget(), allowScope, func(context.Context, string, bool) (bool, error) { return true, nil }, func(string, bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := CaptureDirectoryIgnoreSources(ctx, root, walk, scanner, ignoreCaptureBudget(), func(context.Context, string) error { return nil })
	if err != nil || len(sources) != 2 || sources[0].Base != "" || string(sources[0].Rules) != "*.log\n" || sources[1].Base != "sub" || string(sources[1].Rules) != "*.tmp\n" {
		t.Fatalf("captured rule sources: %+v %v", sources, err)
	}
	if err := os.Remove(filepath.Join(f.root, "sub", ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if got, err := CaptureDirectoryIgnoreSources(ctx, root, walk, scanner, ignoreCaptureBudget(), func(context.Context, string) error { return nil }); !errors.Is(err, ErrWalkChanged) || got != nil {
		t.Fatalf("removed rule source accepted: %+v %v", got, err)
	}
}

func TestCaptureDirectoryIgnoreSourcesRejectsDeniedRules(t *testing.T) {
	f := newGitFixture(t, "sha1")
	if err := os.WriteFile(filepath.Join(f.root, ".gitignore"), []byte("*.log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	walk, err := WalkCandidates(ctx, root, walkBudget(), func(context.Context) error { return nil }, func(_ context.Context, path string, _ bool) (bool, error) { return path != ".gitignore", nil }, func(string, bool) bool { return false })
	if err != nil || !walk.IgnoreRulesUnknown {
		t.Fatalf("denied rule source was not reported: %+v %v", walk, err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := CaptureDirectoryIgnoreSources(ctx, root, walk, scanner, ignoreCaptureBudget(), func(context.Context, string) error { return nil }); !errors.Is(err, ErrIgnoreUnsupported) || got != nil {
		t.Fatalf("unknown rule source accepted: %+v %v", got, err)
	}
}

func TestCaptureInfoExcludeDistinguishesMissingAndPresent(t *testing.T) {
	f := newGitFixture(t, "sha1")
	metadata, err := paths.OpenRoot(filepath.Join(f.root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	file := filepath.Join(f.root, ".git", "info", "exclude")
	if err := os.WriteFile(file, []byte("*.tmp\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := CaptureInfoExclude(ctx, metadata, scanner, 4096, func(context.Context, string) error { return nil })
	if err != nil || source == nil || source.Kind != "info" || string(source.Rules) != "*.tmp\n" {
		t.Fatalf("present info/exclude: %+v %v", source, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	source, err = CaptureInfoExclude(ctx, metadata, scanner, 4096, func(context.Context, string) error { return nil })
	if err != nil || source != nil {
		t.Fatalf("missing info/exclude: %+v %v", source, err)
	}
}

func TestCaptureIgnoreRejectsRulesChangedBetweenReads(t *testing.T) {
	f := newGitFixture(t, "sha1")
	rulePath := filepath.Join(f.root, ".gitignore")
	if err := os.WriteFile(rulePath, []byte("*.log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	walk, err := WalkCandidates(ctx, root, walkBudget(), func(context.Context) error { return nil }, func(context.Context, string, bool) (bool, error) { return true, nil }, func(string, bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	got, err := CaptureDirectoryIgnoreSources(ctx, root, walk, scanner, ignoreCaptureBudget(), func(context.Context, string) error {
		calls++
		if calls == 3 {
			return os.WriteFile(rulePath, []byte("*.tmp\n"), 0600)
		}
		return nil
	})
	if !errors.Is(err, ErrWalkChanged) || got != nil {
		t.Fatalf("changed ignore rules accepted: %+v %v", got, err)
	}
}

func TestCaptureApprovedGlobalIgnoreStaysWithinAuthorizedRoot(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "ignore"), []byte("*.tmp\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allow := func(context.Context, string) error { return nil }
	source, err := CaptureApprovedGlobalIgnore(ctx, root, "ignore", scanner, 4096, allow)
	if err != nil || source == nil || source.Kind != "global" || string(source.Rules) != "*.tmp\n" {
		t.Fatalf("approved global rules: %+v %v", source, err)
	}
	if source, err := CaptureApprovedGlobalIgnore(ctx, root, "../outside", scanner, 4096, allow); err == nil || source != nil {
		t.Fatalf("outside path accepted: %+v %v", source, err)
	}
}
