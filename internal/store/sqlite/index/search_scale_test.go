package index

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestSearchSymbolIndexOnScaledFixture(t *testing.T) {
	store, stage := providerStageFixture(t)
	stage.MaxFrameBytes = 4 * 1024 * 1024
	for i := 0; i < 1200; i++ {
		symbol := stage.Batch.Symbols[0]
		symbol.SymbolID, symbol.Name = fmt.Sprintf("scale%04d", i), fmt.Sprintf("Needle%04d", i)
		symbol.QualifiedName = "pkg." + symbol.Name
		stage.Batch.Symbols = append(stage.Batch.Symbols, symbol)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatal(err)
	}
	publication := publishProviderFixture(stage)
	publication.MaxFacts = 1500
	if err := store.PublishProvider(ctx, publication); err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	request := search.Request{ProfileID: stage.Capture.Snapshot.Manifest().ProfileID, Mode: "prefix", Query: "Needle", Limit: 10, MaxOutputBytes: 65536, MaxManifestBytes: 8192, MaxFileBytes: 4096, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: stage.Capture.Scanner}
	result, err := store.Search(ctx, request)
	if err != nil || len(result.Symbols) != 10 || !result.Truncated || !result.Ambiguous || result.Symbols[0].Symbol.Name != "Needle0000" {
		t.Fatalf("scaled symbols: %+v %v", result, err)
	}
	query, args := symbolSearchQuery(request, fixtureGeneration)
	if plan := explainPlan(t, ctx, store.writer, query, args...); !strings.Contains(plan, "symbols_name") {
		t.Fatalf("scaled symbol plan: %s", plan)
	}
}

func TestSearchFTSIndexOnScaledFixture(t *testing.T) {
	store, _, begin := beginFixture(t)
	manifest := begin.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	manifest.Files = nil
	var files []snapshot.File
	var classifications []FileClassification
	var paths []string
	for i := 0; i < 300; i++ {
		filename := fmt.Sprintf("src/f%04d.ts", i)
		file, err := snapshot.NewFile(filename, []byte(fmt.Sprintf("const commonterm = %d;\n", i)), 1024)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: filename, Status: "captured", ContentHash: &hash, ByteLength: &length})
		files = append(files, file)
		classifications = append(classifications, FileClassification{Path: filename, Kind: "source"})
		paths = append(paths, filename)
	}
	var err error
	begin.MaxManifestBytes = 128 * 1024
	begin.Snapshot, err = snapshot.Freeze(manifest, files, begin.MaxManifestBytes, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	capture := CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, PolicyEpoch: 1, Snapshot: begin.Snapshot, Writer: begin.Writer, Scanner: scanner, MaxManifestBytes: begin.MaxManifestBytes, MaxSourceBytes: 64 * 1024, Authorize: authorize}
	for start := 0; start < len(paths); start += 150 {
		end := start + 150
		if err := store.StageFiles(ctx, StageFilesRequest{Capture: capture, Offset: start, Files: classifications[start:end]}); err != nil {
			t.Fatal(err)
		}
		if err := store.StageSearchDocuments(ctx, StageSearchDocumentsRequest{Capture: capture, AuthorizeGeneration: authorize, Paths: paths[start:end], MaxBatchBytes: 64 * 1024}); err != nil {
			t.Fatal(err)
		}
	}
	stage := StageFilesRequest{Capture: capture}
	if err := store.PublishCapturedOnly(ctx, publishRequest(stage)); err != nil {
		t.Fatal(err)
	}
	request := search.Request{ProfileID: capture.Snapshot.Manifest().ProfileID, Mode: "text", Query: "commonterm", Limit: 10, MaxOutputBytes: 65536, MaxManifestBytes: begin.MaxManifestBytes, MaxFileBytes: 4096, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: scanner}
	result, err := store.Search(ctx, request)
	if err != nil || len(result.Text) != 10 || !result.Truncated || result.Text[0].Path != "src/f0000.ts" {
		t.Fatalf("scaled FTS: %+v %v", result, err)
	}
	if plan := explainPlan(t, ctx, store.writer, textSearchSQL, `body : "commonterm"`, fixtureGeneration, 11); !strings.Contains(strings.ToUpper(plan), "VIRTUAL TABLE INDEX") {
		t.Fatalf("scaled FTS plan: %s", plan)
	}
}
