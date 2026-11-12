//go:build linux

package golang

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const g2Job = "22222222-2222-4222-8222-222222222222"
const g2Old = "33333333-3333-4333-8333-333333333333"
const g2New = "44444444-4444-4444-8444-444444444444"

type g2Store struct {
	store   *index.Store
	writer  *runtimeadapter.Writer
	dsn     string
	fixture *g2Fixture
}

func newG2Store(t *testing.T, ctx context.Context, f *g2Fixture) *g2Store {
	t.Helper()
	statePath := filepath.Join(f.base, "state")
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	worktree := "11111111-1111-4111-8111-111111111111"
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
	maintenanceWriter, err := runtimeadapter.AcquireWriter(ctx, exclusive, g2Job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := sqliteowner.OpenMaintenanceOwner(ctx, exclusive, openDB(1), worktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance.InitializeIndex(ctx, maintenanceWriter, g2Job); err != nil {
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
	writer, err := runtimeadapter.AcquireWriter(ctx, shared, g2Job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = owner.Close(); _ = shared.Close(); _ = state.Close() })
	return &g2Store{store: store, writer: writer, dsn: dsn, fixture: f}
}

func (s *g2Store) publish(t *testing.T, ctx context.Context, gen string, captured *snapshot.Snapshot, files inventory.FileMap, graph GraphResult) {
	t.Helper()
	manifest := captured.Manifest()
	allow := func(context.Context, string, string, string, int64) error { return nil }
	if err := s.store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: gen, JobID: g2Job, PolicyEpoch: 1, Snapshot: captured, Writer: s.writer, Authorize: allow, MaxManifestBytes: 32768}); err != nil {
		t.Fatal(err)
	}
	capture := index.CaptureWrite{GenerationID: gen, JobID: g2Job, PolicyEpoch: 1, Snapshot: captured, Writer: s.writer, Scanner: s.fixture.scanner, Authorize: allow, MaxManifestBytes: 32768, MaxSourceBytes: 65536}
	var classes []index.FileClassification
	var filenames []string
	for _, item := range manifest.Files {
		classes = append(classes, index.FileClassification{Path: item.Path, Kind: "source"})
		filenames = append(filenames, item.Path)
	}
	if err := s.store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: classes}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.StageSearchDocuments(ctx, index.StageSearchDocumentsRequest{Capture: capture, AuthorizeGeneration: allow, Paths: filenames, MaxBatchBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	stream, err := s.store.BeginProviderStage(ctx, capture, allow)
	if err != nil {
		t.Fatal(err)
	}
	batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: g2Job, Sequence: 1, Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{}, Relations: graph.Relations, Diagnostics: []model.DiagnosticFact{}}
	seen := map[string]bool{}
	for _, syntax := range graph.Typed.Syntax {
		if !seen[syntax.Package.PackageKey] {
			batch.Packages = append(batch.Packages, syntax.Package)
			seen[syntax.Package.PackageKey] = true
		}
		batch.Symbols = append(batch.Symbols, syntax.Symbols...)
		batch.Diagnostics = append(batch.Diagnostics, syntax.Diagnostics...)
	}
	stream, err = s.store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: 1024 * 1024})
	if err != nil {
		t.Fatalf("stage real fixture facts: %v", err)
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: g2Job, Sequence: 2, Status: "partial", InputDigest: manifest.SnapshotDigest,
		Capabilities: []model.Capability{{Name: "go_semantics", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "SYNTHETIC", Reason: "Fixture coverage is partial.", SourceRefs: []model.SourceRef{}}}}
	if err := s.store.SealProviderStage(ctx, index.SealProviderRequest{Capture: capture, Stream: stream, Done: done, AuthorizeGeneration: allow, MaxFrameBytes: 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	verify := func(_ context.Context, source *snapshot.Snapshot, done model.ProviderDone) error {
		if source.Manifest().SnapshotDigest != done.InputDigest || graph.Typed.Loader.SnapshotDigest != done.InputDigest {
			return index.ErrCaptureMismatch
		}
		return nil
	}
	revalidate := func(ctx context.Context, source *snapshot.Snapshot) error {
		return indexer.RevalidateLocal(ctx, s.fixture.revalidate(source, files))
	}
	if err := s.store.PublishProvider(ctx, index.PublishProviderRequest{Capture: capture, Stream: stream, AuthorizeGeneration: allow, Revalidate: revalidate, VerifyProvider: verify, MaxFacts: 10000, MaxFrameBytes: 1024 * 1024}); err != nil {
		t.Fatalf("publish fixture facts: %v", err)
	}
	if err := s.store.ReleaseProviderStage(ctx, capture, stream, allow); err != nil {
		t.Fatal(err)
	}
}

func (s *g2Store) targetCallCount(t *testing.T, ctx context.Context, gen string) int {
	t.Helper()
	db, err := sql.Open("sqlite", s.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM relations r JOIN symbols target ON target.generation_id=r.generation_id AND target.symbol_id=r.target_symbol_id WHERE r.generation_id=? AND r.kind='CALLS_DIRECT' AND target.name='Target'`, gen).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func (f *g2Fixture) validationInput(t *testing.T, captured *snapshot.Snapshot, files inventory.FileMap, gen, indexIncarnation string) app.ValidateCapturedInput {
	t.Helper()
	m := captured.Manifest()
	file, ok := captured.File("target.go")
	if !ok {
		t.Fatal("missing target capture")
	}
	member := model.ScopeMember{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: gen, SnapshotDigest: m.SnapshotDigest, PolicyDigest: m.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: indexIncarnation, RecordsIncarnation: "66666666-6666-4666-8666-666666666666"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: gen, Path: "target.go", ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: int64(file.Len())}}
	return app.ValidateCapturedInput{Scope: scope, Sources: []app.ValidationSource{{Ref: ref}}, MaxInputBytes: 8192, MaxManifestBytes: 32768,
		CurrentScope: func(context.Context) (model.ScopeVector, error) { return scope, nil }, ResolveCapture: func(_ context.Context, pinned model.ScopeMember) (*snapshot.Snapshot, error) {
			if pinned.GenerationID != gen {
				return nil, errors.New("generation unavailable")
			}
			return captured, nil
		},
		AuthorizeSource: func(context.Context, model.ScopeMember, model.SourceRef) error { return nil }, VerifyWorkspace: func(ctx context.Context, _ model.ScopeMember, source *snapshot.Snapshot) (app.WorkspaceObservation, error) {
			err := indexer.RevalidateLocal(ctx, f.revalidate(source, files))
			if errors.Is(err, indexer.ErrSnapshotChanged) {
				return app.WorkspaceChanged, nil
			}
			if err != nil {
				return app.WorkspaceUnknown, err
			}
			return app.WorkspaceObservedCurrent, nil
		}}
}
