package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestIndexAndWatchRequireBoundOwnerAndKeepProgressOffStdout(t *testing.T) {
	worktree := "11111111-1111-4111-8111-111111111111"
	profile := strings.Repeat("a", 64)
	for _, mode := range []string{"index", "watch"} {
		var out, stderr bytes.Buffer
		called := false
		deps := Dependencies{CPUs: 4, Index: func(ctx context.Context, config, w, p string, full, watch bool, progress func(IndexProgress) error) error {
			called = true
			if config != "" || w != worktree || p != profile || full || watch != (mode == "watch") {
				t.Fatal("incorrect writer binding")
			}
			return progress(IndexProgress{GenerationID: worktree, SnapshotDigest: profile, Files: 2})
		}}
		code := Run(context.Background(), []string{mode, "--worktree-id", worktree, "--profile-id", profile}, io.NopCloser(strings.NewReader("")), &out, &stderr, deps)
		if code != 0 || !called || out.Len() != 0 || !strings.Contains(stderr.String(), "syntax partial") {
			t.Fatal("foreground command outcome", code, out.String(), stderr.String())
		}
	}
	for _, failure := range []struct {
		err  error
		code int
	}{
		{errors.Join(ErrIndexRecovery, context.DeadlineExceeded), 3},
		{errors.Join(ErrBindingDenied, context.DeadlineExceeded), 4},
		{errors.New("private source path must not leak"), 5},
	} {
		var out, stderr bytes.Buffer
		code := Run(context.Background(), []string{"index", "--worktree-id", worktree, "--profile-id", profile}, io.NopCloser(strings.NewReader("")), &out, &stderr, Dependencies{CPUs: 4, Index: func(context.Context, string, string, string, bool, bool, func(IndexProgress) error) error {
			return failure.err
		}})
		if code != failure.code || out.Len() != 0 || strings.Contains(stderr.String(), "private source") {
			t.Fatal("unsafe command failure", code, stderr.String())
		}
	}
}
