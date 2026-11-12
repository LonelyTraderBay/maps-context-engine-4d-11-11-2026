package index

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCancelBuildingSerializesWithProviderAndPreservesActive(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	old := insertPreviousActive(t, store.writer, StageFilesRequest{Capture: stage.Capture})
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	request := CancelBuildingRequest{Capture: stage.Capture, Authorize: authorize}
	if err := store.CancelBuildingGeneration(ctx, request); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("cancel ignored active stream: %v", err)
	}
	request.Stream = stage.Stream
	if err := store.CancelBuildingGeneration(ctx, request); err != nil {
		t.Fatal(err)
	}
	var active, state string
	if err := store.writer.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := store.writer.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if active != old || state != "CANCELLED" {
		t.Fatalf("cancel changed active=%s state=%s", active, state)
	}
	if _, err := store.StageProviderBatch(ctx, stage); err == nil {
		t.Fatal("cancelled generation accepted a frame")
	}
	if err := store.ReleaseProviderStage(ctx, stage.Capture, stage.Stream, authorize); err != nil {
		t.Fatal(err)
	}
	if err := store.CancelBuildingGeneration(ctx, CancelBuildingRequest{Capture: stage.Capture, Authorize: authorize}); err == nil {
		t.Fatal("cancel replay accepted")
	}
}

func TestCancelBuildingRollsBackOnRevocationAndHonorsDeadline(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	request := CancelBuildingRequest{Capture: stage.Capture, Stream: stage.Stream, Authorize: authorize}
	<-stage.Stream.shared.gate
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if err := store.CancelBuildingGeneration(short, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait ignored deadline: %v", err)
	}
	stage.Stream.shared.gate <- struct{}{}
	calls := 0
	request.Authorize = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return errFixtureLease
		}
		return nil
	}
	if err := store.CancelBuildingGeneration(ctx, request); !errors.Is(err, errFixtureLease) {
		t.Fatalf("revoked cancel accepted: %v", err)
	}
	var state string
	if err := store.writer.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil || state != "BUILDING" {
		t.Fatalf("rejected cancel changed state %s: %v", state, err)
	}
}
