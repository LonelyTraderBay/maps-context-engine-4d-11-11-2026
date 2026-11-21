//go:build linux

package tsprovider

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func TestTSFactsReachSharedContext(t *testing.T) {
	const site = "app/main.ts"
	files := map[string]string{
		"app/tsconfig.json": `{"compilerOptions":{"strict":true,"target":"es2022","lib":["es2022"],"types":[]},"files":["main.ts","lib.ts"]}`,
		site:                "import { twice } from './lib'; export const output = twice(21);\n",
		"app/lib.ts":        "export function twice(value: number): number { return value * 2; }\n",
	}
	config, job, capture := fixture(t, "typed_when_available", files)
	provider, profile, err := New(Installation{
		Node: config.NodePath, Worker: config.Entrypoint, ScratchParent: config.ScratchParent,
		CompilerRoot: config.Compiler.Root, CompilerArchive: config.Compiler.ArchivePath, CompilerManifest: config.Compiler.ManifestPath,
	}, []string{"app/tsconfig.json"}, "typed_when_available")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile, job.Start.Profile) {
		t.Fatal("accepted profile differs from pinned installation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	job.Start.DeadlineAt = deadline.UTC().Format(time.RFC3339Nano)
	manifest := capture.Manifest()

	stateDir := t.TempDir()
	root, err := paths.OpenRoot(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, root, manifest.WorktreeID, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(stateDir, "index.sqlite")}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	identity, err := index.Initialize(ctx, db, manifest.WorktreeID, lease)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, lease, job.Start.JobID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	store, err := index.Open(ctx, db, db, manifest.WorktreeID, lease)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(ctx context.Context, worktree, profile, policy string, epoch int64) error {
		if worktree != manifest.WorktreeID || profile != manifest.ProfileID || policy != manifest.PolicyDigest || epoch != 1 {
			return index.ErrCaptureMismatch
		}
		return job.Recheck(ctx)
	}
	pathAuth := func(ctx context.Context, worktree, path, policy string, epoch int64) error {
		if _, ok := files[path]; !ok {
			return index.ErrCaptureMismatch
		}
		return authorize(ctx, worktree, manifest.ProfileID, policy, epoch)
	}
	stage := index.CaptureWrite{GenerationID: job.Start.GenerationID, JobID: job.Start.JobID, PolicyEpoch: 1,
		Snapshot: capture, Scanner: scanner, Authorize: pathAuth, Writer: writer, MaxManifestBytes: 65536, MaxSourceBytes: 65536}
	err = store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: stage.GenerationID, JobID: stage.JobID, PolicyEpoch: 1,
		Snapshot: capture, Writer: writer, Authorize: authorize, MaxManifestBytes: stage.MaxManifestBytes})
	if err != nil {
		t.Fatal(err)
	}
	var classes []index.FileClassification
	for _, item := range manifest.Files {
		classes = append(classes, index.FileClassification{Path: item.Path, Kind: "text"})
	}
	if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: stage, Files: classes}); err != nil {
		t.Fatal(err)
	}
	stream, err := store.BeginProviderStage(ctx, stage, authorize)
	if err != nil {
		t.Fatal(err)
	}
	done, err := provider.Analyze(ctx, job, func(ctx context.Context, batch model.ProviderBatch) error {
		_, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: stage, Stream: stream, Batch: batch, MaxFrameBytes: 65536})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: stage, Stream: stream, Done: done,
		MaxFrameBytes: 65536, AuthorizeGeneration: authorize}); err != nil {
		t.Fatal(err)
	}
	err = store.PublishProvider(ctx, index.PublishProviderRequest{Capture: stage, Stream: stream, MaxFrameBytes: 65536, MaxFacts: 100,
		AuthorizeGeneration: authorize,
		Revalidate: func(ctx context.Context, candidate *snapshot.Snapshot) error {
			if candidate.Manifest().SnapshotDigest != manifest.SnapshotDigest {
				return index.ErrCaptureMismatch
			}
			return job.Recheck(ctx)
		},
		VerifyProvider: func(ctx context.Context, candidate *snapshot.Snapshot, terminal model.ProviderDone) error {
			if !reflect.DeepEqual(terminal, done) || terminal.JobID != stage.JobID ||
				terminal.VerifyInputDigest(candidate.Manifest().SnapshotDigest) != nil {
				return index.ErrCaptureMismatch
			}
			return job.Recheck(ctx)
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseProviderStage(ctx, stage, stream, authorize); err != nil {
		t.Fatal(err)
	}
	head, err := store.ReadGeneration(ctx, manifest.ProfileID, "", authorize, 65536)
	if err != nil || head.State != "READY" || head.GenerationID != stage.GenerationID {
		t.Fatalf("published generation: %+v, %v", head, err)
	}
	member := model.ScopeMember{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: stage.GenerationID,
		SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1,
		RecordEpoch: 1, IndexIncarnation: identity.Incarnation, RecordsIncarnation: stage.GenerationID}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: stage.JobID,
		UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := store.ReadPublishedGraph(ctx, index.PublishedGraphRequest{
		Member: member, VectorDigest: scope.VectorDigest, AuthorizeGeneration: authorize, AuthorizePath: pathAuth, Scanner: scanner,
		MaxManifestBytes: 65536, MaxFileBytes: 65536, MaxSourceBytes: 65536, MaxFactBytes: 65536, MaxFacts: 100})
	if err != nil {
		t.Fatal(err)
	}
	var target string
	for _, item := range graph.Symbols {
		if item.Name == "twice" && item.Kind == "function" {
			target = item.SymbolID
		}
	}
	if target == "" {
		t.Fatal("published graph omitted the TS function")
	}
	input := contextpack.Input{Scope: scope, Target: model.SymbolRef{Kind: "symbol", SymbolID: target,
		GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}},
		Snapshot: graph.Snapshot, Packages: graph.Packages, Symbols: graph.Symbols, Relations: graph.Relations, Coverage: graph.Coverage,
		Scanner: scanner, VerifyFacts: graph.VerifyFacts,
		AuthorizeScope: func(ctx context.Context, observed model.ScopeVector, entry model.ScopeMember) error {
			if observed.VectorDigest != scope.VectorDigest || entry.GenerationID != member.GenerationID {
				return index.ErrCaptureMismatch
			}
			return authorize(ctx, entry.WorktreeID, entry.ProfileID, entry.PolicyDigest, entry.PolicyEpoch)
		},
		AuthorizePath: func(ctx context.Context, entry model.ScopeMember, path string) error {
			return pathAuth(ctx, entry.WorktreeID, path, entry.PolicyDigest, entry.PolicyEpoch)
		},
		MaxManifestBytes: 65536, MaxFactBytes: 65536, MaxSourceBytes: 65536, MaxFacts: 100,
		MaxNodes: 100, MaxEdges: 100, MaxDepth: 2, MaxOutputBytes: 65536}
	selected, err := contextpack.Select(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	report, err := selected.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range report.References {
		if ref.Relation.Kind == "CALLS_DIRECT" && ref.Source.Path == site &&
			files[site][ref.Source.Range.StartByte:ref.Source.Range.EndByte] == "twice" &&
			report.Coverage.Status == "supported_scope_complete" {
			return
		}
	}
	t.Fatal("shared context omitted the exact TS caller or its captured range")
}
