package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRestoreRequiresConfirmedExactSelection(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "backup.json")
	for _, args := range [][]string{
		{"store", "restore", "--manifest", manifest, "--confirm", "--wait-ms"},
		{"store", "restore", "--manifest", manifest, "--wait-ms", "50"},
		{"store", "restore", "--manifest", manifest, "--confirm", "--wait-ms", "30001"},
		{"store", "restore", "--manifest", "backup.json", "--confirm", "--wait-ms", "50"},
		{"store", "recover", "--target", "catalog", "--report-only", "--apply", "--confirm", "--wait-ms", "50"},
		{"store", "recover", "--target", "worktree", "--worktree-id", "bad", "--apply", "--confirm", "--wait-ms", "50"},
	} {
		var out, stderr bytes.Buffer
		deps := Dependencies{CPUs: 1, StoreRestore: func(context.Context, string, string, string, time.Duration) (StoreRestoreReport, error) {
			t.Error("invalid CLI request reached offline owner")
			return StoreRestoreReport{}, nil
		}}
		if code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &stderr, deps); code != 2 || out.Len() != 0 {
			t.Fatalf("%v: exit %d output %s", args, code, out.String())
		}
	}
}

func TestStoreRebuildIndexRequiresConfirmedWorktreeAndFourSteps(t *testing.T) {
	const worktree = "33333333-3333-4333-8333-333333333333"
	calls := 0
	deps := Dependencies{CPUs: 1, StoreRebuildIndex: func(_ context.Context, got string, wait time.Duration) (StoreRestoreReport, error) {
		calls++
		if got != worktree || wait != 50*time.Millisecond {
			t.Fatalf("unexpected rebuild selection %s %s", got, wait)
		}
		return StoreRestoreReport{
			OperationID: "44444444-4444-4444-8444-444444444444", Target: "worktree", WorktreeID: got, Outcome: "restored",
			CompletedSteps: []string{"index_main", "index_journal", "index_head", "worktree_manifest"},
		}, nil
	}}
	for _, args := range [][]string{
		{"store", "rebuild-index", "--worktree-id", "bad", "--confirm", "--wait-ms", "50"},
		{"store", "rebuild-index", "--worktree-id", worktree, "--wait-ms", "50"},
		{"store", "rebuild-index", "--worktree-id", worktree, "--confirm", "--wait-ms", "050"},
	} {
		if code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), io.Discard, io.Discard, deps); code != 2 || calls != 0 {
			t.Fatalf("%v: exit %d, controller calls %d", args, code, calls)
		}
	}
	args := []string{"store", "rebuild-index", "--worktree-id", worktree, "--confirm", "--wait-ms", "50"}
	var out bytes.Buffer
	if code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, io.Discard, deps); code != 0 || calls != 1 || !strings.Contains(out.String(), "worktree_manifest") || strings.Contains(out.String(), "records_main") {
		t.Fatalf("exit %d, controller calls %d, output %s", code, calls, out.String())
	}
}
