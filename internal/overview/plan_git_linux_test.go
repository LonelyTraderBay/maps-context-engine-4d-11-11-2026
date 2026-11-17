//go:build linux

package overview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func fixtureTreeDigest(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			t.Fatal("unexpected nonregular fixture")
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestSpecOnlyPlanInUnbornGitFixture(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	ctx, in := planFixture(t, false)
	repo, home, template := t.TempDir(), t.TempDir(), t.TempDir()
	file, _ := in.Snapshot.File("spec.md")
	if err = os.WriteFile(filepath.Join(repo, "spec.md"), file.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, git, "init", "--quiet", "--template="+template, repo)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture init: %v %s", err, out)
	}
	before := fixtureTreeDigest(t, repo)
	root, err := paths.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	budget := inventory.GitIndexBudget{MaxBytes: 65536, MaxEntries: 100, MaxPathBytes: 4096, MaxDecodedPathBytes: 65536}
	index, err := inventory.ReadLocalGitIndex(ctx, root, "sha1", budget, func(context.Context) error { return nil })
	if err != nil || !index.IndexMissing || len(index.Index.Entries) != 0 {
		t.Fatalf("expected unborn index: %+v %v", index, err)
	}
	walk, err := inventory.WalkCandidates(ctx, root, inventory.WalkBudget{MaxVisited: 100, MaxDirectories: 10, MaxDepth: 4, MaxPathBytes: 4096, MaxTotalPathBytes: 65536}, func(context.Context) error { return nil }, func(context.Context, string, bool) (bool, error) { return true, nil }, func(string, bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range walk.Candidates {
		if candidate.Path == "spec.md" && candidate.Kind == "regular" {
			found = true
		}
	}
	if !found {
		t.Fatal("untracked document not observed")
	}
	captured, err := inventory.CaptureFile(ctx, root, in.Scanner, "spec.md", 65536, func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if captured.ContentHash() != file.ContentHash() {
		t.Fatal("captured bytes differ")
	}
	frozen, err := snapshot.Freeze(in.Snapshot.Manifest(), []snapshot.File{captured}, 65536, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	in.Snapshot = frozen
	plan, err := BuildPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err := plan.Read(ctx)
	if err != nil || report.ProjectStage != "spec_only" || report.ImplementationEvidence != "none" {
		t.Fatalf("spec-only plan: %+v %v", report, err)
	}
	for _, item := range report.Items {
		if item.Plane != "intended" || item.MappingStatus != "unmapped" {
			t.Fatal("document invented a code fact")
		}
	}
	if after := fixtureTreeDigest(t, repo); after != before {
		t.Fatal("read-only engine changed fixture source or Git metadata")
	}
	check, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	cmd = exec.CommandContext(check, git, "-C", repo, "rev-parse", "--verify", "HEAD")
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "fatal") {
		t.Fatal("fixture unexpectedly has a commit")
	}
}
