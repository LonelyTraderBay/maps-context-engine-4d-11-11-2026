//go:build linux

package overview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestApprovedAliasConfigCapturedFromUnbornGitFixture(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	ctx, in := configFixture(t, testAliasConfig(t), "vocabulary.json")
	repo, home, template := t.TempDir(), t.TempDir(), t.TempDir()
	for _, name := range []string{"spec.md", "vocabulary.json"} {
		file, ok := in.Snapshot.File(name)
		if !ok {
			t.Fatal("fixture source missing")
		}
		if err = os.WriteFile(filepath.Join(repo, name), file.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
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
	index, err := inventory.ReadLocalGitIndex(ctx, root, "sha1", inventory.GitIndexBudget{MaxBytes: 65536, MaxEntries: 100, MaxPathBytes: 4096, MaxDecodedPathBytes: 65536}, func(context.Context) error { return nil })
	if err != nil || !index.IndexMissing {
		t.Fatal("fixture did not have an unborn index")
	}
	files := []snapshot.File{}
	for _, name := range []string{"spec.md", "vocabulary.json"} {
		file, err := inventory.CaptureFile(ctx, root, in.Scanner, name, 65536, func(context.Context, string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	frozen, err := snapshot.Freeze(in.Snapshot.Manifest(), files, 65536, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	in.Snapshot = frozen
	aliases, err := BuildAliasIndexFromConfig(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	result, err := aliases.Lookup(ctx, "PaymentService")
	if err != nil || !result.Ambiguous || len(result.Candidates) != 2 {
		t.Fatal("captured approved configuration did not preserve ambiguity")
	}
	if fixtureTreeDigest(t, repo) != before {
		t.Fatal("alias lookup changed source or Git metadata")
	}
}
