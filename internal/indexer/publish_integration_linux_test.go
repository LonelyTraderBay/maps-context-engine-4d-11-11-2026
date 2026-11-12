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

func TestCapturedPublicationUsesRealLocksAndRevalidatesWorkspace(t *testing.T) {
	request, sourcePath := revalidateFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statePath := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	worktree := request.Snapshot.Manifest().WorktreeID
	const job = "22222222-2222-4222-8222-222222222222"
	const first = "33333333-3333-4333-8333-333333333333"
	const second = "44444444-4444-4444-8444-444444444444"
	const third = "55555555-5555-4555-8555-555555555555"
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(statePath, "index.sqlite"))}).String()
	openDB := func(limit int) *sql.DB {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(limit)
		return db
	}
	exclusive, err := runtime.AcquireLifecycle(ctx, state, worktree, ports.ExclusiveLease, time.Second)
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
	shared, err := runtime.AcquireLifecycle(ctx, state, worktree, ports.SharedLease, time.Second)
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
	writer, err := runtime.AcquireWriter(ctx, shared, job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = owner.Close(); _ = shared.Close(); _ = state.Close() })
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	authorizeGeneration := func(context.Context, string, string, string, int64) error { return nil }
	authorizePath := func(context.Context, string, string, string, int64) error { return nil }
	build := func(generation string) index.CaptureWrite {
		begin := index.BeginGenerationRequest{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: request.Snapshot, Writer: writer, Authorize: authorizeGeneration, MaxManifestBytes: 8192}
		if err := store.BeginGeneration(ctx, begin); err != nil {
			t.Fatal(err)
		}
		capture := index.CaptureWrite{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: request.Snapshot, Writer: writer, Scanner: scanner, Authorize: authorizePath, MaxManifestBytes: 8192, MaxSourceBytes: 4096}
		if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: []index.FileClassification{{Path: "main.go", Kind: "go"}}}); err != nil {
			t.Fatal(err)
		}
		return capture
	}
	firstCapture := build(first)
	publish := func(capture index.CaptureWrite) error {
		return store.PublishCapturedOnly(ctx, index.PublishCapturedRequest{Capture: capture, AuthorizeGeneration: authorizeGeneration, Revalidate: func(ctx context.Context, captured *snapshot.Snapshot) error {
			bound := request
			bound.Snapshot = captured
			return RevalidateLocal(ctx, bound)
		}})
	}
	if err := publish(firstCapture); err != nil {
		t.Fatal(err)
	}
	secondCapture := build(second)
	stream, err := store.BeginProviderStage(ctx, secondCapture, authorizeGeneration)
	if err != nil {
		t.Fatal(err)
	}
	path, pkg, label := "main.go", "pkg", "unresolved"
	batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: 1,
		Packages:  []model.PackageFact{{PackageKey: pkg, ImportPath: "example.invalid/pkg", ModuleKey: "module", Variant: "production", InputDigest: strings.Repeat("a", 64), SemanticStatus: "partial"}},
		Symbols:   []model.SymbolFact{{SymbolID: "symbol", Path: &path, PackageKey: &pkg, Kind: "function", Name: "main", QualifiedName: "pkg.main", ScopeKey: "scope", Range: &model.ByteRange{StartByte: 0, EndByte: 7}, Signature: "func main()"}},
		Relations: []model.RelationFact{{RelationID: "relation", SourceSymbolID: "symbol", Kind: "UNRESOLVED_CALL", EvidenceKind: "unresolved", SitePath: path, Range: model.ByteRange{StartByte: 0, EndByte: 7}, Reason: "Unresolved call.", UnresolvedLabel: &label}}, Diagnostics: []model.DiagnosticFact{}}
	if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: secondCapture, Stream: stream, Batch: batch, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Sequence: 2, Status: "partial", InputDigest: secondCapture.Snapshot.Manifest().SnapshotDigest,
		Capabilities: []model.Capability{{Name: "go_semantics", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}},
		Gaps:         []model.Gap{{Code: "UNRESOLVED_CALL", Reason: "Fixture has an unresolved call.", SourceRefs: []model.SourceRef{}}}}
	if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: secondCapture, Stream: stream, Done: done, AuthorizeGeneration: authorizeGeneration, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	verifyProvider := func(_ context.Context, captured *snapshot.Snapshot, result model.ProviderDone) error {
		if result.InputDigest != captured.Manifest().SnapshotDigest {
			return index.ErrCaptureMismatch
		}
		return nil
	}
	revalidate := func(ctx context.Context, captured *snapshot.Snapshot) error {
		bound := request
		bound.Snapshot = captured
		return RevalidateLocal(ctx, bound)
	}
	if err := store.PublishProvider(ctx, index.PublishProviderRequest{Capture: secondCapture, Stream: stream, AuthorizeGeneration: authorizeGeneration, Revalidate: revalidate, VerifyProvider: verifyProvider, MaxFacts: 100, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseProviderStage(ctx, secondCapture, stream, authorizeGeneration); err != nil {
		t.Fatal(err)
	}
	thirdCapture := build(third)
	if err := os.WriteFile(filepath.Join(filepath.Dir(sourcePath), "new.go"), []byte("package new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publish(thirdCapture); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("changed workspace published: %v", err)
	}
	read := index.PublishedFileRequest{ProfileID: request.Snapshot.Manifest().ProfileID, Path: "main.go", AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: 8192, MaxFileBytes: 4096}
	result, err := store.ReadPublishedFile(ctx, read)
	if err != nil || result.GenerationID != second || string(result.Bytes) != "package old\n" {
		t.Fatalf("prior published snapshot lost: %+v %v", result, err)
	}
}
