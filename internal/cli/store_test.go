package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStoreGCDefaultsToReportAndRequiresExplicitConfirmedApply(t *testing.T) {
	worktree := "11111111-1111-4111-8111-111111111111"
	profile := strings.Repeat("a", 64)
	base := []string{"store", "gc", "--worktree-id", worktree, "--profile-id", profile}
	for _, suffix := range [][]string{nil, {"--report-only"}, {"--apply", "--confirm"}} {
		var out, stderr bytes.Buffer
		calls := 0
		apply := len(suffix) == 2
		deps := Dependencies{CPUs: 1, StoreGC: func(ctx context.Context, selected, w, p string, gotApply bool) (StoreGCReport, error) {
			calls++
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Minute || selected != "" || w != worktree || p != profile || gotApply != apply {
				t.Fatal("incorrect GC scope/deadline/action")
			}
			return StoreGCReport{WorktreeID: w, ProfileID: p, KeepGenerations: 2, DryRun: !gotApply, Generations: []StoreGCGeneration{}}, nil
		}}
		args := append(append([]string{}, base...), suffix...)
		code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
		var report StoreGCReport
		if code != 0 || calls != 1 || stderr.Len() != 0 || json.Unmarshal(out.Bytes(), &report) != nil || report.DryRun == apply {
			t.Fatalf("GC outcome: %d %s %s", code, out.String(), stderr.String())
		}
	}
	for _, suffix := range [][]string{{"--apply"}, {"--confirm"}, {"--apply", "--confirm", "--force"}, {"--report-only", "--confirm"}, {"--report-only", "--apply"}} {
		var out, stderr bytes.Buffer
		calls := 0
		deps := Dependencies{CPUs: 1, StoreGC: func(context.Context, string, string, string, bool) (StoreGCReport, error) {
			calls++
			return StoreGCReport{}, nil
		}}
		code := Run(context.Background(), append(append([]string{}, base...), suffix...), io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
		if code != 2 || calls != 0 || out.Len() != 0 {
			t.Fatalf("invalid flags opened owner: %d %d", code, calls)
		}
	}
}

func TestStoreGCDoesNotExposeOwnerErrorsOrUnapprovedOutput(t *testing.T) {
	worktree := "11111111-1111-4111-8111-111111111111"
	profile := strings.Repeat("a", 64)
	for _, badOutput := range []bool{false, true} {
		var out, stderr bytes.Buffer
		deps := Dependencies{CPUs: 1, StoreGC: func(context.Context, string, string, string, bool) (StoreGCReport, error) {
			if !badOutput {
				return StoreGCReport{}, errors.New("private path or secret from driver")
			}
			return StoreGCReport{WorktreeID: worktree, ProfileID: profile, KeepGenerations: 2, DryRun: true, Generations: []StoreGCGeneration{{GenerationID: worktree, Rows: map[string]int64{"private path or secret from driver": 1}}}}, nil
		}}
		code := Run(context.Background(), []string{"store", "gc", "--worktree-id", worktree, "--profile-id", profile}, io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
		if code == 0 || out.Len() != 0 || strings.Contains(stderr.String(), "private path") {
			t.Fatalf("unsafe GC response: %d %s %s", code, out.String(), stderr.String())
		}
	}
}
