//go:build linux

package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// Composes the actual bounded inventory and captured-byte revalidation with
// synthetic catalog/generation authorization callbacks; no source is edited
// by the validation operation itself.
func TestValidationUsesFullSyntheticWorkspaceObservation(t *testing.T) {
	request, sourcePath := revalidateFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	manifest := request.Snapshot.Manifest()
	statePath := t.TempDir()
	if err := os.Chmod(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	lease, err := runtime.AcquireLifecycle(ctx, state, manifest.WorktreeID, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	writer, err := runtime.AcquireWriter(ctx, lease, "66666666-6666-4666-8666-666666666666", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	file, ok := request.Snapshot.File("main.go")
	if !ok {
		t.Fatal("fixture source missing")
	}
	member := model.ScopeMember{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: "22222222-2222-4222-8222-222222222222", SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: "main.go", ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: int64(file.Len())}}
	input := app.ValidateCapturedInput{Scope: scope, Sources: []app.ValidationSource{{Ref: ref}}, MaxInputBytes: 8192, MaxManifestBytes: 8192,
		CurrentScope: func(context.Context) (model.ScopeVector, error) { return scope, nil }, ResolveCapture: func(_ context.Context, pinned model.ScopeMember) (*snapshot.Snapshot, error) {
			if pinned.GenerationID != member.GenerationID {
				return nil, errors.New("generation unavailable")
			}
			return request.Snapshot, nil
		}, AuthorizeSource: func(ctx context.Context, _ model.ScopeMember, ref model.SourceRef) error {
			return authorizeSource(ctx, request.Inventory, ref.Path)
		},
		VerifyWorkspace: func(ctx context.Context, _ model.ScopeMember, captured *snapshot.Snapshot) (app.WorkspaceObservation, error) {
			bound := request
			bound.Snapshot = captured
			err := RevalidateLocal(ctx, bound)
			if errors.Is(err, ErrSnapshotChanged) {
				return app.WorkspaceChanged, nil
			}
			if err != nil {
				return app.WorkspaceUnknown, err
			}
			return app.WorkspaceObservedCurrent, nil
		}}
	result, err := app.ValidateCapturedWorkspace(ctx, input)
	if err != nil || !result.Data.EditPreconditionValidated || result.Freshness.Status != "observed_current" {
		t.Fatalf("unchanged workspace validation: %v", err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("package new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sourcePath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got, err := app.ValidateCapturedWorkspace(ctx, input); !errors.Is(err, app.ErrFreshnessChanged) || got.Data.EditPreconditionValidated {
		t.Fatalf("same-size and mtime edit accepted: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("package old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(filepath.Dir(sourcePath), "added.go")
	if err := os.WriteFile(added, []byte("package added\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := app.ValidateCapturedWorkspace(ctx, input); !errors.Is(err, app.ErrFreshnessChanged) || got.Data.EditPreconditionValidated {
		t.Fatalf("new untracked source accepted: %v", err)
	}
	if err := os.Remove(added); err != nil {
		t.Fatal(err)
	}
	checks := 0
	request.VerifyDependencies = func(context.Context, []string) error {
		checks++
		if checks == 2 {
			return os.WriteFile(sourcePath, []byte("package new\n"), 0600)
		}
		return nil
	}
	if got, err := app.ValidateCapturedWorkspace(ctx, input); !errors.Is(err, app.ErrFreshnessChanged) || got.Data.EditPreconditionValidated {
		t.Fatalf("change during validation accepted: %v", err)
	}
}
