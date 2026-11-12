package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func publishFixture(t *testing.T, db *sql.DB, generation string, manifest model.CaptureManifest, active bool) {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE generations SET state='READY',manifest_json=?,completed_at='2026-01-01T00:00:00Z' WHERE generation_id=?`, string(raw), generation); err != nil {
		t.Fatal(err)
	}
	if active {
		if _, err := db.Exec(`INSERT INTO active_generations(profile_id,generation_id) VALUES(?,?) ON CONFLICT(profile_id) DO UPDATE SET generation_id=excluded.generation_id`, manifest.ProfileID, generation); err != nil {
			t.Fatal(err)
		}
	}
}

func readFixtureRequest(t *testing.T, stage StageFilesRequest) PublishedFileRequest {
	t.Helper()
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	return PublishedFileRequest{ProfileID: stage.Capture.Snapshot.Manifest().ProfileID, Path: stage.Files[0].Path, Scanner: scanner, MaxManifestBytes: 8192, MaxFileBytes: 1024,
		AuthorizeGeneration: func(context.Context, string, string, string, int64) error { return nil },
		AuthorizePath:       func(context.Context, string, string, string, int64) error { return nil }}
}

func TestReadPublishedFileNeverReturnsBuildingGeneration(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	request := readFixtureRequest(t, stage)
	if file, err := store.ReadPublishedFile(ctx, request); !errors.Is(err, ErrSnapshotUnavailable) || file.Bytes != nil {
		t.Fatalf("BUILDING active accepted: %+v %v", file, err)
	}
	request.GenerationID = fixtureGeneration
	if file, err := store.ReadPublishedFile(ctx, request); !errors.Is(err, ErrSnapshotUnavailable) || file.Bytes != nil {
		t.Fatalf("BUILDING explicit accepted: %+v %v", file, err)
	}
	publishFixture(t, db, fixtureGeneration, stage.Capture.Snapshot.Manifest(), true)
	request.GenerationID = ""
	file, err := store.ReadPublishedFile(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if file.GenerationID != fixtureGeneration || string(file.Bytes) != "package main\n" || file.State != "READY" {
		t.Fatalf("published file: %+v", file)
	}
	request.GenerationID = fixtureGeneration
	if file, err := store.ReadPublishedFile(ctx, request); err != nil || string(file.Bytes) != "package main\n" {
		t.Fatalf("explicit published read: %+v %v", file, err)
	}
}

func TestReadPublishedFileRejectsRevocationAndMismatchedManifest(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	manifest := stage.Capture.Snapshot.Manifest()
	publishFixture(t, db, fixtureGeneration, manifest, true)
	request := readFixtureRequest(t, stage)
	calls := 0
	request.AuthorizePath = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return errFixtureLease
		}
		return nil
	}
	if file, err := store.ReadPublishedFile(ctx, request); !errors.Is(err, errFixtureLease) || file.Bytes != nil {
		t.Fatalf("revoked read returned bytes: %+v %v", file, err)
	}
	request.AuthorizePath = func(context.Context, string, string, string, int64) error { return nil }
	request.MaxFileBytes = 1
	if file, err := store.ReadPublishedFile(ctx, request); !errors.Is(err, ErrCaptureLimit) || file.Bytes != nil {
		t.Fatalf("file budget ignored: %+v %v", file, err)
	}
}

func TestReadPublishedFileRejectsManifestDigestMismatch(t *testing.T) {
	store, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE generations SET snapshot_digest=? WHERE generation_id=?`, strings.Repeat("f", 64), fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	publishFixture(t, db, fixtureGeneration, stage.Capture.Snapshot.Manifest(), true)
	if result, err := store.ReadPublishedFile(ctx, readFixtureRequest(t, stage)); !errors.Is(err, ErrCaptureMismatch) || result.Bytes != nil {
		t.Fatalf("mismatched manifest returned bytes: %+v %v", result, err)
	}
}

func TestReadPublishedFileHandlesUTF8ManifestByteLength(t *testing.T) {
	store, db, begin := beginFixture(t)
	file, err := snapshot.NewFile("tiếng-việt.go", []byte("package main\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest := begin.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, length := file.ContentHash(), int64(file.Len())
	manifest.Files = []model.ManifestFile{{Path: file.Path(), Status: "captured", ContentHash: &hash, ByteLength: &length}}
	begin.Snapshot, err = snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	stage := StageFilesRequest{Capture: CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, PolicyEpoch: 1, Snapshot: begin.Snapshot, Writer: begin.Writer, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 1024, Authorize: func(context.Context, string, string, string, int64) error { return nil }}, Files: []FileClassification{{Path: file.Path(), Kind: "go"}}}
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	publishFixture(t, db, begin.GenerationID, begin.Snapshot.Manifest(), true)
	request := readFixtureRequest(t, stage)
	if result, err := store.ReadPublishedFile(ctx, request); err != nil || result.Path != file.Path() {
		t.Fatalf("UTF-8 manifest failed: %+v %v", result, err)
	}
}

func TestReadPublishedFileKeepsOneSQLiteSnapshotDuringPublish(t *testing.T) {
	store, db, oldStage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, oldStage); err != nil {
		t.Fatal(err)
	}
	publishFixture(t, db, fixtureGeneration, oldStage.Capture.Snapshot.Manifest(), true)
	const second = "55555555-5555-4555-8555-555555555555"
	newFile, err := snapshot.NewFile("main.go", []byte("package changed\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest := oldStage.Capture.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, length := newFile.ContentHash(), int64(newFile.Len())
	manifest.Files[0].ContentHash = &hash
	manifest.Files[0].ByteLength = &length
	newSnapshot, err := snapshot.Freeze(manifest, []snapshot.File{newFile}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	begin := BeginGenerationRequest{GenerationID: second, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: newSnapshot, Writer: &writerProbe{}, MaxManifestBytes: 8192, Authorize: func(context.Context, string, string, string, int64) error { return nil }}
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	newStage := oldStage
	newStage.Capture.GenerationID = second
	newStage.Capture.Snapshot = newSnapshot
	newStage.Capture.Writer = &writerProbe{}
	if err := store.StageFiles(ctx, newStage); err != nil {
		t.Fatal(err)
	}
	var dbPath string
	if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&dbPath); err != nil {
		t.Fatal(err)
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(dbPath)}).String()
	readers, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	readers.SetMaxOpenConns(4)
	defer readers.Close()
	reader, err := Open(ctx, readers, db, fixtureWorktree, store.lease)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	proceed := make(chan struct{})
	request := readFixtureRequest(t, oldStage)
	calls := 0
	request.AuthorizePath = func(ctx context.Context, _ string, _ string, _ string, _ int64) error {
		calls++
		if calls == 1 {
			close(entered)
			select {
			case <-proceed:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	type response struct {
		file PublishedFile
		err  error
	}
	done := make(chan response, 1)
	go func() { file, err := reader.ReadPublishedFile(ctx, request); done <- response{file, err} }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := store.PublishCapturedOnly(ctx, publishRequest(newStage)); err != nil {
		close(proceed)
		t.Fatal(err)
	}
	close(proceed)
	result := <-done
	if result.err != nil || result.file.GenerationID != fixtureGeneration || string(result.file.Bytes) != "package main\n" {
		t.Fatalf("mixed snapshot: %+v %v", result.file, result.err)
	}
	request.AuthorizePath = func(context.Context, string, string, string, int64) error { return nil }
	result.file, result.err = reader.ReadPublishedFile(ctx, request)
	if result.err != nil || result.file.GenerationID != second || string(result.file.Bytes) != "package changed\n" {
		t.Fatalf("new active snapshot not visible: %+v %v", result.file, result.err)
	}
}
