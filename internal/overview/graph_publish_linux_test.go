//go:build linux

package overview

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// This integration publishes synthetic captured facts with the real Linux OS
// leases, SQLite owner and index adapter. Principal and scope authorities are
// fixture callbacks; it is not transport or live-workspace acceptance.
func TestPublishedGraphTraversalWithLinuxLeases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const job = "22222222-2222-4222-8222-222222222222"
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(stateDir, "index.sqlite"))}).String()
	openDB := func(limit int) *sql.DB {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(limit)
		return db
	}
	exclusive, err := runtimeadapter.AcquireLifecycle(ctx, root, worktree, ports.ExclusiveLease, time.Second)
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
	shared, err := runtimeadapter.AcquireLifecycle(ctx, root, worktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := sqliteowner.OpenIndexOwner(ctx, shared, openDB(4), openDB(1), worktree)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtimeadapter.AcquireWriter(ctx, shared, job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close(); _ = owner.Close(); _ = shared.Close() }()
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	const source = "package main\nfunc Foo() { Foo() }\n"
	file, err := snapshot.NewFile("main.go", []byte(source), 1024)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{{Path: file.Path(), Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest = captured.Manifest()
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	if err := store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Authorize: authorize, MaxManifestBytes: 8192}); err != nil {
		t.Fatal(err)
	}
	capture := index.CaptureWrite{GenerationID: generation, JobID: job, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Scanner: scanner, Authorize: authorize, MaxManifestBytes: 8192, MaxSourceBytes: 1024}
	if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: []index.FileClassification{{Path: "main.go", Kind: "go"}}}); err != nil {
		t.Fatal(err)
	}
	stream, err := store.BeginProviderStage(ctx, capture, authorize)
	if err != nil {
		t.Fatal(err)
	}
	path, pkg, target := "main.go", "pkg", "symbol"
	declaration := int64(strings.Index(source, "Foo"))
	call := int64(strings.LastIndex(source, "Foo"))
	batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: 1,
		Packages:  []model.PackageFact{{PackageKey: pkg, ImportPath: "fixture/main", ModuleKey: "module", Variant: "production", InputDigest: manifest.SnapshotDigest, SemanticStatus: "partial"}},
		Symbols:   []model.SymbolFact{{SymbolID: target, Path: &path, PackageKey: &pkg, Kind: "function", Name: "Foo", QualifiedName: "pkg.Foo", ScopeKey: pkg, Range: &model.ByteRange{StartByte: declaration, EndByte: declaration + 3}}},
		Relations: []model.RelationFact{{RelationID: "call", SourceSymbolID: target, TargetSymbolID: &target, Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: path, Range: model.ByteRange{StartByte: call, EndByte: call + 3}}}, Diagnostics: []model.DiagnosticFact{}}
	stream, err = store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Sequence: 2, Status: "partial", InputDigest: manifest.SnapshotDigest, Capabilities: []model.Capability{{Name: "go_semantics", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "FIXTURE_PARTIAL", Reason: "Synthetic provider coverage is partial.", SourceRefs: []model.SourceRef{}}}}
	if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: capture, Stream: stream, Done: done, AuthorizeGeneration: authorize, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishProvider(ctx, index.PublishProviderRequest{Capture: capture, Stream: stream, AuthorizeGeneration: authorize, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }, VerifyProvider: func(context.Context, *snapshot.Snapshot, model.ProviderDone) error { return nil }, MaxFacts: 100, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseProviderStage(ctx, capture, stream, authorize); err != nil {
		t.Fatal(err)
	}
	identity, err := store.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: identity.Incarnation, RecordsIncarnation: job}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: job, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := store.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: scope.VectorDigest, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: scanner, MaxManifestBytes: 8192, MaxFileBytes: 1024, MaxSourceBytes: 1024, MaxFactBytes: 65536, MaxFacts: 100})
	if err != nil {
		t.Fatal(err)
	}
	input := contextpack.Input{Scope: scope, Target: model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation}, SymbolID: target}, Snapshot: catalog.Snapshot, Packages: catalog.Packages, Symbols: catalog.Symbols, Relations: catalog.Relations, Coverage: catalog.Coverage, Scanner: scanner, AuthorizeScope: func(context.Context, model.ScopeVector, model.ScopeMember) error { return nil }, AuthorizePath: func(context.Context, model.ScopeMember, string) error { return nil }, VerifyFacts: catalog.VerifyFacts, MaxManifestBytes: 8192, MaxFactBytes: 65536, MaxSourceBytes: 1024, MaxFacts: 100, MaxNodes: 10, MaxEdges: 10, MaxDepth: 2, MaxOutputBytes: 65536}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "projection-v1", Scope: scope}
	var key [32]byte
	key[0] = 1
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	graph, err := NewProjectedGraph(ctx, input, codec, binding, guard, 10, 10, 65536)
	if err != nil {
		t.Fatal(err)
	}
	cursor := ""
	edges := 0
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("graph cursor did not terminate")
		}
		page, err := graph.Page(ctx, cursor, binding.PrincipalDigest, guard, 10, 10, 8192)
		if err != nil || page.Coverage.Status != "partial" {
			t.Fatalf("published graph projection failed: %v", err)
		}
		for _, edge := range page.Result.Data.Edges {
			if len(edge.EvidenceRefs) != 1 || edge.EvidenceRefs[0].EvidenceID != "call" {
				t.Fatal("published evidence changed")
			}
			edges++
		}
		if page.Result.NextCursor == nil {
			break
		}
		cursor = *page.Result.NextCursor
	}
	if edges != 1 {
		t.Fatalf("published graph lost or duplicated edges: %d", edges)
	}
}
