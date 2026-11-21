package index

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckFactRangesRejectsOutOfBoundsAndBudget(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	manifest := stage.Capture.Snapshot.Manifest()
	if err := checkFactRanges(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 10); err != nil {
		t.Fatal(err)
	}
	if err := checkFactRanges(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 1); !errors.Is(err, ErrCaptureLimit) {
		t.Fatalf("fact budget ignored: %v", err)
	}
	if _, err := store.writer.Exec(`UPDATE symbols SET end_byte=999 WHERE generation_id=? AND symbol_id='symbol'`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if err := checkFactRanges(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 10); !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("invalid symbol range accepted: %v", err)
	}
}

func TestProviderPathsAreRecheckedAfterRangeValidation(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manifest := stage.Capture.Snapshot.Manifest()
	denied := errors.New("fixture path authorization changed")
	calls := 0
	allow := stage.Capture.Authorize
	stage.Capture.Authorize = func(ctx context.Context, worktree, path, digest string, epoch int64) error {
		calls++
		if calls == 2 {
			return denied
		}
		return allow(ctx, worktree, path, digest, epoch)
	}
	if _, err := store.StageProviderBatch(ctx, stage); !errors.Is(err, denied) {
		t.Fatalf("batch did not recheck its path before committing: %v", err)
	}
	calls = 2
	if _, err := store.StageProviderBatch(ctx, stage); err != nil || calls != 4 {
		t.Fatalf("expected first-use and final check of the shared path: calls=%d err=%v", calls, err)
	}
	calls = 0
	if err := checkFactRanges(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 10); !errors.Is(err, denied) {
		t.Fatalf("publication scan did not recheck its path: %v", err)
	}
}
