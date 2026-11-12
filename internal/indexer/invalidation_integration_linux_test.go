//go:build linux

package indexer

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// This fixture stages independent generations and inspects their active rows;
// its authority and provider callbacks do not establish production wiring.
func TestInvalidationRebuildPublicationDoesNotExposeOldEdges(t *testing.T) {
	const oldCaller = "package p\nfunc Caller() { Old() }\n"
	const newCaller = "package p\nfunc Caller() { New() }\n"
	const oldTarget = "package p\nfunc Old() {}\n"
	const newTarget = "package p\nfunc New() {}\n"
	const unchanged = "package p\nvar Unchanged = 1\n"
	before := invalidationSnapshot(t, map[string]string{"caller.go": oldCaller, "old.go": oldTarget, "keep.go": unchanged}, nil)
	after := invalidationSnapshot(t, map[string]string{"caller.go": newCaller, "new.go": newTarget, "keep.go": unchanged}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, request := invalidationRequest(t, before, after)
	plan, err := PlanInvalidation(ctx, request)
	if err != nil || plan.Action != "rebuild" || plan.IncrementalMode != "full" || len(plan.Changes) != 3 {
		t.Fatalf("missing rebuild plan: %+v %v", plan, err)
	}
	reused, err := ReuseCapturedContent(ctx, ContentReuseInput{Invalidation: request, MaxSourceBytes: 65536, MaxReuseFiles: 100})
	if err != nil || reused.SemanticMode != "full_rebuild" || len(reused.ReusedPaths) != 1 || reused.ReusedPaths[0] != "keep.go" {
		t.Fatalf("content reuse did not preserve full semantic rebuild: %v", err)
	}
	statePath := t.TempDir()
	if err := os.Chmod(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	worktree := before.Manifest().WorktreeID
	const job = "22222222-2222-4222-8222-222222222222"
	const oldGeneration = "33333333-3333-4333-8333-333333333333"
	const newGeneration = "44444444-4444-4444-8444-444444444444"
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(statePath, "index.sqlite"))}).String()
	openDB := func(limit int) *sql.DB {
		t.Helper()
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
	shared, err := runtime.AcquireLifecycle(ctx, root, worktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	owner, err := sqliteowner.OpenIndexOwner(ctx, shared, openDB(4), openDB(1), worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, shared, job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	authorizeGeneration := func(context.Context, string, string, string, int64) error { return nil }
	authorizePath := func(context.Context, string, string, string, int64) error { return nil }
	build := func(generation string, captured *snapshot.Snapshot, callerSource, targetPath, targetSource, targetName string) {
		t.Helper()
		manifest := captured.Manifest()
		begin := index.BeginGenerationRequest{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Authorize: authorizeGeneration, MaxManifestBytes: 65536}
		if err := store.BeginGeneration(ctx, begin); err != nil {
			t.Fatal(err)
		}
		capture := index.CaptureWrite{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Scanner: scanner, Authorize: authorizePath, MaxManifestBytes: 65536, MaxSourceBytes: 65536}
		files := []index.FileClassification{}
		for _, file := range manifest.Files {
			files = append(files, index.FileClassification{Path: file.Path, Kind: "go"})
		}
		if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: files}); err != nil {
			t.Fatal(err)
		}
		stream, err := store.BeginProviderStage(ctx, capture, authorizeGeneration)
		if err != nil {
			t.Fatal(err)
		}
		callerPath, pkg := "caller.go", "fixture-package"
		callerID, targetID := "caller-"+targetName, "target-"+targetName
		callerStart, targetStart := strings.Index(callerSource, "Caller"), strings.Index(targetSource, targetName)
		callStart := strings.Index(callerSource, targetName+"()")
		batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: 1,
			Packages: []model.PackageFact{{PackageKey: pkg, ImportPath: "example.test/p", ModuleKey: "fixture-module", Variant: "production", InputDigest: manifest.SnapshotDigest, SemanticStatus: "partial"}},
			Symbols: []model.SymbolFact{
				{SymbolID: callerID, Path: &callerPath, PackageKey: &pkg, Kind: "function", Name: "Caller", QualifiedName: "example.test/p.Caller", ScopeKey: "scope", Range: &model.ByteRange{StartByte: int64(callerStart), EndByte: int64(callerStart + len("Caller"))}},
				{SymbolID: targetID, Path: &targetPath, PackageKey: &pkg, Kind: "function", Name: targetName, QualifiedName: "example.test/p." + targetName, ScopeKey: "scope", Range: &model.ByteRange{StartByte: int64(targetStart), EndByte: int64(targetStart + len(targetName))}}},
			Relations: []model.RelationFact{{RelationID: "call-" + targetName, SourceSymbolID: callerID, TargetSymbolID: &targetID, Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: callerPath, Range: model.ByteRange{StartByte: int64(callStart), EndByte: int64(callStart + len(targetName))}}}, Diagnostics: []model.DiagnosticFact{}}
		stream, err = store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: 65536})
		if err != nil {
			t.Fatalf("stage graph: %v", err)
		}
		done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Sequence: 2, Status: "partial", InputDigest: manifest.SnapshotDigest,
			Capabilities: []model.Capability{{Name: "go_semantics", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "SYNTHETIC", Reason: "Fixture scope.", SourceRefs: []model.SourceRef{}}}}
		if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: capture, Stream: stream, Done: done, AuthorizeGeneration: authorizeGeneration, MaxFrameBytes: 65536}); err != nil {
			t.Fatal(err)
		}
		verify := func(_ context.Context, source *snapshot.Snapshot, done model.ProviderDone) error {
			if source.Manifest().SnapshotDigest != done.InputDigest {
				return index.ErrCaptureMismatch
			}
			return nil
		}
		if err := store.PublishProvider(ctx, index.PublishProviderRequest{Capture: capture, Stream: stream, AuthorizeGeneration: authorizeGeneration, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }, VerifyProvider: verify, MaxFacts: 100, MaxFrameBytes: 65536}); err != nil {
			t.Fatal(err)
		}
		if err := store.ReleaseProviderStage(ctx, capture, stream, authorizeGeneration); err != nil {
			t.Fatal(err)
		}
	}
	build(oldGeneration, before, oldCaller, "old.go", oldTarget, "Old")
	build(newGeneration, reused.Captured, newCaller, "new.go", newTarget, "New")
	read := index.PublishedFileRequest{ProfileID: after.Manifest().ProfileID, Path: "caller.go", AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: 65536, MaxFileBytes: 65536}
	current, err := store.ReadPublishedFileSymbols(ctx, index.PublishedFileSymbolsRequest{File: read, MaxSymbols: 100, MaxFactsBytes: 65536})
	if err != nil || current.File.GenerationID != newGeneration || string(current.File.Bytes) != newCaller || len(current.Symbols) != 1 || current.Symbols[0].SymbolID != "caller-New" {
		t.Fatalf("active generation retained old source or symbols: %v", err)
	}
	read.Path = "old.go"
	if _, err := store.ReadPublishedFile(ctx, read); !errors.Is(err, index.ErrPublishedFileUnavailable) {
		t.Fatalf("removed file remained in active generation: %v", err)
	}
	read.GenerationID = oldGeneration
	prior, err := store.ReadPublishedFile(ctx, read)
	if err != nil || string(prior.Bytes) != oldTarget {
		t.Fatalf("historical pinned generation was lost: %v", err)
	}
	db := openDB(1)
	defer db.Close()
	var oldEdges, newEdges int
	if err := db.QueryRowContext(ctx, `SELECT sum(CASE WHEN r.relation_id='call-Old' THEN 1 ELSE 0 END),sum(CASE WHEN r.relation_id='call-New' THEN 1 ELSE 0 END) FROM relations r JOIN active_generations a ON a.generation_id=r.generation_id`).Scan(&oldEdges, &newEdges); err != nil || oldEdges != 0 || newEdges != 1 {
		t.Fatalf("active graph contains stale or missing edges: %d %d %v", oldEdges, newEdges, err)
	}
	kept, ok := after.File("keep.go")
	if !ok {
		t.Fatal("unchanged file missing")
	}
	var blobs, references int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM content_blobs WHERE content_hash=?),(SELECT count(*) FROM files WHERE content_hash=? AND path='keep.go')`, kept.ContentHash(), kept.ContentHash()).Scan(&blobs, &references); err != nil || blobs != 1 || references != 2 {
		t.Fatalf("unchanged captured blob was not shared across generations: %d %d %v", blobs, references, err)
	}
}
