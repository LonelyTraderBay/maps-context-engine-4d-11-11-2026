package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
)

func TestQueryExplicitBindingReachesTrustedOwner(t *testing.T) {
	worktree := "11111111-1111-4111-8111-111111111111"
	profile := strings.Repeat("a", 64)
	called := false
	open := func(_ context.Context, config, w, p string) (*app.CoreService, func() error, error) {
		called = true
		if config != "" || w != worktree || p != profile {
			t.Fatalf("binding changed: %q %q %q", config, w, p)
		}
		return nil, nil, ErrBindingDenied
	}
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"--worktree-id", worktree, "--profile-id", profile, "query", "status", "--input-json", "-"}, io.NopCloser(strings.NewReader("{}")), &out, &diagnostic, Dependencies{CPUs: 4, OpenCore: open})
	if code != 3 || !called || !strings.Contains(out.String(), "ACCESS_DENIED") {
		t.Fatalf("binding was not authorized by owner: %d %s %s", code, out.String(), diagnostic.String())
	}
}

func TestRepoAddUsesOnlyAuthorizedCallback(t *testing.T) {
	worktree := "11111111-1111-4111-8111-111111111111"
	profile := strings.Repeat("a", 64)
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"repo", "add", "--root", "/approved"}, io.NopCloser(strings.NewReader("")), &out, &diagnostic, Dependencies{CPUs: 4, AddRepo: func(_ context.Context, config, root string) (RegisteredRepo, error) {
		if config != "" || root != "/approved" {
			t.Fatal("root changed")
		}
		return RegisteredRepo{WorktreeID: worktree, ProfileID: profile}, nil
	}})
	if code != 0 || !strings.Contains(out.String(), worktree) || !strings.Contains(out.String(), profile) || diagnostic.Len() != 0 {
		t.Fatalf("registration output: %d %s %s", code, out.String(), diagnostic.String())
	}
}
