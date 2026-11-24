package compose

import (
	"context"
	"errors"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func TestExplorerPublishedReadBindsPathToGenerationPolicy(t *testing.T) {
	ctx := context.Background()
	principal := app.CorePrincipal{ID: "reader"}
	member := model.ScopeMember{WorktreeID: "worktree", ProfileID: "profile", PolicyDigest: "policy", PolicyEpoch: 4}
	calls := 0
	authority := &BoundCoreAuthority{
		AuthorizePath: func(context.Context, app.CorePrincipal, model.PolicyConfig, string) error {
			t.Fatal("a bounded path repeated the full authorization")
			return ErrAuthority
		},
		AuthorizeBoundPath: func(_ context.Context, actor app.CorePrincipal, config model.PolicyConfig, path string) error {
			calls++
			if actor != principal {
				return ErrAuthority
			}
			return policy.CheckConfiguredPath(path, config.DeniedGlobs)
		},
	}
	e := &ExplorerReader{core: &CoreBackend{
		Authority: authority, Reader: &ScopeReader{MaxManifestBytes: 1 << 20}, limits: model.DefaultRuntimeConfig().Core,
	}}
	observed := ScopeObservation{
		Vector: model.ScopeVector{Members: []model.ScopeMember{member}},
		Binding: catalog.Binding{Policy: model.PolicyConfig{
			MaxFileBytes: 1024, MaxSourceBytes: 4096, DeniedGlobs: []string{"private.go"},
		}},
	}
	input, _, err := e.publishedRead(principal, observed)
	if err != nil {
		t.Fatal(err)
	}
	if err := input.AuthorizePath(ctx, member.WorktreeID, "source.go", member.PolicyDigest, member.PolicyEpoch-1); !errors.Is(err, ErrScopeChanged) || calls != 0 {
		t.Fatalf("mismatched policy epoch: %v, path checks: %d", err, calls)
	}
	if err := input.AuthorizeGeneration(ctx, member.WorktreeID, "other-profile", member.PolicyDigest, member.PolicyEpoch); !errors.Is(err, ErrScopeChanged) {
		t.Fatalf("mismatched generation profile: %v", err)
	}
	if err := input.AuthorizePath(ctx, member.WorktreeID, "private.go", member.PolicyDigest, member.PolicyEpoch); !errors.Is(err, ErrAuthority) {
		t.Fatalf("denied path: %v", err)
	}
	if err := input.AuthorizePath(ctx, member.WorktreeID, "source.go", member.PolicyDigest, member.PolicyEpoch); err != nil || calls != 2 {
		t.Fatalf("allowed path: %v, path checks: %d", err, calls)
	}
}
