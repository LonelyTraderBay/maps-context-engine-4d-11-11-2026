package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStoreRecoveryReportRejectsIncompleteInputBeforeOwner(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, args := range [][]string{
		{"store", "recover", "--target"},
		{"store", "recover", "--target", "catalog", "--apply"},
		{"store", "recover", "--target", "worktree", "--worktree-id", "bad", "--report-only"},
		{"store", "recover", "--target", "catalog", "--worktree-id", id, "--report-only"},
		{"store", "recover", "--target", "catalog", "--report-only", "--confirm"},
	} {
		calls := 0
		var out, stderr bytes.Buffer
		deps := Dependencies{CPUs: 1, StoreRecoveryReport: func(context.Context, string, string) (StoreRecoveryReport, error) {
			calls++
			return StoreRecoveryReport{}, nil
		}}
		if code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &stderr, deps); code != 2 || calls != 0 || out.Len() != 0 {
			t.Fatalf("invalid report opened owner: %d %d", code, calls)
		}
	}
	for _, choice := range []struct {
		target   string
		explicit bool
	}{{"catalog", false}, {"catalog", true}, {"worktree", false}, {"worktree", true}} {
		target := choice.target
		worktree := ""
		args := []string{"store", "recover", "--target", target}
		if target == "worktree" {
			worktree = id
			args = append(args, "--worktree-id", id)
		}
		if choice.explicit {
			args = append(args, "--report-only")
		}
		var out, stderr bytes.Buffer
		calls := 0
		deps := Dependencies{CPUs: 1, StoreRecoveryReport: func(ctx context.Context, gotTarget, gotWorktree string) (StoreRecoveryReport, error) {
			calls++
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 10*time.Second || gotTarget != target || gotWorktree != worktree {
				t.Fatal("unbounded or incorrect input")
			}
			return StoreRecoveryReport{Target: target, WorktreeID: worktree, ReportOnly: true, Phase: "none", CompletedSteps: []string{}}, nil
		}}
		if code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &stderr, deps); code != 0 || calls != 1 || !strings.Contains(out.String(), `"actual_state_verified":false`) {
			t.Fatalf("report: %d %s %s", code, out.String(), stderr.String())
		}
	}
}

func TestStoreRecoveryReportRejectsClaimsOfVerification(t *testing.T) {
	r := StoreRecoveryReport{Target: "catalog", ReportOnly: true, Phase: "none", CompletedSteps: []string{}}
	if !validStoreRecoveryReport(r, "catalog", "") {
		t.Fatal("valid empty report rejected")
	}
	r.ActualStateVerified = true
	if validStoreRecoveryReport(r, "catalog", "") {
		t.Fatal("metadata reporter claimed file verification")
	}
}
