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
