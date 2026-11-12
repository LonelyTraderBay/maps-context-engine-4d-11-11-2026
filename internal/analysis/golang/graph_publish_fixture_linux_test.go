//go:build linux

package golang

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// Uses real SQLite staging, locks and publication on synthetic bytes. The
// authority/revalidation callbacks are fixture proofs, not production wiring.
func runGraphPublicationFixture(t *testing.T, ctx context.Context, typed TypedInput, analyzed CoverageResult, sourcePath, source string) {
	t.Helper()
	statePath := t.TempDir()
	if err := os.Chmod(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	manifest := typed.Loader.Preflight.Snapshot.Manifest()
	worktree := manifest.WorktreeID
	const job = "22222222-2222-4222-8222-222222222222"
	const generation = "33333333-3333-4333-8333-333333333333"
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
	exclusive, err := runtimeadapter.AcquireLifecycle(ctx, state, worktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	maintenanceWriter, err := runtimeadapter.AcquireWriter(ctx, exclusive, job, time.Second)
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
	shared, err := runtimeadapter.AcquireLifecycle(ctx, state, worktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := sqliteowner.OpenIndexOwner(ctx, shared, openDB(4), openDB(1), worktree)
	if err != nil {
		t.Fatal(err)
	}
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtimeadapter.AcquireWriter(ctx, shared, job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = owner.Close(); _ = shared.Close() })
	authorizeGeneration := func(context.Context, string, string, string, int64) error { return nil }
	authorizePath := func(context.Context, string, string, string, int64) error { return nil }
	begin := index.BeginGenerationRequest{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: typed.Loader.Preflight.Snapshot, Writer: writer, Authorize: authorizeGeneration, MaxManifestBytes: 32768}
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	capture := index.CaptureWrite{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: typed.Loader.Preflight.Snapshot, Writer: writer, Scanner: typed.Loader.Preflight.Scanner, Authorize: authorizePath, MaxManifestBytes: 32768, MaxSourceBytes: 65536}
	classifications := make([]index.FileClassification, 0, len(manifest.Files))
	generated := make(map[string]bool, len(analyzed.GeneratedPaths))
	for _, path := range analyzed.GeneratedPaths {
		generated[path] = true
	}
	for _, item := range manifest.Files {
		classifications = append(classifications, index.FileClassification{Path: item.Path, Kind: "go_fixture", Generated: generated[item.Path]})
	}
	if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: classifications}); err != nil {
		t.Fatal(err)
	}
	stream, err := store.BeginProviderStage(ctx, capture, authorizeGeneration)
	if err != nil {
		t.Fatal(err)
	}
	batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: 1, Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{}, Relations: append(append([]model.RelationFact{}, analyzed.Graph.Relations...), analyzed.TestReferences...), Diagnostics: analyzed.Diagnostics}
	allSyntax := append(append([]SyntaxResult{}, analyzed.Graph.Typed.Syntax...), analyzed.FallbackSyntax...)
	packages := map[string]bool{}
	fileSymbolCount := 0
	for _, file := range allSyntax {
		if !packages[file.Package.PackageKey] {
			batch.Packages = append(batch.Packages, file.Package)
			packages[file.Package.PackageKey] = true
		}
		batch.Symbols = append(batch.Symbols, file.Symbols...)
		if file.Path == sourcePath {
			fileSymbolCount += len(file.Symbols)
		}
	}
	const frameLimit = 1024 * 1024
	stream, err = store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: frameLimit})
	if err != nil {
		t.Fatalf("stage graph: %v", err)
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Sequence: 2, Status: "partial", InputDigest: manifest.SnapshotDigest,
		Capabilities: analyzed.Coverage.Capabilities, Gaps: analyzed.Coverage.Gaps}
	if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: capture, Stream: stream, Done: done, AuthorizeGeneration: authorizeGeneration, MaxFrameBytes: frameLimit}); err != nil {
		t.Fatalf("seal graph: %v", err)
	}
	verify := func(_ context.Context, captured *snapshot.Snapshot, done model.ProviderDone) error {
		if done.InputDigest != captured.Manifest().SnapshotDigest {
			return index.ErrCaptureMismatch
		}
		return nil
	}
	if err := store.PublishProvider(ctx, index.PublishProviderRequest{Capture: capture, Stream: stream, AuthorizeGeneration: authorizeGeneration, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }, VerifyProvider: verify, MaxFacts: 10000, MaxFrameBytes: frameLimit}); err != nil {
		t.Fatalf("publish graph: %v", err)
	}
	if err := store.ReleaseProviderStage(ctx, capture, stream, authorizeGeneration); err != nil {
		t.Fatal(err)
	}
	view, err := store.ReadPublishedFileSymbols(ctx, index.PublishedFileSymbolsRequest{File: index.PublishedFileRequest{ProfileID: manifest.ProfileID, Path: sourcePath, AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: capture.Scanner, MaxManifestBytes: 32768, MaxFileBytes: 65536}, MaxSymbols: 128, MaxFactsBytes: frameLimit})
	if err != nil || view.File.GenerationID != generation || view.File.SnapshotDigest != manifest.SnapshotDigest || view.File.State != "READY_PARTIAL" || string(view.File.Bytes) != source || len(view.Symbols) != fileSymbolCount {
		t.Fatalf("published source/symbol snapshot mismatch: %v", err)
	}
	// There is no public relation-query primitive yet. Inspect the fixture DB
	// directly and do not treat this as product query or freshness evidence.
	db := openDB(1)
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT r.relation_id,r.source_symbol_id,r.target_symbol_id,r.kind,r.evidence_kind,r.site_path,r.start_byte,r.end_byte,r.reason,r.unresolved_label FROM relations r JOIN active_generations a ON a.generation_id=r.generation_id WHERE r.generation_id=? ORDER BY r.relation_id`, generation)
	if err != nil {
		t.Fatal(err)
	}
	var stored []model.RelationFact
	for rows.Next() {
		var item model.RelationFact
		var target, label sql.NullString
		if err := rows.Scan(&item.RelationID, &item.SourceSymbolID, &target, &item.Kind, &item.EvidenceKind, &item.SitePath, &item.Range.StartByte, &item.Range.EndByte, &item.Reason, &label); err != nil {
			t.Fatal(err)
		}
		if target.Valid {
			item.TargetSymbolID = &target.String
		}
		if label.Valid {
			item.UnresolvedLabel = &label.String
		}
		stored = append(stored, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	want := append([]model.RelationFact(nil), batch.Relations...)
	sort.Slice(want, func(i, j int) bool { return want[i].RelationID < want[j].RelationID })
	if !reflect.DeepEqual(stored, want) {
		t.Fatal("published graph relations differ from analyzer output")
	}
	var diagnostics, generatedFlag int
	var summary string
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM diagnostics WHERE generation_id=?),summary_json FROM capability_results WHERE generation_id=?`, generation, generation).Scan(&diagnostics, &summary); err != nil {
		t.Fatal(err)
	}
	wantGenerated := 0
	if generated[sourcePath] {
		wantGenerated = 1
	}
	if err := db.QueryRowContext(ctx, `SELECT generated FROM files WHERE generation_id=? AND path=?`, generation, sourcePath).Scan(&generatedFlag); err != nil || generatedFlag != wantGenerated {
		t.Fatalf("generated marker was not persisted: %v", err)
	}
	coverage, err := model.DecodeCoverageJSON([]byte(summary), frameLimit)
	if err != nil || diagnostics != len(analyzed.Diagnostics) || coverage.Status != "partial" || !reflect.DeepEqual(coverage.Capabilities, analyzed.Coverage.Capabilities) || !reflect.DeepEqual(coverage.Gaps, analyzed.Coverage.Gaps) {
		t.Fatalf("published diagnostics or coverage differ from analyzer output: %v", err)
	}
}
