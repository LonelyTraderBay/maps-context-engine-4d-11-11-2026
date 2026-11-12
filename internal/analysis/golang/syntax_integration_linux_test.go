//go:build linux

package golang

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func TestSyntaxResultsStageAndPublishAgainstCapturedInput(t *testing.T) {
	for _, test := range []struct {
		name, source string
		parseError   bool
	}{
		{"valid", "package sample\ntype Entry struct { Value string `json:\"key\"` }\nfunc Run(p int) int { x := p; return x }\n", false},
		{"parse_error", "package sample\nfunc Run(\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := syntaxFixture(t, "pkg/main.go", test.source)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := AnalyzeSyntaxFile(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "state")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			root, err := paths.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			worktree := input.Snapshot.Manifest().WorktreeID
			const job = "22222222-2222-4222-8222-222222222222"
			const generation = "33333333-3333-4333-8333-333333333333"
			dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(dir, "index.sqlite"))}).String()
			openDB := func(limit int) *sql.DB {
				db, err := sql.Open("sqlite", dsn)
				if err != nil {
					t.Fatal(err)
				}
				db.SetMaxOpenConns(limit)
				return db
			}
			exclusive, err := runtime.AcquireLifecycle(ctx, root, worktree, ports.ExclusiveLease, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			maintenanceWriter, err := runtime.AcquireWriter(ctx, exclusive, job, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			maintenance, err := sqliteowner.OpenMaintenanceOwner(ctx, exclusive, openDB(1), worktree)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := maintenance.InitializeIndex(ctx, maintenanceWriter, job); err != nil {
				t.Fatal(err)
			}
			if err := maintenance.Close(); err != nil {
				t.Fatal(err)
			}
			if err := maintenanceWriter.Close(); err != nil {
				t.Fatal(err)
			}
			if err := exclusive.Close(); err != nil {
				t.Fatal(err)
			}
			lease, err := runtime.AcquireLifecycle(ctx, root, worktree, ports.SharedLease, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			owner, err := sqliteowner.OpenIndexOwner(ctx, lease, openDB(4), openDB(1), worktree)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			store, err := owner.Store()
			if err != nil {
				t.Fatal(err)
			}
			writer, err := runtime.AcquireWriter(ctx, lease, job, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			authorize := func(context.Context, string, string, string, int64) error { return nil }
			if err := store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: input.Snapshot, Writer: writer, Authorize: authorize, MaxManifestBytes: 8192}); err != nil {
				t.Fatal(err)
			}
			capture := index.CaptureWrite{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: input.Snapshot, Writer: writer, Scanner: input.Scanner, MaxManifestBytes: 8192, MaxSourceBytes: 65536, Authorize: authorize}
			if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: []index.FileClassification{{Path: input.Path, Kind: "go"}}}); err != nil {
				t.Fatal(err)
			}
			stream, err := store.BeginProviderStage(ctx, capture, authorize)
			if err != nil {
				t.Fatal(err)
			}
			batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: 1, Packages: []model.PackageFact{result.Package}, Symbols: result.Symbols, Relations: []model.RelationFact{}, Diagnostics: result.Diagnostics}
			if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: 65536}); err != nil {
				t.Fatal(err)
			}
			if test.parseError {
				if len(result.Diagnostics) != 1 {
					t.Fatal("parse error missing diagnostic")
				}
				if err := store.MarkParseErrors(ctx, index.MarkParseErrorsRequest{Capture: capture, Failures: []index.ParseFailure{{Path: input.Path, DiagnosticID: result.Diagnostics[0].DiagnosticID}}}); err != nil {
					t.Fatal(err)
				}
			}
			done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Sequence: 2, Status: "partial", InputDigest: result.SnapshotDigest, Capabilities: []model.Capability{{Name: "go_syntax", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "BUILD_SELECTION_UNVERIFIED", Reason: "Package selection and typed closure are not verified.", SourceRefs: []model.SourceRef{}}}}
			if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: capture, Stream: stream, Done: done, MaxFrameBytes: 65536, AuthorizeGeneration: authorize}); err != nil {
				t.Fatal(err)
			}
			request := index.PublishProviderRequest{Capture: capture, Stream: stream, AuthorizeGeneration: authorize, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }, VerifyProvider: func(_ context.Context, captured *snapshot.Snapshot, done model.ProviderDone) error {
				file, ok := captured.File(input.Path)
				if !ok || file.ContentHash() != result.ContentHash || captured.Manifest().SnapshotDigest != result.SnapshotDigest || done.InputDigest != result.SnapshotDigest {
					return index.ErrCaptureMismatch
				}
				return nil
			}, MaxFacts: 128, MaxFrameBytes: 65536}
			if err := store.PublishProvider(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := store.ReleaseProviderStage(ctx, capture, stream, authorize); err != nil {
				t.Fatal(err)
			}
			view, err := store.ReadPublishedFileSymbols(ctx, index.PublishedFileSymbolsRequest{File: index.PublishedFileRequest{ProfileID: input.Snapshot.Manifest().ProfileID, Path: input.Path, Scanner: input.Scanner, MaxManifestBytes: 8192, MaxFileBytes: 65536, AuthorizeGeneration: authorize, AuthorizePath: authorize}, MaxSymbols: 32, MaxFactsBytes: 65536})
			if err != nil || view.File.GenerationID != generation || view.File.State != "READY_PARTIAL" || string(view.File.Bytes) != test.source {
				t.Fatalf("published source mismatch: %+v %v", view, err)
			}
			if test.parseError && len(view.Symbols) != 0 || !test.parseError && len(view.Symbols) == 0 {
				t.Fatalf("wrong syntax result: %+v", view.Symbols)
			}
			if !test.parseError {
				found := false
				for _, symbol := range view.Symbols {
					if symbol.Kind == "field" && symbol.Name == "Value" && symbol.Signature == "string `json:\"key\"`" {
						found = true
					}
				}
				if !found {
					t.Fatal("stored field lost its source-backed type or tag")
				}
			}
		})
	}
}
