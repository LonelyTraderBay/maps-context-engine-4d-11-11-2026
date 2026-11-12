package indexer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func TestReuseCapturedContentKeepsSemanticRebuildAndSnapshotIdentity(t *testing.T) {
	before := invalidationSnapshot(t, map[string]string{"same.go": "same", "changed.go": "old", "old.go": "moved"}, nil)
	after := invalidationSnapshot(t, map[string]string{"same.go": "same", "changed.go": "new", "new.go": "moved"}, nil)
	ctx, invalidation := invalidationRequest(t, before, after)
	input := ContentReuseInput{Invalidation: invalidation, MaxSourceBytes: 65536, MaxReuseFiles: 100}
	result, err := ReuseCapturedContent(ctx, input)
	if err != nil || result.Invalidation.Action != "rebuild" || result.IncrementalMode != "content_only" || result.SemanticMode != "full_rebuild" || !reflect.DeepEqual(result.ReusedPaths, []string{"same.go"}) || result.Captured == nil || result.Captured.Manifest().SnapshotDigest != after.Manifest().SnapshotDigest {
		t.Fatalf("invalid content reuse: %+v %v", result, err)
	}
	for _, item := range after.Manifest().Files {
		got, ok := result.Captured.File(item.Path)
		want, present := after.File(item.Path)
		if !ok || !present || got.ContentHash() != want.ContentHash() || string(got.Bytes()) != string(want.Bytes()) {
			t.Fatalf("reconstructed content mismatch: %s", item.Path)
		}
	}
	policyChanged := invalidationSnapshot(t, map[string]string{"same.go": "same", "changed.go": "new", "new.go": "moved"}, func(m *model.CaptureManifest) { m.PolicyDigest = strings.Repeat("d", 64) })
	input.Invalidation.After = policyChanged
	result, err = ReuseCapturedContent(ctx, input)
	if err != nil || result.SemanticMode != "full_rebuild" || result.IncrementalMode != "full" || len(result.ReusedPaths) != 0 {
		t.Fatalf("content reused across policy change: %+v %v", result, err)
	}
	profileChanged := invalidationSnapshot(t, map[string]string{"same.go": "same"}, func(m *model.CaptureManifest) { m.ProfileID = strings.Repeat("d", 64) })
	input.Invalidation.After = profileChanged
	result, err = ReuseCapturedContent(ctx, input)
	if err != nil || result.SemanticMode != "full_rebuild" || !reflect.DeepEqual(result.ReusedPaths, []string{"same.go"}) {
		t.Fatalf("raw content or full semantic fallback incorrect: %+v %v", result, err)
	}
}

func TestReuseCapturedContentNoopRequiresPriorGenerationProof(t *testing.T) {
	before := invalidationSnapshot(t, map[string]string{"same.go": "same"}, nil)
	ctx, invalidation := invalidationRequest(t, before, before)
	input := ContentReuseInput{Invalidation: invalidation, MaxSourceBytes: 65536, MaxReuseFiles: 100}
	result, err := ReuseCapturedContent(ctx, input)
	if err != nil || result.Invalidation.Action != "rebuild" || result.SemanticMode != "full_rebuild" {
		t.Fatalf("unproved previous result reused: %+v %v", result, err)
	}
	checks := 0
	input.Invalidation.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) {
		checks++
		return true, nil
	}
	result, err = ReuseCapturedContent(ctx, input)
	if err != nil || checks != 3 || result.Invalidation.Action != "noop" || result.SemanticMode != "reuse_published" || result.Captured != before {
		t.Fatalf("verified noop did not reuse prior result: %+v %v", result, err)
	}
	checks = 0
	input.Invalidation.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) {
		checks++
		return checks < 3, nil
	}
	if result, err := ReuseCapturedContent(ctx, input); !errors.Is(err, ErrInvalidationAuthority) || result.Captured != nil || checks != 3 {
		t.Fatalf("stale noop proof returned cached content: %v", err)
	}
	input.Invalidation.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) { return true, nil }
	input.MaxSourceBytes = 2
	if result, err := ReuseCapturedContent(ctx, input); !errors.Is(err, ErrContentReuseLimit) || result.Captured != nil {
		t.Fatalf("noop bypassed content budget: %v", err)
	}
	secret := invalidationSnapshot(t, map[string]string{"secret.go": "api_key=abcdefghijklmnop"}, nil)
	ctx, invalidation = invalidationRequest(t, secret, secret)
	invalidation.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) { return true, nil }
	input.Invalidation, input.MaxSourceBytes = invalidation, 65536
	if result, err := ReuseCapturedContent(ctx, input); !errors.Is(err, policy.ErrSecretBlocked) || result.Captured != nil {
		t.Fatalf("noop bypassed secret scan: %v", err)
	}
}

func TestReuseCapturedContentRejectsBudgetRevocationAndSecrets(t *testing.T) {
	before := invalidationSnapshot(t, map[string]string{"a.go": "a", "b.go": "b"}, nil)
	after := invalidationSnapshot(t, map[string]string{"a.go": "a", "b.go": "b", "c.go": "c"}, nil)
	ctx, invalidation := invalidationRequest(t, before, after)
	input := ContentReuseInput{Invalidation: invalidation, MaxSourceBytes: 65536, MaxReuseFiles: 1}
	if result, err := ReuseCapturedContent(ctx, input); !errors.Is(err, ErrContentReuseLimit) || result.Captured != nil {
		t.Fatalf("reuse limit returned partial capture: %v", err)
	}
	input.MaxReuseFiles = 100
	input.MaxSourceBytes = 2
	if result, err := ReuseCapturedContent(ctx, input); !errors.Is(err, ErrContentReuseLimit) || result.Captured != nil {
		t.Fatalf("source budget returned partial capture: %v", err)
	}
	input.MaxSourceBytes = 65536
	for _, revokeAt := range []int{5, 10} {
		checks := 0
		input.Invalidation.AuthorizeScope = func(context.Context, string, string, string, int64) error {
			checks++
			if checks == revokeAt {
				return errors.New("private policy detail")
			}
			return nil
		}
		if result, err := ReuseCapturedContent(ctx, input); !errors.Is(err, ErrInvalidationAuthority) || result.Captured != nil || checks != revokeAt || strings.Contains(err.Error(), "private") {
			t.Fatalf("revocation at check %d returned cached content: %v", revokeAt, err)
		}
	}
	secret := invalidationSnapshot(t, map[string]string{"secret.go": "api_key=abcdefghijklmnop"}, nil)
	ctx, invalidation = invalidationRequest(t, before, secret)
	input.Invalidation = invalidation
	if result, err := ReuseCapturedContent(ctx, input); !errors.Is(err, policy.ErrSecretBlocked) || result.Captured != nil {
		t.Fatalf("secret content returned from cache: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if result, err := ReuseCapturedContent(canceled, input); !errors.Is(err, context.Canceled) || result.Captured != nil {
		t.Fatalf("cancellation returned cached content: %v", err)
	}
}
