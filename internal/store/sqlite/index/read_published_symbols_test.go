package index

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestReadPublishedFileSymbolsKeepsSourceAndFactsInOneSnapshot(t *testing.T) {
	store, oldStage := sealedProviderFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.PublishProvider(ctx, publishProviderFixture(oldStage)); err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	if err := store.ReleaseProviderStage(ctx, oldStage.Capture, oldStage.Stream, authorize); err != nil {
		t.Fatal(err)
	}
	const second = "55555555-5555-4555-8555-555555555555"
	newFile, err := snapshot.NewFile("main.go", []byte("package changed\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest := oldStage.Capture.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, length := newFile.ContentHash(), int64(newFile.Len())
	manifest.Files[0].ContentHash, manifest.Files[0].ByteLength = &hash, &length
	newSnapshot, err := snapshot.Freeze(manifest, []snapshot.File{newFile}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	newCapture := oldStage.Capture
	newCapture.GenerationID, newCapture.Snapshot = second, newSnapshot
	begin := BeginGenerationRequest{GenerationID: second, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: newSnapshot, Writer: newCapture.Writer, MaxManifestBytes: 8192, Authorize: authorize}
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	if err := store.StageFiles(ctx, StageFilesRequest{Capture: newCapture, Files: []FileClassification{{Path: "main.go", Kind: "go"}}}); err != nil {
		t.Fatal(err)
	}
	stream, err := store.BeginProviderStage(ctx, newCapture, authorize)
	if err != nil {
		t.Fatal(err)
	}
	batch := providerBatchFixture()
	batch.Symbols[0].Name, batch.Symbols[0].QualifiedName = "changed", "pkg.changed"
	newStage := StageProviderRequest{Capture: newCapture, Stream: stream, Batch: batch, MaxFrameBytes: 65536}
	if _, err := store.StageProviderBatch(ctx, newStage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(newStage)
	done.Sequence = 2
	if err := store.SealProviderStage(ctx, sealRequest(newStage, done)); err != nil {
		t.Fatal(err)
	}
	var dbPath string
	if err := store.writer.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&dbPath); err != nil {
		t.Fatal(err)
	}
	readers, err := sql.Open("sqlite", fixtureSQLiteURI(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	readers.SetMaxOpenConns(4)
	defer readers.Close()
	reader, err := Open(ctx, readers, store.writer, fixtureWorktree, store.lease)
	if err != nil {
		t.Fatal(err)
	}
	request := PublishedFileSymbolsRequest{File: readFixtureRequest(t, StageFilesRequest{Capture: oldStage.Capture, Files: []FileClassification{{Path: "main.go"}}}), MaxSymbols: 1, MaxFactsBytes: 65536}
	entered, proceed := make(chan struct{}), make(chan struct{})
	calls := 0
	request.File.AuthorizePath = func(ctx context.Context, _ string, _ string, _ string, _ int64) error {
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
		view PublishedFileSymbols
		err  error
	}
	result := make(chan response, 1)
	go func() { view, err := reader.ReadPublishedFileSymbols(ctx, request); result <- response{view, err} }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := store.PublishProvider(ctx, publishProviderFixture(newStage)); err != nil {
		close(proceed)
		t.Fatal(err)
	}
	close(proceed)
	old := <-result
	if old.err != nil || old.view.File.GenerationID != fixtureGeneration || string(old.view.File.Bytes) != "package main\n" || len(old.view.Symbols) != 1 || old.view.Symbols[0].Name != "main" {
		t.Fatalf("mixed old view: %+v %v", old.view, old.err)
	}
	request.File.AuthorizePath = func(context.Context, string, string, string, int64) error { return nil }
	current, err := reader.ReadPublishedFileSymbols(ctx, request)
	if err != nil || current.File.GenerationID != second || string(current.File.Bytes) != "package changed\n" || len(current.Symbols) != 1 || current.Symbols[0].Name != "changed" {
		t.Fatalf("mixed new view: %+v %v", current, err)
	}
	request.MaxFactsBytes = 128
	limited, err := reader.ReadPublishedFileSymbols(ctx, request)
	if !errors.Is(err, ErrCaptureLimit) || limited.File.GenerationID != "" || len(limited.Symbols) != 0 {
		t.Fatalf("bounded read leaked partial output: %+v %v", limited, err)
	}
	request.MaxFactsBytes = 65536
	calls = 0
	request.File.AuthorizePath = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return errFixtureLease
		}
		return nil
	}
	revoked, err := reader.ReadPublishedFileSymbols(ctx, request)
	if !errors.Is(err, errFixtureLease) || revoked.File.GenerationID != "" || len(revoked.Symbols) != 0 {
		t.Fatalf("revoked read leaked output: %+v %v", revoked, err)
	}
}
