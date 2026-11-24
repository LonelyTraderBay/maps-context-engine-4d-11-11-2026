package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreBackupValidatesCompleteSelectionBeforeOpeningOwner(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	output := filepath.Join(t.TempDir(), "new-backup")
	for _, target := range []string{"catalog", "worktree"} {
		args := []string{"store", "backup", "--target", target}
		worktree := ""
		count := 2
		if target == "worktree" {
			worktree = id
			count = 3
			args = append(args, "--worktree-id", id)
		}
		args = append(args, "--output", output)
		var out, stderr bytes.Buffer
		calls := 0
		deps := Dependencies{CPUs: 1, StoreBackup: func(ctx context.Context, selected, gotTarget, gotWorktree, gotOutput string) (StoreBackupReport, error) {
			calls++
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Minute || selected != "" || gotTarget != target || gotWorktree != worktree || gotOutput != output {
				t.Fatal("incorrect backup inputs or deadline")
			}
			return StoreBackupReport{OperationID: id, Target: target, WorktreeID: worktree, ManifestSHA256: strings.Repeat("a", 64), FileCount: count, TotalBytes: 4096}, nil
		}}
		code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
		var report StoreBackupReport
		if code != 0 || calls != 1 || stderr.Len() != 0 || json.Unmarshal(out.Bytes(), &report) != nil || report.FileCount != count {
			t.Fatalf("backup result: %d %s %s", code, out.String(), stderr.String())
		}
	}
	for _, args := range [][]string{
		{"store", "backup", "--target", "other", "--output", output},
		{"store", "backup", "--target", "catalog", "--output", "relative"},
		{"store", "backup", "--target", "catalog", "--output", output, "--confirm"},
		{"store", "backup", "--target", "worktree", "--worktree-id", "../bad", "--output", output},
		{"store", "backup", "--target", "catalog", "--worktree-id", id, "--output", output},
	} {
		var out, stderr bytes.Buffer
		calls := 0
		deps := Dependencies{CPUs: 1, StoreBackup: func(context.Context, string, string, string, string) (StoreBackupReport, error) {
			calls++
			return StoreBackupReport{}, nil
		}}
		code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
		if code != 2 || calls != 0 || out.Len() != 0 {
			t.Fatalf("invalid request opened owner: %d %d", code, calls)
		}
	}
}

func TestStoreBackupSanitizesSourceFailure(t *testing.T) {
	var out, stderr bytes.Buffer
	deps := Dependencies{CPUs: 1, StoreBackup: func(context.Context, string, string, string, string) (StoreBackupReport, error) {
		return StoreBackupReport{}, errors.New("private source path or secret")
	}}
	code := Run(context.Background(), []string{"store", "backup", "--target", "catalog", "--output", filepath.Join(t.TempDir(), "new")}, io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
	if code == 0 || out.Len() != 0 || strings.Contains(stderr.String(), "private source") {
		t.Fatalf("unsafe backup error: %d %s %s", code, out.String(), stderr.String())
	}
}

func TestStoreBackupReportsSchemaTooNew(t *testing.T) {
	var out, stderr bytes.Buffer
	deps := Dependencies{CPUs: 1, StoreBackup: func(context.Context, string, string, string, string) (StoreBackupReport, error) {
		return StoreBackupReport{}, errors.Join(ErrStoreSchemaTooNew, errors.New("private source path"))
	}}
	code := Run(context.Background(), []string{"store", "backup", "--target", "catalog", "--output", filepath.Join(t.TempDir(), "new")}, io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
	if code != 3 || out.Len() != 0 || !strings.HasPrefix(stderr.String(), "mce4d: SCHEMA_TOO_NEW;") || strings.Contains(stderr.String(), "private source path") {
		t.Fatalf("unexpected schema refusal: %d %s %s", code, out.String(), stderr.String())
	}
}
