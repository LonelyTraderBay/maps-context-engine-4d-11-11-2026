package index

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func providerDoneFixture(request StageProviderRequest) model.ProviderDone {
	manifest := request.Capture.Snapshot.Manifest()
	return model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: fixtureJob, Sequence: 1, Status: "partial", InputDigest: manifest.SnapshotDigest,
		Capabilities: []model.Capability{{Name: "go_semantics", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}},
		Gaps:         []model.Gap{{Code: "PARSE_INCOMPLETE", Reason: "Fixture could not complete analysis.", SourceRefs: []model.SourceRef{}}}}
}

func sealRequest(stage StageProviderRequest, done model.ProviderDone) SealProviderRequest {
	return SealProviderRequest{Capture: stage.Capture, Stream: stage.Stream, Done: done, MaxFrameBytes: 65536, AuthorizeGeneration: func(context.Context, string, string, string, int64) error { return nil }}
}

func TestSealProviderStageRequiresOrderedDoneAndMatchingSource(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	manifest := stage.Capture.Snapshot.Manifest()
	file, _ := stage.Capture.Snapshot.File("main.go")
	done.Gaps[0].SourceRefs = []model.SourceRef{{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: stage.Capture.GenerationID, Path: "main.go", ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: 7}}}
	request := sealRequest(stage, done)
	request.Done.InputDigest = manifest.ProfileID
	if err := store.SealProviderStage(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("wrong input digest accepted: %v", err)
	}
	request.Done = done
	request.Done.Gaps[0].SourceRefs[0].GenerationID = "99999999-9999-4999-8999-999999999999"
	if err := store.SealProviderStage(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("wrong source generation accepted: %v", err)
	}
	request.Done = done
	request.Done.Gaps[0].SourceRefs[0].GenerationID = stage.Capture.GenerationID
	if err := store.SealProviderStage(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := store.SealProviderStage(ctx, request); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("terminal frame repeated: %v", err)
	}
	stage.Batch.Sequence = 2
	if _, err := store.StageProviderBatch(ctx, stage); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("batch accepted after terminal frame: %v", err)
	}
}

func TestSealProviderStageRejectsContradictoryCoverageAndRevocation(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := providerDoneFixture(stage)
	done.Status = "complete_in_supported_scope"
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("complete status hid gaps: %v", err)
	}
	done = providerDoneFixture(stage)
	done.Gaps = []model.Gap{}
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("partial status without a gap accepted: %v", err)
	}
	done = providerDoneFixture(stage)
	manifest := stage.Capture.Snapshot.Manifest()
	file, _ := stage.Capture.Snapshot.File("main.go")
	done.Gaps[0].SourceRefs = []model.SourceRef{{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: stage.Capture.GenerationID, Path: "main.go", ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: 7}}}
	calls := 0
	stage.Capture.Authorize = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return errFixtureLease
		}
		return nil
	}
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); !errors.Is(err, errFixtureLease) {
		t.Fatalf("revoked evidence accepted: %v", err)
	}
	stage.Capture.Authorize = func(context.Context, string, string, string, int64) error { return nil }
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatalf("revoked seal advanced stream: %v", err)
	}
}
