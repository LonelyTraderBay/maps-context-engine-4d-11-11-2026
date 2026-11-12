package index

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPublicationOutcomeByJobDistinguishesPublishedAndUnavailable(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := PublicationOutcomeRequest{GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: stage.Capture.Snapshot, MaxManifestBytes: 8192, Authorize: func(context.Context, string, string, string, int64) error { return nil }}
	result, err := store.PublicationOutcomeByJob(ctx, request)
	if err != nil || result.Status != "building" {
		t.Fatalf("building outcome: %+v %v", result, err)
	}
	request.GenerationID = "77777777-7777-4777-8777-777777777777"
	result, err = store.PublicationOutcomeByJob(ctx, request)
	if err != nil || result.Status != "unavailable" {
		t.Fatalf("missing outcome: %+v %v", result, err)
	}
	request.GenerationID = fixtureGeneration
	request.JobID = "88888888-8888-4888-8888-888888888888"
	if _, err := store.PublicationOutcomeByJob(ctx, request); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("wrong job accepted: %v", err)
	}
	request.JobID = fixtureJob
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	old := insertPreviousActive(t, db, stage)
	if err := store.PublishCapturedOnly(ctx, publishRequest(stage)); err != nil {
		t.Fatal(err)
	}
	result, err = store.PublicationOutcomeByJob(ctx, request)
	if err != nil || result.Status != "published_active" {
		t.Fatalf("active outcome: %+v %v", result, err)
	}
	request.GenerationID = old
	result, err = store.PublicationOutcomeByJob(ctx, request)
	if err != nil || result.Status != "published_historical" {
		t.Fatalf("historical outcome: %+v %v", result, err)
	}
	if _, err := db.Exec(`UPDATE generations SET state='RETIRED' WHERE generation_id=?`, old); err != nil {
		t.Fatal(err)
	}
	result, err = store.PublicationOutcomeByJob(ctx, request)
	if err != nil || result.Status != "retired" {
		t.Fatalf("retired outcome: %+v %v", result, err)
	}
}

func TestPublicationOutcomeByJobHidesResultAfterRevocation(t *testing.T) {
	store, _, stage := stageFixture(t)
	checks := 0
	request := PublicationOutcomeRequest{GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: stage.Capture.Snapshot, MaxManifestBytes: 8192, Authorize: func(context.Context, string, string, string, int64) error {
		checks++
		if checks == 2 {
			return errFixtureLease
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := store.PublicationOutcomeByJob(ctx, request)
	if !errors.Is(err, errFixtureLease) || result.Status != "" {
		t.Fatalf("revoked outcome disclosed: %+v %v", result, err)
	}
}
