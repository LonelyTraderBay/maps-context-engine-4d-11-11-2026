package index

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"modernc.org/sqlite"
)

// Limit this disposable database's pages, never the host filesystem. Exercise
// the real driver and staging transaction, including an earlier small insert.
func TestSQLiteFullRollsBackBatchAndPreservesPublishedSource(t *testing.T) {
	store, db, old := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishCapturedOnly(ctx, publishRequest(old)); err != nil {
		t.Fatal(err)
	}
	manifest := old.Capture.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	manifest.Files = nil
	var files []snapshot.File
	for i, content := range []string{"package next\n", strings.Repeat("// bounded synthetic filler\n", 20000)} {
		path := fmt.Sprintf("%c.go", 'a'+i)
		file, err := snapshot.NewFile(path, []byte(content), 1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &length})
		files = append(files, file)
	}
	captured, err := snapshot.Freeze(manifest, files, 8192, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	const next = "66666666-6666-4666-8666-666666666666"
	if err := store.BeginGeneration(ctx, BeginGenerationRequest{GenerationID: next, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: captured, Writer: &writerProbe{}, MaxManifestBytes: 8192, Authorize: func(context.Context, string, string, string, int64) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	stage := old
	stage.Capture.GenerationID, stage.Capture.Snapshot = next, captured
	stage.Capture.Scanner, stage.Capture.MaxSourceBytes = scanner, 1024*1024
	stage.Files = []FileClassification{{Path: "a.go", Kind: "go"}, {Path: "b.go", Kind: "go"}}
	var pages, limit int
	if err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", pages)).Scan(&limit); err != nil || limit != pages {
		t.Fatalf("page limit: %d != %d: %v", limit, pages, err)
	}
	err = store.StageFiles(ctx, stage)
	var full *sqlite.Error
	if !errors.As(err, &full) || full.Code()&0xff != 13 {
		t.Fatalf("expected actual SQLITE_FULL (13), got %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM files WHERE generation_id=?", next).Scan(&count); err != nil || count != 0 || blobCount(t, db) != 1 {
		t.Fatalf("failed batch retained rows/blobs: count=%d error=%v", count, err)
	}
	got, err := store.ReadPublishedFile(ctx, readFixtureRequest(t, old))
	if err != nil || got.GenerationID != fixtureGeneration || string(got.Bytes) != "package main\n" {
		t.Fatalf("old published source changed: %+v %v", got, err)
	}
	// Increasing only the fixture's limit permits the identical batch to retry.
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", pages+512)); err != nil {
		t.Fatal(err)
	}
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatalf("retry after capacity restoration: %v", err)
	}
}
