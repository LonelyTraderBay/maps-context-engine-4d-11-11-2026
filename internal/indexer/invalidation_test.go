package indexer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func invalidationSnapshot(t *testing.T, files map[string]string, edit func(*model.CaptureManifest)) *snapshot.Snapshot {
	t.Helper()
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	var captured []snapshot.File
	for name, body := range files {
		file, err := snapshot.NewFile(name, []byte(body), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size})
		captured = append(captured, file)
	}
	if edit != nil {
		edit(&manifest)
	}
	result, err := snapshot.Freeze(manifest, captured, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func invalidationRequest(t *testing.T, before, after *snapshot.Snapshot) (context.Context, InvalidationInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, InvalidationInput{Before: before, After: after, PolicyEpoch: 1, Scanner: scanner, MaxManifestBytes: 65536, MaxChanges: 1000, MaxOutputBytes: 262144,
		AuthorizeScope: func(context.Context, string, string, string, int64) error { return nil }, AuthorizePath: func(context.Context, string, string, string, string, int64) error { return nil }}
}

func TestInvalidationTracksContentMembershipAndRenameWithoutGuessing(t *testing.T) {
	before := invalidationSnapshot(t, map[string]string{"a.go": "one", "keep.go": "same", "old/path.go": "moved", "go.mod": "module a"}, nil)
	after := invalidationSnapshot(t, map[string]string{"a.go": "two", "keep.go": "same", "new/path.go": "moved", "go.mod": "module b", "added.go": "new"}, nil)
	ctx, request := invalidationRequest(t, before, after)
	request.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) {
		t.Fatal("changed input requested reuse proof")
		return true, nil
	}
	plan, err := PlanInvalidation(ctx, request)
	if err != nil || plan.Action != "rebuild" || plan.IncrementalMode != "full" || plan.BeforeDigest != before.Manifest().SnapshotDigest || plan.AfterDigest != after.Manifest().SnapshotDigest {
		t.Fatalf("invalid changed plan: %+v %v", plan, err)
	}
	var got []string
	for _, change := range plan.Changes {
		got = append(got, change.Path+":"+change.Kind)
		if change.Kind == "added" && (change.Before != nil || change.After == nil) || change.Kind == "removed" && (change.Before == nil || change.After != nil) || change.Kind == "modified" && (change.Before == nil || change.After == nil) {
			t.Fatalf("invalid change boundary: %+v", change)
		}
	}
	want := []string{"a.go:modified", "added.go:added", "go.mod:modified", "new/path.go:added", "old/path.go:removed"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(plan.Reasons, []string{"FILE_INPUT_CHANGED", "GO_BUILD_INPUT_CHANGED", "MEMBERSHIP_CHANGED"}) {
		t.Fatalf("wrong ordered changes or reasons: %+v %+v", got, plan.Reasons)
	}
	if *plan.Changes[0].Before.ByteLength != *plan.Changes[0].After.ByteLength || *plan.Changes[0].Before.ContentHash == *plan.Changes[0].After.ContentHash {
		t.Fatal("same-size changed bytes were not recognized")
	}
}

func TestInvalidationRequiresProofBeforeNoopAndNeverPromotesPartial(t *testing.T) {
	before := invalidationSnapshot(t, map[string]string{"same.go": "package same\n"}, nil)
	ctx, request := invalidationRequest(t, before, before)
	plan, err := PlanInvalidation(ctx, request)
	if err != nil || plan.Action != "rebuild" || !reflect.DeepEqual(plan.Reasons, []string{"REUSE_PROOF_MISSING"}) {
		t.Fatalf("unproved noop: %+v %v", plan, err)
	}
	request.VerifyReuse = func(_ context.Context, first, second model.CaptureManifest) (bool, error) {
		first.Files[0].Path = "mutated.go"
		second.Files[0].Path = "other.go"
		return true, nil
	}
	plan, err = PlanInvalidation(ctx, request)
	if err != nil || plan.Action != "noop" || len(plan.Changes) != 0 || len(plan.Reasons) != 0 || before.Manifest().Files[0].Path != "same.go" {
		t.Fatalf("verified noop failed or callback mutated capture: %+v %v", plan, err)
	}
	checks := 0
	request.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) {
		checks++
		return checks == 1, nil
	}
	plan, err = PlanInvalidation(ctx, request)
	if err != nil || checks != 2 || plan.Action != "rebuild" || !reflect.DeepEqual(plan.Reasons, []string{"REUSE_UNVERIFIED"}) {
		t.Fatalf("changed reuse conditions were accepted: %+v %v", plan, err)
	}
	request.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) { return false, nil }
	plan, err = PlanInvalidation(ctx, request)
	if err != nil || plan.Action != "rebuild" || !reflect.DeepEqual(plan.Reasons, []string{"REUSE_UNVERIFIED"}) {
		t.Fatalf("unverified prior result was reused: %+v %v", plan, err)
	}
	incomplete := invalidationSnapshot(t, nil, func(m *model.CaptureManifest) {
		reason := "excluded_by_policy"
		m.Files = append(m.Files, model.ManifestFile{Path: "hidden.go", Status: "excluded", ReasonCode: &reason})
	})
	ctx, request = invalidationRequest(t, incomplete, incomplete)
	request.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) {
		t.Fatal("incomplete capture attempted reuse")
		return true, nil
	}
	plan, err = PlanInvalidation(ctx, request)
	if err != nil || plan.Action != "rebuild" || !reflect.DeepEqual(plan.Reasons, []string{"CAPTURE_INCOMPLETE"}) {
		t.Fatalf("incomplete capture became noop: %+v %v", plan, err)
	}
}

func TestInvalidationRebuildsOnProfilePolicyProviderAndDependencyChanges(t *testing.T) {
	files := map[string]string{"a.go": "same"}
	before := invalidationSnapshot(t, files, nil)
	cases := []struct {
		name, reason string
		edit         func(*model.CaptureManifest)
	}{
		{"profile", "PROFILE_CHANGED", func(m *model.CaptureManifest) { m.ProfileID = strings.Repeat("d", 64) }},
		{"policy", "POLICY_CHANGED", func(m *model.CaptureManifest) { m.PolicyDigest = strings.Repeat("d", 64) }},
		{"provider", "PROVIDER_CHANGED", func(m *model.CaptureManifest) { m.ProviderDigest = strings.Repeat("d", 64) }},
		{"dependency", "DEPENDENCY_INPUTS_CHANGED", func(m *model.CaptureManifest) { m.DependencyInputDigests = []string{strings.Repeat("d", 64)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			after := invalidationSnapshot(t, files, tc.edit)
			ctx, request := invalidationRequest(t, before, after)
			plan, err := PlanInvalidation(ctx, request)
			if err != nil || plan.Action != "rebuild" || plan.IncrementalMode != "full" || len(plan.Changes) != 0 || !reflect.DeepEqual(plan.Reasons, []string{tc.reason}) {
				t.Fatalf("input identity change missed: %+v %v", plan, err)
			}
		})
	}
}

func TestInvalidationRejectsRevocationCancellationAndBudgets(t *testing.T) {
	before := invalidationSnapshot(t, map[string]string{"a.go": "a", "b.go": "b"}, nil)
	after := invalidationSnapshot(t, map[string]string{"a.go": "A", "c.go": "c"}, nil)
	ctx, request := invalidationRequest(t, before, after)
	request.MaxChanges = 1
	if plan, err := PlanInvalidation(ctx, request); !errors.Is(err, ErrInvalidationLimit) || len(plan.Changes) != 0 {
		t.Fatalf("change limit returned partial plan: %v", err)
	}
	request.MaxChanges = 1000
	scopeChecks := 0
	request.AuthorizeScope = func(context.Context, string, string, string, int64) error {
		scopeChecks++
		if scopeChecks == 3 {
			return errors.New("private policy detail")
		}
		return nil
	}
	if plan, err := PlanInvalidation(ctx, request); !errors.Is(err, ErrInvalidationAuthority) || len(plan.Changes) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("revocation returned plan: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if plan, err := PlanInvalidation(canceled, request); !errors.Is(err, context.Canceled) || len(plan.Changes) != 0 {
		t.Fatalf("cancellation returned plan: %v", err)
	}
	other := invalidationSnapshot(t, map[string]string{"a.go": "a"}, func(m *model.CaptureManifest) { m.WorktreeID = "22222222-2222-4222-8222-222222222222" })
	_, request = invalidationRequest(t, before, other)
	if plan, err := PlanInvalidation(ctx, request); !errors.Is(err, ErrInvalidationInput) || len(plan.Changes) != 0 {
		t.Fatalf("cross-worktree comparison accepted: %v", err)
	}
	secret := invalidationSnapshot(t, nil, func(m *model.CaptureManifest) {
		reason := "api_key=abcdefghijklmnop"
		m.Files = append(m.Files, model.ManifestFile{Path: "hidden.go", Status: "excluded", ReasonCode: &reason})
	})
	_, request = invalidationRequest(t, secret, secret)
	if plan, err := PlanInvalidation(ctx, request); !errors.Is(err, policy.ErrSecretBlocked) || len(plan.Changes) != 0 {
		t.Fatalf("secret-bearing reason returned a plan: %v", err)
	}
	denied := invalidationSnapshot(t, nil, func(m *model.CaptureManifest) {
		reason := "excluded_by_policy"
		m.Files = append(m.Files, model.ManifestFile{Path: ".env", Status: "excluded", ReasonCode: &reason})
	})
	_, request = invalidationRequest(t, denied, denied)
	if plan, err := PlanInvalidation(ctx, request); !errors.Is(err, policy.ErrDeniedPath) || len(plan.Changes) != 0 {
		t.Fatalf("denied path returned a plan: %v", err)
	}
}
