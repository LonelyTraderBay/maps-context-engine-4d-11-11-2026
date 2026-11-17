package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func validationFixture(t *testing.T) (context.Context, ValidateCapturedInput, *snapshot.Snapshot) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	file, err := snapshot.NewFile("main.go", []byte("a😀b\n"), 100)
	if err != nil {
		t.Fatal(err)
	}
	hash, size := file.ContentHash(), int64(file.Len())
	worktree := "11111111-1111-4111-8111-111111111111"
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{{Path: "main.go", Status: "captured", ContentHash: &hash, ByteLength: &size}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 100)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: "22222222-2222-4222-8222-222222222222", SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{WorktreeID: worktree, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: "main.go", ContentHash: hash, Range: model.ByteRange{StartByte: 1, EndByte: 5}}
	input := ValidateCapturedInput{Scope: scope, Sources: []ValidationSource{{Ref: ref}}, MaxInputBytes: 8192, MaxManifestBytes: 8192,
		CurrentScope: func(context.Context) (model.ScopeVector, error) { return scope, nil }, ResolveCapture: func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error) { return captured, nil },
		AuthorizeSource: func(context.Context, model.ScopeMember, model.SourceRef) error { return nil }, VerifyWorkspace: func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
			return WorkspaceObservedCurrent, nil
		}}
	return ctx, input, captured
}

func TestValidateCapturedWorkspaceRequiresAllChecksAndReturnsBoundedObservation(t *testing.T) {
	ctx, input, _ := validationFixture(t)
	scopes, authorizations, workspaces := 0, 0, 0
	input.CurrentScope = func(context.Context) (model.ScopeVector, error) { scopes++; return input.Scope, nil }
	input.AuthorizeSource = func(context.Context, model.ScopeMember, model.SourceRef) error { authorizations++; return nil }
	input.VerifyWorkspace = func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
		workspaces++
		return WorkspaceObservedCurrent, nil
	}
	result, err := ValidateCapturedWorkspace(ctx, input)
	if err != nil || !result.Data.EditPreconditionValidated || result.Freshness.Status != "observed_current" || result.Freshness.Scope != "workspace" || result.Freshness.BufferScope != "disk_only" || result.Freshness.CheckedAt == nil || result.Freshness.ChangedCount == nil || *result.Freshness.ChangedCount != 0 || scopes != 3 || authorizations != 2 || workspaces != 1 {
		t.Fatalf("incomplete validation checks: %+v %v", result, err)
	}
	data, _ := json.Marshal(result.Data)
	if _, err := model.DecodeValidateDataJSON(data, 8192); err != nil {
		t.Fatal(err)
	}
	freshness, _ := json.Marshal(result.Freshness)
	if _, err := model.DecodeFreshnessJSON(freshness, 8192); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCapturedWorkspaceRejectsChangedEpochsAndSources(t *testing.T) {
	ctx, input, _ := validationFixture(t)
	current := input.Scope
	current.Members = append([]model.ScopeMember(nil), input.Scope.Members...)
	input.CurrentScope = func(context.Context) (model.ScopeVector, error) { return current, nil }
	input.VerifyWorkspace = func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
		current.Members[0].RecordEpoch++
		current.VectorDigest, _ = model.ComputeScopeVectorDigest(current, 8192)
		return WorkspaceObservedCurrent, nil
	}
	if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrValidationScopeChanged) || got.Data.EditPreconditionValidated {
		t.Fatalf("changed record epoch accepted: %v", err)
	}
	ctx, input, _ = validationFixture(t)
	checks := 0
	input.AuthorizeSource = func(context.Context, model.ScopeMember, model.SourceRef) error {
		checks++
		if checks == 2 {
			return errors.New("private policy detail")
		}
		return nil
	}
	if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrValidationAuthority) || got.Data.EditPreconditionValidated || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked source accepted: %v", err)
	}
	for name, mutate := range map[string]func(*model.SourceRef){
		"generation": func(r *model.SourceRef) { r.GenerationID = "66666666-6666-4666-8666-666666666666" },
		"hash":       func(r *model.SourceRef) { r.ContentHash = strings.Repeat("d", 64) },
		"utf8":       func(r *model.SourceRef) { r.Range.StartByte = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, request, _ := validationFixture(t)
			mutate(&request.Sources[0].Ref)
			if got, err := ValidateCapturedWorkspace(ctx, request); !errors.Is(err, ErrValidationPrecondition) || got.Data.EditPreconditionValidated {
				t.Fatalf("mismatched source accepted: %v", err)
			}
		})
	}
}

func TestValidateCapturedWorkspaceRejectsAllPinnedRevisionChanges(t *testing.T) {
	for name, mutate := range map[string]func(*model.ScopeVector){
		"policy_epoch":    func(v *model.ScopeVector) { v.Members[0].PolicyEpoch++ },
		"policy_digest":   func(v *model.ScopeVector) { v.Members[0].PolicyDigest = strings.Repeat("d", 64) },
		"binding":         func(v *model.ScopeVector) { v.Members[0].BindingRevision++ },
		"record":          func(v *model.ScopeVector) { v.Members[0].RecordEpoch++ },
		"profile":         func(v *model.ScopeVector) { v.Members[0].ProfileID = strings.Repeat("d", 64) },
		"snapshot":        func(v *model.ScopeVector) { v.Members[0].SnapshotDigest = strings.Repeat("d", 64) },
		"catalog_restore": func(v *model.ScopeVector) { v.CatalogIncarnation = "66666666-6666-4666-8666-666666666666" },
		"index_restore":   func(v *model.ScopeVector) { v.Members[0].IndexIncarnation = "66666666-6666-4666-8666-666666666666" },
		"record_restore":  func(v *model.ScopeVector) { v.Members[0].RecordsIncarnation = "66666666-6666-4666-8666-666666666666" },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, input, _ := validationFixture(t)
			current := input.Scope
			current.Members = append([]model.ScopeMember(nil), input.Scope.Members...)
			input.CurrentScope = func(context.Context) (model.ScopeVector, error) { return current, nil }
			input.VerifyWorkspace = func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
				mutate(&current)
				current.VectorDigest, _ = model.ComputeScopeVectorDigest(current, 8192)
				return WorkspaceObservedCurrent, nil
			}
			if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrValidationScopeChanged) || got.Data.EditPreconditionValidated {
				t.Fatalf("changed pinned revision accepted: %v", err)
			}
		})
	}
}

func TestValidateCapturedWorkspaceNeverPromotesUnknownOrChanged(t *testing.T) {
	ctx, input, _ := validationFixture(t)
	for _, tc := range []struct {
		observation WorkspaceObservation
		err         error
		want        error
	}{{WorkspaceChanged, nil, ErrFreshnessChanged}, {WorkspaceUnknown, nil, ErrFreshnessUnknown}, {"", errors.New("private"), ErrFreshnessUnknown}, {"invented", nil, ErrValidationAuthority}} {
		input.VerifyWorkspace = func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
			return tc.observation, tc.err
		}
		if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, tc.want) || got.Data.EditPreconditionValidated {
			t.Fatalf("unsafe workspace result accepted: %v", err)
		}
	}
	timed, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	if got, err := ValidateCapturedWorkspace(timed, input); !errors.Is(err, ErrFreshnessUnknown) || got.Data.EditPreconditionValidated {
		t.Fatalf("deadline claimed current: %v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if got, err := ValidateCapturedWorkspace(canceled, input); !errors.Is(err, context.Canceled) || got.Data.EditPreconditionValidated {
		t.Fatalf("cancellation claimed current: %v", err)
	}
	input.Sources = nil
	if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrValidationInput) || got.Data.EditPreconditionValidated {
		t.Fatalf("empty preconditions claimed validation: %v", err)
	}
}

func TestValidateCapturedWorkspaceRejectsUnobservedCapture(t *testing.T) {
	for _, status := range []string{"metadata_only", "read_error"} {
		t.Run(status, func(t *testing.T) {
			ctx, input, original := validationFixture(t)
			manifest := original.Manifest()
			manifest.Files = append(manifest.Files, model.ManifestFile{Path: "unobserved.go", Status: status})
			manifest.SnapshotDigest = ""
			file, ok := original.File("main.go")
			if !ok {
				t.Fatal("fixture source missing")
			}
			captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 100)
			if err != nil {
				t.Fatal(err)
			}
			scope := input.Scope
			scope.Members = append([]model.ScopeMember(nil), scope.Members...)
			scope.Members[0].SnapshotDigest = captured.Manifest().SnapshotDigest
			scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
			if err != nil {
				t.Fatal(err)
			}
			input.Scope = scope
			input.CurrentScope = func(context.Context) (model.ScopeVector, error) { return scope, nil }
			input.ResolveCapture = func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error) { return captured, nil }
			if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrFreshnessUnknown) || got.Data.EditPreconditionValidated {
				t.Fatalf("%s capture falsely validated: %v", status, err)
			}
		})
	}
}

func TestValidateCapturedWorkspaceChecksEveryFederatedMember(t *testing.T) {
	ctx, input, first := validationFixture(t)
	manifest := first.Manifest()
	file, ok := first.File("main.go")
	if !ok {
		t.Fatal("fixture source missing")
	}
	manifest.WorktreeID = "77777777-7777-4777-8777-777777777777"
	manifest.SnapshotDigest = ""
	second, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 100)
	if err != nil {
		t.Fatal(err)
	}
	member := input.Scope.Members[0]
	member.WorktreeID = manifest.WorktreeID
	member.GenerationID = "88888888-8888-4888-8888-888888888888"
	member.SnapshotDigest = second.Manifest().SnapshotDigest
	group, revision := "99999999-9999-4999-8999-999999999999", int64(1)
	scope := input.Scope
	scope.Kind, scope.GroupID, scope.GroupRevision = "federated", &group, &revision
	scope.Members = append(append([]model.ScopeMember(nil), scope.Members...), member)
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	input.Scope = scope
	input.CurrentScope = func(context.Context) (model.ScopeVector, error) { return scope, nil }
	input.ResolveCapture = func(_ context.Context, m model.ScopeMember) (*snapshot.Snapshot, error) {
		if m.WorktreeID == manifest.WorktreeID {
			return second, nil
		}
		return first, nil
	}
	checks := 0
	input.VerifyWorkspace = func(_ context.Context, m model.ScopeMember, _ *snapshot.Snapshot) (WorkspaceObservation, error) {
		checks++
		if m.WorktreeID == manifest.WorktreeID {
			return WorkspaceUnknown, nil
		}
		return WorkspaceObservedCurrent, nil
	}
	if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrFreshnessUnknown) || got.Data.EditPreconditionValidated || checks != 2 {
		t.Fatalf("unverified federated member accepted: checks=%d err=%v", checks, err)
	}
	input.VerifyWorkspace = func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
		return WorkspaceObservedCurrent, nil
	}
	reads := 0
	input.CurrentScope = func(context.Context) (model.ScopeVector, error) {
		reads++
		if reads == 2 {
			changed := scope
			next := revision + 1
			changed.GroupRevision = &next
			changed.VectorDigest, _ = model.ComputeScopeVectorDigest(changed, 8192)
			return changed, nil
		}
		return scope, nil
	}
	if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrValidationScopeChanged) || got.Data.EditPreconditionValidated {
		t.Fatalf("changed group revision accepted: %v", err)
	}
	unavailable := scope
	unavailable.UnavailableMembers = []model.MemberUnavailable{{Status: "index_missing", ReasonCode: "NO_INDEX"}}
	unavailable.VectorDigest, err = model.ComputeScopeVectorDigest(unavailable, 8192)
	if err != nil {
		t.Fatal(err)
	}
	input.Scope = unavailable
	if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrFreshnessUnknown) || got.Data.EditPreconditionValidated {
		t.Fatalf("unavailable member accepted: %v", err)
	}
}

func TestValidateCapturedWorkspaceCopiesCallbackInputsAndBoundsSources(t *testing.T) {
	ctx, input, captured := validationFixture(t)
	service := "service-a"
	input.Scope.Members[0].ServiceID = &service
	var err error
	input.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(input.Scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	input.Sources[0].ServiceID = &service
	input.CurrentScope = func(context.Context) (model.ScopeVector, error) { return input.Scope, nil }
	input.ResolveCapture = func(_ context.Context, member model.ScopeMember) (*snapshot.Snapshot, error) {
		*member.ServiceID = "mutated"
		return captured, nil
	}
	input.VerifyWorkspace = func(_ context.Context, member model.ScopeMember, _ *snapshot.Snapshot) (WorkspaceObservation, error) {
		*member.ServiceID = "mutated"
		return WorkspaceObservedCurrent, nil
	}
	if got, err := ValidateCapturedWorkspace(ctx, input); err != nil || !got.Data.EditPreconditionValidated || service != "service-a" {
		t.Fatalf("callback changed pinned member identity: %v", err)
	}
	input.Sources[0].Ref.Path = strings.Repeat("a", input.MaxInputBytes+1)
	if got, err := ValidateCapturedWorkspace(ctx, input); !errors.Is(err, ErrValidationInput) || got.Data.EditPreconditionValidated {
		t.Fatalf("oversized source metadata accepted: %v", err)
	}
}
