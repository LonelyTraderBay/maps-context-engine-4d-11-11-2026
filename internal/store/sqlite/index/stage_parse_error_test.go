package index

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestMarkParseErrorsRequiresDiagnosticAndPreservesCapture(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	path := "main.go"
	stage.Batch.Diagnostics = []model.DiagnosticFact{{DiagnosticID: "parse", Path: &path, Code: "PARSE_ERROR", Severity: "error", Message: "Parse failed.", ScopeDescription: "workspace"}}
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	digest := stage.Capture.Snapshot.Manifest().SnapshotDigest
	request := MarkParseErrorsRequest{Capture: stage.Capture, Failures: []ParseFailure{{Path: path, DiagnosticID: "missing"}}}
	if err := store.MarkParseErrors(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("missing diagnostic accepted: %v", err)
	}
	var status string
	if err := store.writer.QueryRow(`SELECT status FROM files WHERE generation_id=? AND path=?`, fixtureGeneration, path).Scan(&status); err != nil || status != "indexed" {
		t.Fatalf("failed mutation changed status: %q %v", status, err)
	}
	request.Failures[0].DiagnosticID = "parse"
	if err := store.MarkParseErrors(ctx, request); err != nil {
		t.Fatal(err)
	}
	var reason, storedDigest, contentHash string
	if err := store.writer.QueryRow(`SELECT f.status,f.reason_code,f.content_hash,g.snapshot_digest FROM files f JOIN generations g ON g.generation_id=f.generation_id WHERE f.generation_id=? AND f.path=?`, fixtureGeneration, path).Scan(&status, &reason, &contentHash, &storedDigest); err != nil {
		t.Fatal(err)
	}
	file, _ := stage.Capture.Snapshot.File(path)
	if status != "parse_error" || reason != "PARSE_ERROR" || contentHash != file.ContentHash() || storedDigest != digest || blobCount(t, store.writer) != 1 {
		t.Fatal("parse projection changed captured input")
	}
	if err := store.MarkParseErrors(ctx, request); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("replay accepted: %v", err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishProvider(ctx, publishProviderFixture(stage)); err != nil {
		t.Fatalf("documented partial capture failed to publish: %v", err)
	}
}

func TestMarkParseErrorsRollsBackWhenPermissionIsRevoked(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	path := "main.go"
	stage.Batch.Diagnostics = []model.DiagnosticFact{{DiagnosticID: "parse", Path: &path, Code: "PARSE_ERROR", Severity: "error", Message: "Parse failed.", ScopeDescription: "workspace"}}
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	calls := 0
	stage.Capture.Authorize = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return errFixtureLease
		}
		return nil
	}
	request := MarkParseErrorsRequest{Capture: stage.Capture, Failures: []ParseFailure{{Path: path, DiagnosticID: "parse"}}}
	if err := store.MarkParseErrors(ctx, request); !errors.Is(err, errFixtureLease) {
		t.Fatalf("revoked mutation accepted: %v", err)
	}
	var status string
	if err := store.writer.QueryRow(`SELECT status FROM files WHERE generation_id=? AND path=?`, fixtureGeneration, path).Scan(&status); err != nil || status != "indexed" {
		t.Fatalf("revoked mutation left status %q: %v", status, err)
	}
}
