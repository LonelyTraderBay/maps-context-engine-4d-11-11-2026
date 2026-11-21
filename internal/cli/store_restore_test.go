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
