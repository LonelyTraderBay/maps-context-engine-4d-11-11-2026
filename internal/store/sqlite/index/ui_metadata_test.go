package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/uimap"
)

func TestUIAtomicImportAndImmutableBinding(t *testing.T) {
	t.Run("standalone", func(t *testing.T) {
		source := []byte("export function Page() { return <button/> }\n")
		hash := uiHash(source)
		profile := strings.Repeat("a", 64)
		anchor := model.AnchorEntry{Kind: "component_definition", Path: "entry.tsx", Range: model.ByteRange{StartByte: 16, EndByte: 20}, ContentHash: hash}
		var err error
		anchor.AnchorID, err = model.SourceAnchorDigest(model.SourceAnchorIdentity{Path: anchor.Path, Kind: anchor.Kind,
			Range: anchor.Range, ContentHash: hash, ProviderDigest: profile}, source, 65536)
		if err != nil {
			t.Fatal(err)
		}
		build := model.BuildManifest{SchemaVersion: 1, WorktreeID: fixtureWorktree, ProfileID: profile, ProviderVersion: "1.0.0",
			BuildConfigDigest: profile, InputManifestDigest: profile, ProviderDigest: profile, Mode: "development",
			ModuleCoverage: "loaded_modules_only", Sources: []model.ManifestSource{{Path: anchor.Path, ContentHash: hash}},
			Anchors: []model.AnchorEntry{anchor}}
		build.BuildMapID, err = model.ComputeBuildMapDigest(build, 65536)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(build)
		if err != nil {
			t.Fatal(err)
		}
		exerciseUIBinding(t, source, raw)
	})
	t.Run("vite-produced", func(t *testing.T) {
		folder := os.Getenv("MCE4D_T085_MANIFEST_OUT")
		if folder == "" {
			t.Skip("requires output from the real Vite fixture")
		}
		source, err := os.ReadFile(filepath.Join(folder, "fixture-source.tsx"))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(folder, "fixture-build.json"))
		if err != nil {
			t.Fatal(err)
		}
		exerciseUIBinding(t, source, raw)
	})
}

func uiHash(bytes []byte) string {
	digest := sha256.Sum256(bytes)
	return hex.EncodeToString(digest[:])
}

func exerciseUIBinding(t *testing.T, source, raw []byte) {
	t.Helper()
	build, err := model.DecodeBuildManifestJSON(raw, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	artifactRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifactRoot, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	approval := uimap.Approval{WorktreeID: build.WorktreeID, ProfileID: build.ProfileID, ProviderDigest: build.ProviderDigest,
		BuildConfigDigest: build.BuildConfigDigest, InputManifestDigest: build.InputManifestDigest, ArtifactDigest: uiHash(raw)}
	artifact, err := uimap.ReadApprovedArtifact(ctx, root, "manifest.json", approval, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	wrong := approval
	wrong.ArtifactDigest = strings.Repeat("f", 64)
	if _, err := uimap.ReadApprovedArtifact(ctx, root, "manifest.json", wrong, 1<<20); !errors.Is(err, uimap.ErrBuildArtifact) {
		t.Fatalf("wrong artifact digest: %v", err)
	}
	store, db, begin := beginFixture(t)
	const path = "entry.tsx"
	file, err := snapshot.NewFile(path, source, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	hash, size := file.ContentHash(), int64(file.Len())
	policyDigest, providerDigest := strings.Repeat("b", 64), strings.Repeat("c", 64)
	captured, err := snapshot.Freeze(model.CaptureManifest{SchemaVersion: 1, WorktreeID: fixtureWorktree, ProfileID: build.ProfileID,
		PolicyDigest: policyDigest, ProviderDigest: providerDigest, DependencyInputDigests: []string{},
		Files: []model.ManifestFile{{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &size}}},
		[]snapshot.File{file}, 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(ctx context.Context, w, p, d string, epoch int64) error {
		if w != fixtureWorktree || p != build.ProfileID || d != policyDigest || epoch != 1 {
			return ErrCaptureMismatch
		}
		return ctx.Err()
	}
	pathAuth := func(ctx context.Context, w, name, d string, epoch int64) error {
		if name != path {
			return ErrCaptureMismatch
		}
		return authorize(ctx, w, build.ProfileID, d, epoch)
	}
	begin.Snapshot, begin.Authorize, begin.MaxManifestBytes = captured, authorize, 1<<20
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	stage := CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, Snapshot: captured, PolicyEpoch: 1, Scanner: scanner,
		Authorize: pathAuth, Writer: begin.Writer, MaxManifestBytes: 1 << 20, MaxSourceBytes: 1 << 20}
	if err := store.StageFiles(ctx, StageFilesRequest{Capture: stage, Files: []FileClassification{{Path: path, Kind: "tsx"}}}); err != nil {
		t.Fatal(err)
	}
	stream, err := store.BeginProviderStage(ctx, stage, authorize)
	if err != nil {
		t.Fatal(err)
	}
	batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: stage.JobID, Sequence: 1, Packages: []model.PackageFact{},
		Symbols: []model.SymbolFact{}, Relations: []model.RelationFact{}, Diagnostics: []model.DiagnosticFact{}}
	for i, anchor := range build.Anchors {
		if anchor.Kind != "component_definition" {
			continue
		}
		label := string(source[anchor.Range.StartByte:anchor.Range.EndByte])
		batch.Symbols = append(batch.Symbols, model.SymbolFact{SymbolID: fmt.Sprintf("component-%d", i), Kind: "function", Name: label,
			QualifiedName: path + "#" + label, Path: &anchor.Path, Range: &anchor.Range, ScopeKey: "scope", Exported: true})
	}
	stageProvider := StageProviderRequest{Capture: stage, Stream: stream, Batch: batch, MaxFrameBytes: 65536}
	if _, err := store.StageProviderBatch(ctx, stageProvider); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stageProvider)
	done.Sequence = 2
	if err := store.SealProviderStage(ctx, sealRequest(stageProvider, done)); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishProvider(ctx, publishProviderFixture(stageProvider)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseProviderStage(ctx, stage, stream, authorize); err != nil {
		t.Fatal(err)
	}

	write := UIWriteAuthority{Writer: begin.Writer, JobID: begin.JobID, Authorize: func(ctx context.Context, _ Identity) error { return ctx.Err() }}
	identity, err := InitializeUIAddon(ctx, db, fixtureWorktree, fixtureLease(ports.ExclusiveLease), write)
	if err != nil || identity != store.identity {
		t.Fatalf("addon migration changed index identity: %v %v", identity, err)
	}
	if _, err := Open(ctx, db, db, fixtureWorktree, fixtureLease(ports.SharedLease)); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("R1 admits V4: %v", err)
	}
	addon, err := OpenUIAddon(ctx, db, fixtureWorktree, fixtureLease(ports.SharedLease))
	if err != nil {
		t.Fatal(err)
	}
	deny := errors.New("fixture authority changed")
	late := write
	checks := 0
	late.Authorize = func(context.Context, Identity) error {
		checks++
		if checks == 2 {
			return deny
		}
		return nil
	}
	if err := addon.ImportBuildMap(ctx, late, artifact); !errors.Is(err, deny) {
		t.Fatalf("authority change didn't abort import: %v", err)
	}
	count := func(query string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			t.Fatalf("count %q got %d want %d: %v", query, got, want, err)
		}
	}
	count("SELECT count(*) FROM ui_build_maps", 0)
	count("SELECT count(*) FROM ui_source_anchors", 0)
	for range 2 {
		if err := addon.ImportBuildMap(ctx, write, artifact); err != nil {
			t.Fatal(err)
		}
	}
	count("SELECT count(*) FROM ui_build_maps", 1)
	count("SELECT count(*) FROM ui_source_anchors", len(build.Anchors))
	count("SELECT count(*) FROM ui_build_bindings", 0)
	member := model.ScopeMember{WorktreeID: fixtureWorktree, ProfileID: build.ProfileID, GenerationID: stage.GenerationID,
		SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: policyDigest, PolicyEpoch: 1, BindingRevision: 1,
		IndexIncarnation: identity.Incarnation, RecordsIncarnation: fixtureGeneration}
	bind := UIBindRequest{Authority: write, BuildMapID: build.BuildMapID, Member: member, MaxBytes: 1 << 20, Scanner: scanner,
		AuthorizeGeneration: authorize, AuthorizePath: pathAuth,
		Resolver: model.ResolverIdentity{ImplementationArtifactHash: strings.Repeat("d", 64), SemanticVersion: "1.0.0", OptionsDigest: strings.Repeat("e", 64)}}
	wrongBind := bind
	wrongBind.Member.SnapshotDigest = strings.Repeat("f", 64)
	if _, err := addon.BindBuildMap(ctx, wrongBind); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("wrong generation snapshot admitted: %v", err)
	}
	checks = 0
	wrongBind = bind
	wrongBind.Authority = late
	if _, err := addon.BindBuildMap(ctx, wrongBind); !errors.Is(err, deny) {
		t.Fatalf("late binding denial accepted: %v", err)
	}
	count("SELECT count(*) FROM ui_build_bindings", 0)
	count("SELECT count(*) FROM ui_anchor_links", 0)
	first, err := addon.BindBuildMap(ctx, bind)
	if err != nil || first.Anchors != len(build.Anchors) {
		t.Fatalf("binding %v %v", first, err)
	}
	again, err := addon.BindBuildMap(ctx, bind)
	if err != nil || again != first {
		t.Fatalf("idempotent binding %v %v", again, err)
	}
	bind.Resolver.SemanticVersion = "1.0.1"
	next, err := addon.BindBuildMap(ctx, bind)
	if err != nil || next.BindingID == first.BindingID {
		t.Fatalf("resolver version not independent: %v %v", next, err)
	}
	count("SELECT count(*) FROM ui_build_bindings", 2)
	count("SELECT count(*) FROM ui_anchor_links", 2*len(build.Anchors))
	count("SELECT count(*) FROM ui_anchor_links WHERE json_extract(target_ref_json,'$.kind')='symbol'", 2*len(batch.Symbols))
	count("SELECT count(*) FROM pragma_foreign_key_check", 0)

	reader, err := OpenUICompatible(ctx, db, db, fixtureWorktree, fixtureLease(ports.SharedLease))
	if err != nil {
		t.Fatal(err)
	}
	read := PublishedGraphRequest{Member: member, AuthorizeGeneration: authorize, MaxManifestBytes: 1 << 20}
	for _, anchor := range build.Anchors {
		match, err := reader.ReadUIAnchorMatch(ctx, read, first, anchor.AnchorID)
		if err != nil || match == nil || match.AnchorRef.BindingID != first.BindingID || match.AnchorRef.AnchorID != anchor.AnchorID ||
			len(match.SourceRefs) != 1 || match.SourceRefs[0].Range != anchor.Range || match.SourceRefs[0].ContentHash != anchor.ContentHash {
			t.Fatalf("pinned anchor mismatch: %+v %v", match, err)
		}
	}
	absent, err := reader.ReadUIAnchorMatch(ctx, read, first, strings.Repeat("f", 64))
	if absent != nil || err != nil {
		t.Fatalf("unknown anchor generated evidence: %+v %v", absent, err)
	}
}
