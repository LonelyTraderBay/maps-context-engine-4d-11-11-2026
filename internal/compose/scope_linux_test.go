//go:build linux

package compose

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const scopeJob = "22222222-2222-4222-8222-222222222222"
const scopeGeneration = "33333333-3333-4333-8333-333333333333"

type scopeBudgetReservation struct{}

func (scopeBudgetReservation) Check(context.Context) error { return nil }
func (scopeBudgetReservation) Close() error                { return nil }

type scopeShortTTLConfig struct{}

func (scopeShortTTLConfig) Read(ctx context.Context, _ int) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return []byte(`{"schema_version":1,"core":{"pack_ttl_seconds":5}}`), true, nil
}
func (scopeShortTTLConfig) Set(context.Context, []byte, int) (ports.RuntimeConfigWriteResult, error) {
	return ports.RuntimeConfigWriteResult{State: "not_applied"}, errors.New("fixture is read-only")
}

type refusingPackCreate struct{ *packstore.Store }

func (refusingPackCreate) Create(context.Context, string, []byte, func(context.Context, []byte) error) error {
	return packstore.ErrUncertain
}

// Track successful fixture publications even if the enclosing CLI call later
// fails before returning its token, so one subtest cannot consume another's
// quota. Only the exact created inode is eligible for cleanup.
type trackedFixturePackStore struct {
	*packstore.Store
	t         *testing.T
	owner     *paths.Root
	protected []*paths.Root
	directory string
}

func (s *trackedFixturePackStore) Create(ctx context.Context, id string, raw []byte, verify func(context.Context, []byte) error) error {
	if err := s.Store.Create(ctx, id, raw, verify); err != nil {
		return err
	}
	name := filepath.Join(s.directory, id+".pack")
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("published fixture file unavailable")
	}
	s.t.Cleanup(func() { cleanupRetainedFixtureFile(s.t, s.owner, s.protected, name, info) })
	return nil
}

// Remove only the exact inode created by this subtest in its private fixture.
func cleanupRetainedFixtureFile(t *testing.T, owner *paths.Root, protected []*paths.Root, name string, expected os.FileInfo) {
	t.Helper()
	rootPath, err := owner.CanonicalPath()
	if err != nil || runtimeadapter.CheckCleanupRoot(owner, protected) != nil {
		t.Errorf("owned retained fixture root changed before cleanup")
		return
	}
	relative, err := filepath.Rel(rootPath, name)
	if err != nil || relative != filepath.Join("scratch", "pack", filepath.Base(name)) {
		t.Errorf("owned retained fixture cleanup escaped its directory")
		return
	}
	current, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil || expected == nil || !current.Mode().IsRegular() || !os.SameFile(expected, current) {
		t.Errorf("owned retained fixture changed before cleanup")
		return
	}
	if err := os.Remove(name); err != nil {
		t.Errorf("owned retained fixture cleanup failed: %v", err)
	}
}

func scopeDB(t *testing.T, path string, limit int) *sql.DB {
	t.Helper()
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(limit)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func scopeRoot(t *testing.T, path string) *paths.Root {
	t.Helper()
	root, err := paths.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func scopeJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// This fixture creates all roots and databases under its private temp folder.
// Git is run only on that synthetic repo; principal and publication proof are
// explicitly fixture callbacks, while SQLite and Linux locks are real.
func TestScopeReaderComposesRealStoresAndRejectsChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()
	base := t.TempDir()
	repo, template, catalogDir, worktreeDir, sandboxDir := filepath.Join(base, "repo"), filepath.Join(base, "template"), filepath.Join(base, "catalog"), filepath.Join(base, "worktree"), filepath.Join(base, "sandbox")
	for _, dir := range []string{repo, template, catalogDir, worktreeDir, sandboxDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	cmd := exec.CommandContext(ctx, git, "init", "--template="+template, repo)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture git: %v %s", err, out)
	}
	// Enough captured occurrences to force rename continuation at the policy
	// wire cap, while remaining below this fixture's 1 KiB file limit.
	source := []byte("package fixture\n// sharedneedle\nfunc Example() { " + strings.Repeat("Example();", 64) + " }\n")
	otherSource := []byte("package fixture\n// sharedneedle\nfunc ExampleTwo() {}\n")
	moduleSource := []byte("module example.invalid/fixture\n\ngo 1.27\n")
	documentSource := []byte("Objective: map the declared event.\nConstraint: preserve the schema.\n")
	vocabularySource := []byte(`{"schema_version":1,"entries":[{"alias":"Objective","entity_id":"goal","path":"design.md","range":{"start_byte":0,"end_byte":9}},{"alias":"Objective","entity_id":"target","path":"design.md","range":{"start_byte":0,"end_byte":9}}]}`)
	if err := os.WriteFile(filepath.Join(repo, "main.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "other.go"), otherSource, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), moduleSource, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "design.md"), documentSource, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "vocabulary.json"), vocabularySource, 0600); err != nil {
		t.Fatal(err)
	}
	root := scopeRoot(t, repo)
	common, err := runtimeadapter.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = common.Close() })
	catalogRoot, worktreeRoot := scopeRoot(t, catalogDir), scopeRoot(t, worktreeDir)
	catalogPath := filepath.Join(catalogDir, "catalog.sqlite")
	catExclusive, err := runtimeadapter.AcquireLifecycle(ctx, catalogRoot, "catalog", ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	catMutation, err := runtimeadapter.AcquireCatalogMutation(ctx, catExclusive, scopeJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	catMaintenance, err := sqliteowner.OpenMaintenanceOwner(ctx, catExclusive, scopeDB(t, catalogPath, 1), "catalog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catMaintenance.InitializeCatalog(ctx, catMutation, scopeJob); err != nil {
		t.Fatal(err)
	}
	if err := catMaintenance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := catMutation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := catExclusive.Close(); err != nil {
		t.Fatal(err)
	}
	catShared, err := runtimeadapter.AcquireLifecycle(ctx, catalogRoot, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catShared.Close() })
	catOwner, err := sqliteowner.OpenCatalogOwner(ctx, catShared, scopeDB(t, catalogPath, 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catOwner.Close() })
	cat, err := catOwner.Store()
	if err != nil {
		t.Fatal(err)
	}
	catMutation, err = runtimeadapter.AcquireCatalogMutation(ctx, catShared, scopeJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := cat.WithMutation(ctx, catMutation, scopeJob)
	if err != nil {
		t.Fatal(err)
	}
	provider := strings.Repeat("c", 64)
	p := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{repo}, DeniedGlobs: []string{}, TrustedRulePaths: []string{"design.md", "vocabulary.json"}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 1024, MaxSourceBytes: 8192, MaxOutputBytes: 16384, SecretRulesVersion: "v1"}
	profile := model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: provider, ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	registration, err := bound.Register(ctx, root, common, "", "default", scopeJSON(t, p), scopeJSON(t, profile))
	if err != nil {
		t.Fatal(err)
	}
	if err := catMutation.Close(); err != nil {
		t.Fatal(err)
	}
	indexPath, recordsPath := filepath.Join(worktreeDir, "index.sqlite"), filepath.Join(worktreeDir, "records.sqlite")
	exclusive, err := runtimeadapter.AcquireLifecycle(ctx, worktreeRoot, registration.WorktreeID, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtimeadapter.AcquireWriter(ctx, exclusive, scopeJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	indexMaintenance, err := sqliteowner.OpenMaintenanceOwner(ctx, exclusive, scopeDB(t, indexPath, 1), registration.WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := indexMaintenance.InitializeIndex(ctx, writer, scopeJob); err != nil {
		t.Fatal(err)
	}
	if err := indexMaintenance.Close(); err != nil {
		t.Fatal(err)
	}
	recordsMaintenance, err := sqliteowner.OpenMaintenanceOwner(ctx, exclusive, scopeDB(t, recordsPath, 1), registration.WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordsMaintenance.InitializeRecords(ctx, writer, scopeJob); err != nil {
		t.Fatal(err)
	}
	if err := recordsMaintenance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	shared, err := runtimeadapter.AcquireLifecycle(ctx, worktreeRoot, registration.WorktreeID, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	indexOwner, err := sqliteowner.OpenIndexOwner(ctx, shared, scopeDB(t, indexPath, 4), scopeDB(t, indexPath, 1), registration.WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = indexOwner.Close() })
	recordsDB := scopeDB(t, recordsPath, 1)
	recordsOwner, err := sqliteowner.OpenRecordsOwner(ctx, shared, recordsDB, registration.WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recordsOwner.Close() })
	idx, err := indexOwner.Store()
	if err != nil {
		t.Fatal(err)
	}
	recordStore, err := recordsOwner.Store()
	if err != nil {
		t.Fatal(err)
	}
	writer, err = runtimeadapter.AcquireWriter(ctx, shared, scopeJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	file, err := snapshot.NewFile("main.go", source, 1024)
	if err != nil {
		t.Fatal(err)
	}
	other, err := snapshot.NewFile("other.go", otherSource, 1024)
	if err != nil {
		t.Fatal(err)
	}
	moduleFile, err := snapshot.NewFile("go.mod", moduleSource, 1024)
	if err != nil {
		t.Fatal(err)
	}
	documentFile, err := snapshot.NewFile("design.md", documentSource, 1024)
	if err != nil {
		t.Fatal(err)
	}
	vocabularyFile, err := snapshot.NewFile("vocabulary.json", vocabularySource, 1024)
	if err != nil {
		t.Fatal(err)
	}
	contentHash, length := file.ContentHash(), int64(file.Len())
	otherHash, otherLength := other.ContentHash(), int64(other.Len())
	moduleHash, moduleLength := moduleFile.ContentHash(), int64(moduleFile.Len())
	documentHash, documentLength := documentFile.ContentHash(), int64(documentFile.Len())
	vocabularyHash, vocabularyLength := vocabularyFile.ContentHash(), int64(vocabularyFile.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: registration.WorktreeID, ProfileID: registration.ProfileID, PolicyDigest: registration.PolicyDigest, ProviderDigest: provider, Files: []model.ManifestFile{{Path: "design.md", Status: "captured", ContentHash: &documentHash, ByteLength: &documentLength}, {Path: "go.mod", Status: "captured", ContentHash: &moduleHash, ByteLength: &moduleLength}, {Path: "main.go", Status: "captured", ContentHash: &contentHash, ByteLength: &length}, {Path: "other.go", Status: "captured", ContentHash: &otherHash, ByteLength: &otherLength}, {Path: "vocabulary.json", Status: "captured", ContentHash: &vocabularyHash, ByteLength: &vocabularyLength}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{documentFile, moduleFile, file, other, vocabularyFile}, 8192, 8192)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	scanner, err := policy.NewScanner(8192)
	if err != nil {
		t.Fatal(err)
	}
	begin := index.BeginGenerationRequest{GenerationID: scopeGeneration, JobID: scopeJob, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Authorize: authorize, MaxManifestBytes: 8192}
	if err := idx.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	capture := index.CaptureWrite{GenerationID: scopeGeneration, JobID: scopeJob, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Authorize: authorize, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 8192}
	if err := idx.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: []index.FileClassification{{Path: "design.md", Kind: "document"}, {Path: "go.mod", Kind: "module"}, {Path: "main.go", Kind: "go"}, {Path: "other.go", Kind: "go"}, {Path: "vocabulary.json", Kind: "document"}}}); err != nil {
		t.Fatal(err)
	}
	if err := idx.StageSearchDocuments(ctx, index.StageSearchDocumentsRequest{Capture: capture, AuthorizeGeneration: authorize, Paths: []string{"main.go", "other.go"}, MaxBatchBytes: 8192}); err != nil {
		t.Fatal(err)
	}
	stream, err := idx.BeginProviderStage(ctx, capture, authorize)
	if err != nil {
		t.Fatal(err)
	}
	filePath, otherPath, pkg, symbol := "main.go", "other.go", "fixture", "target-main"
	moduleID, err := overview.GoModuleID(registration.WorktreeID, "go.mod")
	if err != nil {
		t.Fatal(err)
	}
	declaration, call := int64(strings.Index(string(source), "Example")), int64(strings.LastIndex(string(source), "Example"))
	relations := make([]model.RelationFact, 0, 12)
	for i := 0; i < 12; i++ {
		relations = append(relations, model.RelationFact{RelationID: fmt.Sprintf("call-%02d", i), SourceSymbolID: symbol, TargetSymbolID: &symbol, Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: filePath, Range: model.ByteRange{StartByte: call, EndByte: call + 7}})
	}
	batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: scopeJob, Sequence: 1, Packages: []model.PackageFact{{PackageKey: pkg, ImportPath: "example.invalid/fixture", ModuleKey: moduleID, Variant: "production", InputDigest: captured.Manifest().SnapshotDigest, SemanticStatus: "partial"}}, Symbols: []model.SymbolFact{{SymbolID: symbol, Path: &filePath, PackageKey: &pkg, Kind: "function", Name: "Example", QualifiedName: "fixture.Example", ScopeKey: pkg, Range: &model.ByteRange{StartByte: declaration, EndByte: declaration + 7}}, {SymbolID: "file-main", Path: &filePath, PackageKey: &pkg, Kind: "file", Name: "main.go", QualifiedName: "fixture.main.go", ScopeKey: pkg, Range: &model.ByteRange{StartByte: 0, EndByte: int64(len(source))}}, {SymbolID: "file-other", Path: &otherPath, PackageKey: &pkg, Kind: "file", Name: "other.go", QualifiedName: "fixture.other.go", ScopeKey: pkg, Range: &model.ByteRange{StartByte: 0, EndByte: int64(len(otherSource))}}}, Relations: relations, Diagnostics: []model.DiagnosticFact{}}
	stream, err = idx.StageProviderBatch(ctx, index.StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: scopeJob, Sequence: 2, Status: "partial", InputDigest: captured.Manifest().SnapshotDigest, Capabilities: []model.Capability{{Name: "go_semantics", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "FIXTURE_PARTIAL", Reason: "Synthetic provider coverage is partial.", SourceRefs: []model.SourceRef{}}}}
	if err := idx.SealProviderStage(ctx, index.SealProviderRequest{Capture: capture, Stream: stream, Done: done, AuthorizeGeneration: authorize, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	if err := idx.PublishProvider(ctx, index.PublishProviderRequest{Capture: capture, Stream: stream, AuthorizeGeneration: authorize, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }, VerifyProvider: func(context.Context, *snapshot.Snapshot, model.ProviderDone) error { return nil }, MaxFacts: 100, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	if err := idx.ReleaseProviderStage(ctx, capture, stream, authorize); err != nil {
		t.Fatal(err)
	}
	principal := app.CorePrincipal{ID: "fixture", Digest: strings.Repeat("d", 64), BindingDigest: strings.Repeat("e", 64)}
	denied := errors.New("fixture revoked")
	allowed := true
	access := func(_ context.Context, p app.CorePrincipal, worktree, profile string) error {
		if !allowed || p != principal || worktree != registration.WorktreeID || profile != registration.ProfileID {
			return denied
		}
		return nil
	}
	reader := &ScopeReader{Catalog: cat, Index: idx, Records: recordStore, WorktreeRoot: root, CommonRoot: common, WorktreeID: registration.WorktreeID, ProfileID: registration.ProfileID, Access: access, MaxHeads: 100, MaxManifestBytes: 8192}
	first, err := reader.Resolve(ctx, principal, "")
	if err != nil || first.Vector.Members[0].GenerationID != scopeGeneration || first.Records.RecordEpoch != 0 || first.Records.ApprovedHeads != 0 {
		t.Fatalf("initial scope: %+v %v", first, err)
	}
	if none, err := reader.NoApprovedRecords(ctx, principal, first.Vector); err != nil || !none {
		t.Fatalf("empty records proof: %v %v", none, err)
	}
	blockedPath := ""
	authority := &BoundCoreAuthority{Reader: reader, AuthenticatePrincipal: func(context.Context) (app.CorePrincipal, error) { return principal, nil }, AuthorizePath: func(_ context.Context, _ app.CorePrincipal, p model.PolicyConfig, path string) error {
		if path == blockedPath || len(p.AllowedRoots) == 0 || p.AllowedRoots[0] != repo || path != "main.go" && path != "other.go" && path != "go.mod" && path != "design.md" && path != "vocabulary.json" {
			return denied
		}
		p.AllowedRoots[0] = "mutated fixture copy"
		return nil
	}}
	pos := &model.PositionSelector{Kind: "source_position", Path: "main.go", ContentHash: contentHash, ByteOffset: 0}
	request := model.LocateRequest{Mode: "symbol", Position: pos, Limit: 1}
	if err := authority.AuthorizeRequest(ctx, principal, "mce4d_locate", request); err != nil {
		t.Fatalf("authorized request: %v", err)
	}
	pos.Path = ".env"
	if err := authority.AuthorizeRequest(ctx, principal, "mce4d_locate", request); !errors.Is(err, policy.ErrDeniedPath) {
		t.Fatalf("sensitive path accepted: %v", err)
	}
	pos.Path = "main.go"
	ref := model.SourceRef{WorktreeID: registration.WorktreeID, ProfileID: registration.ProfileID, GenerationID: scopeGeneration, Path: "main.go", ContentHash: contentHash, Range: model.ByteRange{StartByte: 0, EndByte: 1}}
	graphRef := &model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}, SymbolID: "fixture"}
	response := model.Response{Scope: &first.Vector, Data: model.LocateData{Operation: "locate", Candidates: []model.Candidate{{Ref: graphRef, Name: "fixture", QualifiedName: "fixture", SourceRefs: []model.SourceRef{ref}, EvidenceRefs: []model.EvidenceRef{}}}}}
	if err := authority.AuthorizeResponse(ctx, principal, "mce4d_locate", request, response); err != nil {
		t.Fatalf("authorized response: %v", err)
	}
	foreign := model.Response{Data: model.StatusData{RegisteredScopes: []model.RegisteredScopeStatus{{WorktreeID: scopeGeneration, RootStatus: "registered", IndexState: "missing"}}}}
	if err := authority.AuthorizeResponse(ctx, principal, "mce4d_workspace_status", model.StatusRequest{}, foreign); !errors.Is(err, ErrAuthority) {
		t.Fatalf("foreign status scope accepted: %v", err)
	}
	response.Data = model.LocateData{Operation: "locate", Candidates: []model.Candidate{{Ref: graphRef, SourceRefs: []model.SourceRef{{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID, Path: ".env", ContentHash: ref.ContentHash, Range: ref.Range}}}}}
	if err := authority.AuthorizeResponse(ctx, principal, "mce4d_locate", request, response); !errors.Is(err, policy.ErrDeniedPath) {
		t.Fatalf("sensitive response path accepted: %v", err)
	}
	descriptor := model.Response{Scope: &first.Vector, Status: "error", Error: &model.ErrorInfo{Code: "RESULT_TOO_LARGE", Details: []model.Gap{{Code: "EVIDENCE_ITEM_TOO_LARGE", SourceRefs: []model.SourceRef{ref}}}}}
	if err := authority.AuthorizeResponse(ctx, principal, "mce4d_context_pack", model.PackRequest{}, descriptor); err != nil {
		t.Fatalf("authorized oversized descriptor rejected: %v", err)
	}
	if err := authority.AuthorizeResponse(ctx, principal, "mce4d_locate", request, descriptor); !errors.Is(err, ErrAuthority) {
		t.Fatalf("descriptor accepted for another operation: %v", err)
	}
	blockedPath = "main.go"
	if err := authority.AuthorizeResponse(ctx, principal, "mce4d_context_pack", model.PackRequest{}, descriptor); !errors.Is(err, ErrAuthority) {
		t.Fatalf("descriptor bypassed source revocation: %v", err)
	}
	blockedPath = ""
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	configStore, err := runtimeadapter.NewRuntimeConfigStore(catalogRoot, "fixture-config.json")
	if err != nil {
		t.Fatal(err)
	}
	config, err := app.NewRuntimeConfigService(ctx, configStore, 4)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := app.NewFreshnessPackCache(codec, 32, 32*1024*1024, 900)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewCoreBackend(config, reader, authority, codec, cache, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := app.NewCoreService(config, authority, backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	options := app.CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "mcp_dual"}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	decode := func(name string, result app.CoreCallResult) model.Response {
		t.Helper()
		if len(result.Blocks) != 2 || string(result.Blocks[0]) != string(result.Blocks[1]) {
			t.Fatal("invalid dual delivery")
		}
		value, err := registry.DecodeResponse(contracts.MCP, name, result.Blocks[0], 16384, 4)
		if err != nil || model.ValidateDeliveredBytes(value.Budget, result.Blocks) != nil {
			t.Fatalf("wire/accounting: %v", err)
		}
		return value
	}
	indexDB := scopeDB(t, indexPath, 1)
	mutation, err := writer.HoldWriter(ctx, registration.WorktreeID, scopeJob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := indexDB.ExecContext(ctx, `DELETE FROM active_generations WHERE profile_id=?`, registration.ProfileID); err != nil {
		t.Fatal(err)
	}
	if err := mutation.Close(); err != nil {
		t.Fatal(err)
	}
	preIndex, err := service.Call(ctx, "mce4d_workspace_status", []byte(`{}`), options)
	if err != nil || preIndex.IsError {
		t.Fatalf("pre-index status failed: %v", err)
	}
	preIndexResponse := decode("mce4d_workspace_status", preIndex)
	preIndexData := preIndexResponse.Data.(model.StatusData)
	if preIndexResponse.Scope != nil || preIndexData.IndexState != "missing" || len(preIndexData.Scopes) != 0 || len(preIndexData.RegisteredScopes) != 1 || preIndexData.RegisteredScopes[0].WorktreeID != registration.WorktreeID || preIndexData.RegisteredScopes[0].IndexState != "missing" {
		t.Fatalf("pre-index status invented a scope: %+v", preIndexData)
	}
	preIndexVerify, err := service.Call(ctx, "mce4d_workspace_status", []byte(`{"verify":"workspace"}`), options)
	if err != nil || preIndexVerify.IsError {
		t.Fatalf("pre-index verified status failed: %v", err)
	}
	preIndexUnknown := decode("mce4d_workspace_status", preIndexVerify)
	if preIndexUnknown.Scope != nil || preIndexUnknown.Status != "partial" || preIndexUnknown.Freshness.Status != "unknown" || preIndexUnknown.Freshness.Scope != "none" || preIndexUnknown.Data.(model.StatusData).IndexState != "missing" {
		t.Fatal("missing index falsely claimed workspace verification")
	}
	missingLocate, err := service.Call(ctx, "mce4d_locate", []byte(`{"query":"Example"}`), options)
	if err != nil || !missingLocate.IsError || decode("mce4d_locate", missingLocate).Error.Code != "INDEX_MISSING" {
		t.Fatalf("missing index was not explicit: %v", err)
	}
	blocks, err := backend.WorkspaceStatus(ctx, principal, model.StatusRequest{}, options)
	if err != nil {
		t.Fatal(err)
	}
	prepared := decode("mce4d_workspace_status", app.CoreCallResult{Blocks: blocks})
	mutation, err = writer.HoldWriter(ctx, registration.WorktreeID, scopeJob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := indexDB.ExecContext(ctx, `INSERT INTO active_generations(profile_id,generation_id) VALUES(?,?)`, registration.ProfileID, scopeGeneration); err != nil {
		t.Fatal(err)
	}
	if err := mutation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := authority.AuthorizeResponse(ctx, principal, "mce4d_workspace_status", model.StatusRequest{}, prepared); err == nil {
		t.Fatal("status stayed missing after publication")
	}
	if err := backend.VerifyDelivery(ctx, principal, "mce4d_workspace_status", model.StatusRequest{}, prepared); err == nil {
		t.Fatal("stale missing status passed final delivery")
	}
	status, err := service.Call(ctx, "mce4d_workspace_status", []byte(`{}`), options)
	if err != nil || status.IsError || decode("mce4d_workspace_status", status).Data.(model.StatusData).IndexState != "published" {
		t.Fatalf("wired status: %v", err)
	}
	withoutVerifier, err := service.Call(ctx, "mce4d_workspace_status", []byte(`{"verify":"workspace"}`), options)
	if err != nil || !withoutVerifier.IsError || decode("mce4d_workspace_status", withoutVerifier).Error.Code != "FRESHNESS_UNKNOWN" {
		t.Fatalf("workspace verification without an owner was accepted: %v", err)
	}
	filtered, err := service.Call(ctx, "mce4d_locate", []byte(`{"query":"Example","kinds":["function"],"package":"example.invalid/fixture"}`), options)
	if err != nil || filtered.IsError {
		t.Fatalf("wired filtered locate: %v", err)
	}
	filteredData := decode("mce4d_locate", filtered).Data.(model.LocateData)
	if len(filteredData.Candidates) != 1 || filteredData.Candidates[0].Ref.(*model.SymbolRef).SymbolID != "target-main" {
		t.Fatalf("wired filters lost the matching symbol: %+v", filteredData)
	}
	filtered, err = service.Call(ctx, "mce4d_locate", []byte(`{"query":"Example","kinds":["function"],"package":"fixture/other"}`), options)
	if err != nil || filtered.IsError || len(decode("mce4d_locate", filtered).Data.(model.LocateData).Candidates) != 0 {
		t.Fatalf("wired package filter returned a nonmatching symbol: %v", err)
	}
	graph, err := idx.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: first.Vector.Members[0], VectorDigest: first.Vector.VectorDigest, AuthorizeGeneration: func(ctx context.Context, worktree, profile, digest string, epoch int64) error {
		if digest != first.Binding.PolicyDigest || epoch != first.Binding.PolicyEpoch {
			return ErrScopeChanged
		}
		return access(ctx, principal, worktree, profile)
	}, AuthorizePath: func(ctx context.Context, worktree, name, digest string, epoch int64) error {
		if worktree != registration.WorktreeID || digest != first.Binding.PolicyDigest || epoch != first.Binding.PolicyEpoch {
			return ErrScopeChanged
		}
		return authority.path(ctx, principal, first.Binding.Policy, name)
	}, Scanner: scanner, MaxManifestBytes: 8192, MaxFileBytes: 1024, MaxSourceBytes: 8192, MaxFactBytes: 65536, MaxFacts: 100})
	if err != nil {
		t.Fatal(err)
	}
	incompleteGraph := *graph
	incompleteGraph.Symbols = []model.SymbolFact{}
	for _, fact := range graph.Symbols {
		if fact.SymbolID != "file-other" {
			incompleteGraph.Symbols = append(incompleteGraph.Symbols, fact)
		}
	}
	if packages, err := evidencedOverviewPackages(ctx, &incompleteGraph); !errors.Is(err, contextpack.ErrContinuationUnavailable) || packages != nil {
		t.Fatalf("missing package membership was inferred: %+v %v", packages, err)
	}
	report, err := backend.buildOverview(ctx, principal, first.Vector, graph, scanner, 8192)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil || len(summary.Modules) != 1 || summary.Modules[0].ImportPath != "example.invalid/fixture" || len(summary.Modules[0].Packages) != 1 || len(summary.Modules[0].Packages[0].Files) != 2 || summary.Modules[0].Packages[0].Files[0] != "main.go" || summary.Modules[0].Packages[0].Files[1] != "other.go" {
		t.Fatalf("overview lost evidenced module membership: %+v %v", summary, err)
	}
	planRequest, err := model.DecodePackRequestJSON([]byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","intent":"Review the design"}`), 8192)
	if err != nil {
		t.Fatal(err)
	}
	boundary := int64(strings.Index(string(documentSource), "Constraint:"))
	declRef := model.SourceRef{WorktreeID: registration.WorktreeID, ProfileID: registration.ProfileID, GenerationID: scopeGeneration, Path: "design.md", ContentHash: documentHash, Range: model.ByteRange{StartByte: 0, EndByte: boundary}}
	constraintRef := declRef
	constraintRef.Range = model.ByteRange{StartByte: boundary, EndByte: int64(len(documentSource))}
	documentsAllowed := true
	plan, err := backend.buildPlan(ctx, principal, first.Vector, graph, scanner, 8192, planRequest, []overview.DocumentDeclaration{{Kind: "objective", Source: declRef}, {Kind: "constraint", Source: constraintRef}}, func(context.Context, overview.Scope, overview.DocumentDeclaration) error {
		if !documentsAllowed {
			return denied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	planReport, err := plan.Read(ctx)
	if err != nil || planReport.ProjectStage != "mixed" || len(planReport.Items) != 6 {
		t.Fatalf("plan did not combine intended documents and observed module: %+v %v", planReport, err)
	}
	for _, item := range planReport.Items {
		observed := item.Kind == "module" || item.Kind == "structure_module" || item.Kind == "structure_file"
		if observed && item.Plane != "observed_static" || !observed && (item.Plane != "intended" || item.MappingStatus != "unmapped") {
			t.Fatalf("plan mixed intended and observed evidence: %+v", item)
		}
	}
	documentsAllowed = false
	if leaked, err := plan.Read(ctx); err == nil || len(leaked.Items) != 0 {
		t.Fatalf("revoked document leaked through plan: %+v %v", leaked, err)
	}
	documentsAllowed = true
	planRaw, err := json.Marshal(planRequest)
	if err != nil {
		t.Fatal(err)
	}
	planDigest, err := model.RequestDigest("context_pack", principal.ID, "PackRequest", planRequest, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	planBinding := contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: principal.Digest, BindingDigest: principal.BindingDigest, RequestDigest: planDigest, OrderVersion: "pack-v1", Scope: first.Vector}
	planGuard := backend.packGuard(principal)
	planValidation := app.ValidateCapturedInput{Scope: first.Vector, MaxInputBytes: 65536, MaxManifestBytes: 8192, CurrentScope: func(ctx context.Context) (model.ScopeVector, error) {
		value, err := reader.Recheck(ctx, principal, first.Vector)
		return value.Vector, err
	}, ResolveCapture: func(ctx context.Context, member model.ScopeMember) (*snapshot.Snapshot, error) {
		if member.GenerationID != scopeGeneration {
			return nil, ErrScopeChanged
		}
		if _, err := reader.Recheck(ctx, principal, first.Vector); err != nil {
			return nil, err
		}
		return graph.Snapshot, nil
	}, AuthorizeSource: func(ctx context.Context, _ model.ScopeMember, ref model.SourceRef) error {
		return authority.path(ctx, principal, first.Binding.Policy, ref.Path)
	}}
	planInput := app.FreshnessPlanInput{Plan: plan, Codec: codec, Binding: planBinding, Guard: planGuard, RequestJSON: planRaw, PrincipalID: principal.ID, Coverage: graph.Coverage, Scanner: scanner, Validation: planValidation, RequestLimit: 65536}
	secretInput := planInput
	secretInput.Coverage = model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{{Code: "FIXTURE", Reason: `api_key="TESTONLY` + strings.Repeat("X", 16) + `"`, SourceRefs: []model.SourceRef{}}}, ScopeDescription: "fixture"}
	if value, token, err := app.NewFreshnessPlan(ctx, secretInput); !errors.Is(err, policy.ErrSecretBlocked) || value != nil || token != "" {
		t.Fatalf("sensitive coverage was returned: %v", err)
	}
	deniedInput := planInput
	deniedInput.Coverage = model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{{Code: "FIXTURE", Reason: "fixture", SourceRefs: []model.SourceRef{ref}}}, ScopeDescription: "fixture"}
	deniedInput.Validation.AuthorizeSource = func(context.Context, model.ScopeMember, model.SourceRef) error { return denied }
	if value, token, err := app.NewFreshnessPlan(ctx, deniedInput); !errors.Is(err, app.ErrValidationAuthority) || value != nil || token != "" {
		t.Fatalf("unauthorized coverage reference was returned: %v", err)
	}
	planPack, planToken, err := app.NewFreshnessPlan(ctx, planInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.AdmitRetained(ctx, planPack, planToken, principal.Digest, planGuard, 65536); err != nil {
		t.Fatal(err)
	}
	if retained, err := cache.GetRetained(ctx, planToken, principal.Digest, planGuard); err != nil || retained != planPack {
		t.Fatalf("plan retention: %v", err)
	}
	planPage, err := planPack.First(ctx, packPageOptions(options))
	if err != nil {
		t.Fatal(err)
	}
	planWire := decode("mce4d_context_pack", app.CoreCallResult{Blocks: planPage.Blocks})
	if planWire.Freshness.Status != "not_checked" || planWire.Readiness != "needs_context" || planWire.Pagination.NextCursor == nil {
		t.Fatal("plan first page lost snapshot or continuation")
	}
	if page, err := planPack.Continue(ctx, planToken, *planWire.Pagination.NextCursor, strings.Repeat("f", 64), planGuard, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
		t.Fatal("another principal continued the plan")
	}
	invalidCursor := *planWire.Pagination.NextCursor + "x"
	if page, err := planPack.Continue(ctx, planToken, invalidCursor, principal.Digest, planGuard, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
		t.Fatal("tampered plan cursor was accepted")
	}
	documentsAllowed = false
	if page, err := planPack.Continue(ctx, planToken, *planWire.Pagination.NextCursor, principal.Digest, planGuard, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
		t.Fatal("revoked plan continuation leaked")
	}
	documentsAllowed = true
	seenPlanItems := map[string]bool{}
	wantPlanContent := map[string]string{}
	for _, item := range planReport.Items {
		wantPlanContent[item.ID] = item.Content
	}
	var summaryID string
	for pages := 0; ; pages++ {
		for _, item := range planWire.Data.(model.PackData).Items {
			if item.WhyIncluded == "plan_summary" {
				if summaryID != "" && summaryID != item.ItemID {
					t.Fatal("plan metadata ID changed between pages")
				}
				summaryID = item.ItemID
				continue
			}
			if seenPlanItems[item.ItemID] || len(item.SourceRefs) != 1 || item.Content == nil || wantPlanContent[item.ItemID] != *item.Content {
				t.Fatal("plan evidence was duplicated or unsourced")
			}
			seenPlanItems[item.ItemID] = true
		}
		if planWire.Pagination.NextCursor == nil {
			break
		}
		if pages >= 20 {
			t.Fatal("plan cursor failed to terminate")
		}
		planPage, err = planPack.Continue(ctx, planToken, *planWire.Pagination.NextCursor, principal.Digest, planGuard, packPageOptions(options))
		if err != nil {
			t.Fatal(err)
		}
		planWire = decode("mce4d_expand_pack", app.CoreCallResult{Blocks: planPage.Blocks})
	}
	if len(seenPlanItems) != 6 {
		t.Fatalf("missing plan evidence: %d", len(seenPlanItems))
	}
	manyGaps := planInput
	manyGaps.Scanner, err = policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	manyGaps.Coverage = model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}
	for i := 0; i < 20; i++ {
		manyGaps.Coverage.Gaps = append(manyGaps.Coverage.Gaps, model.Gap{Code: fmt.Sprintf("FIXTURE_%02d", i), Reason: strings.Repeat("x", 600), SourceRefs: []model.SourceRef{}})
	}
	gapPlan, gapToken, err := app.NewFreshnessPlan(ctx, manyGaps)
	if err != nil {
		t.Fatal(err)
	}
	gapPage, err := gapPlan.First(ctx, packPageOptions(options))
	if err != nil {
		t.Fatalf("many gaps blocked first page: %v", err)
	}
	gapResponse := decode("mce4d_context_pack", app.CoreCallResult{Blocks: gapPage.Blocks})
	seenGaps := map[string]bool{}
	grouped := false
	for pages := 0; ; pages++ {
		for _, gap := range gapResponse.Coverage.Gaps {
			if gap.Code == "GAP_DETAILS_PENDING" {
				if gap.Count == nil || *gap.Count < 1 {
					t.Fatal("grouped safety details lost their pending count")
				}
				grouped = true
				continue
			}
			if seenGaps[gap.Code] {
				t.Fatalf("gap was repeated across pages: %s", gap.Code)
			}
			seenGaps[gap.Code] = true
		}
		if gapResponse.Pagination.NextCursor == nil {
			break
		}
		if pages >= 30 {
			t.Fatal("safety detail cursor failed to terminate")
		}
		gapPage, err = gapPlan.Continue(ctx, gapToken, *gapResponse.Pagination.NextCursor, principal.Digest, planGuard, packPageOptions(options))
		if err != nil {
			t.Fatalf("many gaps continuation: %v", err)
		}
		gapResponse = decode("mce4d_expand_pack", app.CoreCallResult{Blocks: gapPage.Blocks})
	}
	if !grouped || len(seenGaps) != len(manyGaps.Coverage.Gaps) {
		t.Fatalf("safety details were lost: grouped=%v seen=%d", grouped, len(seenGaps))
	}
	if validation, err := planPack.Validate(ctx, planToken, principal.Digest, planGuard); !errors.Is(err, app.ErrFreshnessUnknown) || validation.Data.EditPreconditionValidated {
		t.Fatalf("plan accepted missing workspace proof: %v", err)
	}
	publicRequest := []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","intent":"Review the design","max_output_bytes":16384}`)
	withoutProvider, err := service.Call(ctx, "mce4d_context_pack", publicRequest, options)
	if err != nil || !withoutProvider.IsError || decode("mce4d_context_pack", withoutProvider).Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("plan ran without a declaration authority: %v", err)
	}
	wholeRule := declRef
	wholeRule.Range.EndByte = int64(len(documentSource))
	vocabularyRule := model.SourceRef{WorktreeID: registration.WorktreeID, ProfileID: registration.ProfileID, GenerationID: scopeGeneration, Path: "vocabulary.json", ContentHash: vocabularyHash, Range: model.ByteRange{StartByte: 0, EndByte: int64(len(vocabularySource))}}
	registryCurrent, wholeRuleAvailable := true, false
	registryAuthority, registryRevision := "fixture-registry", "revision-1"
	declarationProvider := func(_ context.Context, actor app.CorePrincipal, scope model.ScopeVector, request model.PackRequest) (PlanDeclarations, error) {
		if actor != principal || len(scope.Members) != 1 || scope.Members[0].GenerationID != scopeGeneration || request.TaskKind != "plan" && request.TaskKind != "explore" {
			return PlanDeclarations{}, ErrAuthority
		}
		items := []overview.DocumentDeclaration{{Kind: "objective", Source: declRef}, {Kind: "constraint", Source: constraintRef}}
		if wholeRuleAvailable {
			items = append(items, overview.DocumentDeclaration{Kind: "constraint", Source: wholeRule}, overview.DocumentDeclaration{Kind: "constraint", Source: vocabularyRule})
		}
		capturedAuthority, capturedRevision := registryAuthority, registryRevision
		digest, err := DeclarationRevisionDigest(capturedAuthority, capturedRevision)
		if err != nil {
			return PlanDeclarations{}, err
		}
		return PlanDeclarations{Items: items, RetainedBytes: 4096, RevisionDigest: digest, Recheck: func(context.Context) error {
			if !registryCurrent || registryAuthority != capturedAuthority || registryRevision != capturedRevision {
				return denied
			}
			return nil
		}, AuthorizeDocument: func(_ context.Context, _ overview.Scope, declaration overview.DocumentDeclaration) error {
			if !documentsAllowed {
				return denied
			}
			for _, allowed := range items {
				if declaration == allowed {
					return nil
				}
			}
			return denied
		}}, nil
	}
	planBackend, err := NewCoreBackendWithPlan(config, reader, authority, codec, cache, nil, declarationProvider)
	if err != nil {
		t.Fatal(err)
	}
	planService, err := app.NewCoreService(config, authority, planBackend, nil)
	if err != nil {
		t.Fatal(err)
	}
	missingRule, err := planService.Call(ctx, "mce4d_context_pack", publicRequest, options)
	if err != nil || !missingRule.IsError || decode("mce4d_context_pack", missingRule).Error.Code != "CONTEXT_INCOMPLETE" {
		t.Fatalf("plan omitted a trusted rule: %v", err)
	}
	wholeRuleAvailable = true
	publicResult, err := planService.Call(ctx, "mce4d_context_pack", publicRequest, options)
	if err != nil || publicResult.IsError {
		t.Fatalf("wired plan: %v %s", err, publicResult.Blocks[0])
	}
	publicPage := decode("mce4d_context_pack", publicResult)
	publicData := publicPage.Data.(model.PackData)
	if publicPage.Pagination.NextCursor == nil || publicData.PackToken == "" || publicPage.Freshness.Status != "not_checked" {
		t.Fatal("wired plan lost its continuation or freshness")
	}
	retainedPlanCursor := *publicPage.Pagination.NextCursor
	var retainedPlan *app.RetainedPlanPack
	t.Run("retained_cli_plan_across_services", func(t *testing.T) {
		t.Cleanup(func() {
			registryCurrent, documentsAllowed, wholeRuleAvailable = true, true, true
			registryAuthority, registryRevision = "fixture-registry", "revision-1"
		})
		if err := os.MkdirAll(filepath.Join(worktreeDir, "scratch", "pack"), 0700); err != nil {
			t.Fatal(err)
		}
		store, err := packstore.New(worktreeRoot, []*paths.Root{root, catalogRoot}, shared, registration.WorktreeID, 16*1024*1024, 32*1024*1024, 32,
			func(ctx context.Context) error {
				_, err := reader.Recheck(ctx, principal, first.Vector)
				return err
			}, func(context.Context, int64) (packstore.BudgetReservation, error) {
				return scopeBudgetReservation{}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		trackedStore := &trackedFixturePackStore{Store: store, t: t, owner: worktreeRoot, protected: []*paths.Root{root, catalogRoot}, directory: filepath.Join(worktreeDir, "scratch", "pack")}
		// Exercise the product default query timeout, without a test override.
		planConfig := config
		opened, released := 0, 0
		run := func(provider PlanDeclarationProvider, action string, raw []byte) (model.Response, int) {
			t.Helper()
			open := func(_ context.Context, configPath, worktree, profile string) (*app.CoreService, func() error, error) {
				if configPath != "" || worktree != "" || profile != "" {
					return nil, nil, cli.ErrBindingDenied
				}
				freshCache, err := app.NewFreshnessPackCache(codec, 32, 32*1024*1024, 900)
				if err != nil {
					return nil, nil, err
				}
				freshBackend, err := NewCoreBackendWithPlan(planConfig, reader, authority, codec, freshCache, nil, provider)
				if err == nil {
					freshBackend, err = freshBackend.WithCLIPackRetention(trackedStore)
				}
				if err != nil {
					return nil, nil, err
				}
				service, err := app.NewCoreService(planConfig, authority, freshBackend, nil)
				if err != nil {
					return nil, nil, err
				}
				opened++
				return service, func() error { released++; return nil }, nil
			}
			var out, errOut bytes.Buffer
			code := cli.Run(ctx, []string{"query", action, "--input-json", "-"}, io.NopCloser(bytes.NewReader(raw)), &out, &errOut, cli.Dependencies{CPUs: 4, OpenRetainedCore: open})
			if errOut.Len() != 0 {
				t.Fatalf("CLI %s diagnostic: %s", action, errOut.String())
			}
			name := map[string]string{"context": "mce4d_context_pack", "expand": "mce4d_expand_pack", "validate": "mce4d_validate_pack"}[action]
			response, err := registry.DecodeResponse(contracts.MCP, name, bytes.TrimSpace(out.Bytes()), 131072, 4)
			if err != nil {
				t.Fatalf("CLI %s response: %v", action, err)
			}
			return response, code
		}
		request := []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","intent":"Review the design","max_output_bytes":8192}`)
		unproved := func(ctx context.Context, actor app.CorePrincipal, scope model.ScopeVector, request model.PackRequest) (PlanDeclarations, error) {
			declarations, err := declarationProvider(ctx, actor, scope, request)
			declarations.RevisionDigest = ""
			return declarations, err
		}
		denied, code := run(unproved, "context", request)
		if code == 0 || code == 6 || denied.Data != nil || denied.Error == nil {
			t.Fatal("plan without revision proof returned a token")
		}
		initial, code := run(declarationProvider, "context", request)
		data, ok := initial.Data.(model.PackData)
		if code != 0 && code != 6 || !ok || initial.Pagination.NextCursor == nil || data.PackToken == "" {
			t.Fatalf("retained plan initial response: exit=%d error=%+v", code, initial.Error)
		}
		claims, err := codec.VerifyPrincipal(ctx, data.PackToken, "pack", principal.Digest, planBackend.packGuard(principal))
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(worktreeDir, "scratch", "pack", claims.Binding.ResultID+".pack")
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("retained plan file unavailable")
		}
		expand := scopeJSON(t, model.ExpandRequest{PackToken: data.PackToken, NextCursor: *initial.Pagination.NextCursor, MaxOutputBytes: int64Pointer(8192)})
		next, code := run(declarationProvider, "expand", expand)
		nextData, nextOK := next.Data.(model.PackData)
		if code != 0 && code != 6 || !nextOK || nextData.PackToken != data.PackToken || opened < 3 || opened != released {
			t.Fatalf("retained plan continuation: exit=%d error=%+v", code, next.Error)
		}
		validation, code := run(declarationProvider, "validate", scopeJSON(t, model.ValidateRequest{PackToken: &data.PackToken}))
		if code != 3 || validation.Error == nil || validation.Error.Code != "FRESHNESS_UNKNOWN" {
			t.Fatal("retained plan accepted missing workspace verification")
		}
		registryRevision = "revision-2"
		changed, code := run(declarationProvider, "expand", expand)
		if code == 0 || code == 6 || changed.Data != nil || changed.Error == nil {
			t.Fatal("plan survived a declaration authority revision change")
		}
		registryRevision = "revision-1"
		original, err := os.ReadFile(file)
		if err != nil || len(original) < 2 {
			t.Fatal("retained plan could not be read for the fixture")
		}
		tampered := bytes.Clone(original)
		tampered[len(tampered)/2] ^= 1
		if err := os.WriteFile(file, tampered, 0600); err != nil {
			t.Fatal(err)
		}
		changed, code = run(declarationProvider, "expand", expand)
		if code == 0 || code == 6 || changed.Data != nil || changed.Error == nil {
			t.Fatal("tampered plan returned data")
		}
		if err := os.WriteFile(file, original, 0600); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("restore_pinned_plan_with_current_declarations", func(t *testing.T) {
		t.Cleanup(func() {
			registryCurrent, documentsAllowed, wholeRuleAvailable = true, true, true
			registryAuthority, registryRevision = "fixture-registry", "revision-1"
		})
		guard := planBackend.packGuard(principal)
		retained, err := cache.GetRetained(ctx, publicData.PackToken, principal.Digest, guard)
		if err != nil {
			t.Fatal(err)
		}
		original, ok := retained.(*app.FreshnessPlan)
		if !ok {
			t.Fatal("cached result is not the original plan")
		}
		if err := os.MkdirAll(filepath.Join(worktreeDir, "scratch", "pack"), 0700); err != nil {
			t.Fatal(err)
		}
		store, err := packstore.New(worktreeRoot, []*paths.Root{root, catalogRoot}, shared, registration.WorktreeID, 16*1024*1024, 32*1024*1024, 4,
			func(ctx context.Context) error {
				_, err := reader.Recheck(ctx, principal, first.Vector)
				return err
			}, func(context.Context, int64) (packstore.BudgetReservation, error) {
				return scopeBudgetReservation{}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		factory, err := planBackend.PlanRestoreFactory(principal)
		if err != nil {
			t.Fatal(err)
		}
		retention, err := app.NewPlanPackRetention(store, codec, 16*1024*1024, 900, factory)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := retention.Save(ctx, original, principal.Digest, principal.ID, publicRequest, guard)
		if err != nil || handle.Token() != publicData.PackToken {
			t.Fatalf("persist plan with catalog authority: %v", err)
		}
		retainedPlan = handle
		want, err := original.Continue(ctx, publicData.PackToken, *publicPage.Pagination.NextCursor, principal.Digest, guard, packPageOptions(options))
		if err != nil {
			t.Fatal(err)
		}
		got, err := handle.Continue(ctx, *publicPage.Pagination.NextCursor, packPageOptions(options))
		if err != nil || !reflect.DeepEqual(got.Response.Data, want.Response.Data) {
			t.Fatalf("restored catalog plan differs: %v", err)
		}
		registryCurrent = false
		if page, err := handle.Continue(ctx, *publicPage.Pagination.NextCursor, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
			t.Fatal("changed declaration revision returned plan data")
		}
		registryCurrent = true
		registryRevision = "revision-2"
		if page, err := handle.Continue(ctx, *publicPage.Pagination.NextCursor, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
			t.Fatal("changed declaration revision returned plan data")
		}
		if result, err := retention.Get(ctx, publicData.PackToken, principal.Digest, principal.ID, guard); err == nil || result != nil {
			t.Fatal("new owner accepted unchanged declarations at a different revision")
		}
		registryRevision = "revision-1"
		registryAuthority = "other-registry"
		if page, err := handle.Continue(ctx, *publicPage.Pagination.NextCursor, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
			t.Fatal("changed declaration authority returned plan data")
		}
		if result, err := retention.Get(ctx, publicData.PackToken, principal.Digest, principal.ID, guard); err == nil || result != nil {
			t.Fatal("new authority accepted unchanged declarations at the same revision")
		}
		registryAuthority = "fixture-registry"
		documentsAllowed = false
		if page, err := handle.Continue(ctx, *publicPage.Pagination.NextCursor, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
			t.Fatal("revoked document authority returned plan data")
		}
		documentsAllowed = true
		wholeRuleAvailable = false
		if result, err := retention.Get(ctx, publicData.PackToken, principal.Digest, principal.ID, guard); err == nil || result != nil {
			t.Fatal("changed complete declaration set restored an old plan")
		}
		wholeRuleAvailable = true
	})
	actualRequest, err := model.DecodePackRequestJSON(publicRequest, 65536)
	if err != nil {
		t.Fatal(err)
	}
	wrongRequest := actualRequest
	wrongRequest.Intent = stringPointer("A different task")
	if err := planBackend.VerifyDelivery(ctx, principal, "mce4d_context_pack", wrongRequest, publicPage); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("final delivery accepted a different initial request: %v", err)
	}
	repeatedPage := publicPage
	repeatedData := publicData
	repeatedData.Operation = "expand_pack"
	repeatedPage.Data = repeatedData
	repeatedRequest := model.ExpandRequest{PackToken: publicData.PackToken, NextCursor: *publicPage.Pagination.NextCursor}
	if err := planBackend.VerifyDelivery(ctx, principal, "mce4d_expand_pack", repeatedRequest, repeatedPage); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("final delivery accepted a repeating continuation: %v", err)
	}
	publicExpandRaw := scopeJSON(t, model.ExpandRequest{PackToken: publicData.PackToken, NextCursor: *publicPage.Pagination.NextCursor, MaxOutputBytes: int64Pointer(16384)})
	documentsAllowed = false
	revoked, err := planService.Call(ctx, "mce4d_expand_pack", publicExpandRaw, options)
	if err != nil || !revoked.IsError || decode("mce4d_expand_pack", revoked).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("wired plan returned a revoked document: %v %+v", err, revoked)
	}
	documentsAllowed = true
	registryCurrent = false
	revoked, err = planService.Call(ctx, "mce4d_expand_pack", publicExpandRaw, options)
	if err != nil || !revoked.IsError || decode("mce4d_expand_pack", revoked).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("changed declaration revision was accepted: %v %+v", err, revoked)
	}
	registryCurrent = true
	publicItems := 0
	moduleSeen := false
	structureFiles := map[string]bool{}
	for pages := 0; ; pages++ {
		for _, item := range publicPage.Data.(model.PackData).Items {
			if item.WhyIncluded != "plan_summary" {
				publicItems++
			}
			if item.WhyIncluded == "observed_module_structure" {
				var value overview.PlanModuleStructure
				if item.Content == nil || json.Unmarshal([]byte(*item.Content), &value) != nil || item.Kind != "overview" || len(item.SourceRefs) != 1 || value.Module.ModuleID != moduleID || value.ImportPath != "example.invalid/fixture" || value.ModuleSource != item.SourceRefs[0] {
					t.Fatal("module hierarchy lost its source-backed identity")
				}
				moduleSeen = true
			}
			if item.WhyIncluded == "observed_file_membership" {
				var value overview.PlanFileStructure
				if item.Content == nil || json.Unmarshal([]byte(*item.Content), &value) != nil || item.Kind != "overview" || len(item.SourceRefs) != 1 || value.Module.ModuleID != moduleID || value.Package.PackageKey != pkg || value.FileSource != item.SourceRefs[0] || structureFiles[value.FileSource.Path] {
					t.Fatal("file membership lost its source-backed identity")
				}
				structureFiles[value.FileSource.Path] = true
			}
		}
		if publicPage.Pagination.NextCursor == nil {
			break
		}
		if pages >= 20 {
			t.Fatal("wired plan continuation did not terminate")
		}
		raw := scopeJSON(t, model.ExpandRequest{PackToken: publicData.PackToken, NextCursor: *publicPage.Pagination.NextCursor, MaxOutputBytes: int64Pointer(16384)})
		next, err := planService.Call(ctx, "mce4d_expand_pack", raw, options)
		if err != nil || next.IsError {
			t.Fatalf("wired plan continuation: %v %s", err, next.Blocks[0])
		}
		publicPage = decode("mce4d_expand_pack", next)
	}
	if publicItems != 8 || !moduleSeen || len(structureFiles) != 2 || !structureFiles["main.go"] || !structureFiles["other.go"] {
		t.Fatalf("wired plan omitted or duplicated evidence: %d", publicItems)
	}
	moduleRequest := scopeJSON(t, model.PackRequest{Selector: &model.ModuleSelector{Kind: "module", ModuleID: moduleID}, TaskKind: "plan", FreshnessMode: "analysis_snapshot", MaxOutputBytes: int64Pointer(16384)})
	moduleResult, err := planService.Call(ctx, "mce4d_context_pack", moduleRequest, options)
	if err != nil || moduleResult.IsError {
		t.Fatalf("module plan failed: %v %+v", err, moduleResult)
	}
	moduleTargets := decode("mce4d_context_pack", moduleResult).Data.(model.PackData).TargetRefs
	if len(moduleTargets) != 1 || moduleTargets[0].(*model.ModuleRef).ModuleID != moduleID {
		t.Fatal("module plan lost its resolved target")
	}
	documentRequest := scopeJSON(t, model.PackRequest{Selector: &model.DocumentSelector{Kind: "document_anchor", Path: declRef.Path, ContentHash: declRef.ContentHash, Range: declRef.Range}, TaskKind: "plan", FreshnessMode: "analysis_snapshot", MaxOutputBytes: int64Pointer(16384)})
	documentResult, err := planService.Call(ctx, "mce4d_context_pack", documentRequest, options)
	if err != nil || documentResult.IsError {
		t.Fatalf("document plan failed: %v %+v", err, documentResult)
	}
	documentPage := decode("mce4d_context_pack", documentResult)
	documentData := documentPage.Data.(model.PackData)
	if len(documentData.TargetRefs) != 1 || documentData.TargetRefs[0].(*model.FileRef).Path != declRef.Path {
		t.Fatal("document plan lost its selected file")
	}
	documentItems := map[model.SourceRef]bool{}
	for pages := 0; ; pages++ {
		for _, item := range documentPage.Data.(model.PackData).Items {
			if item.WhyIncluded != "plan_summary" && len(item.SourceRefs) == 1 {
				documentItems[item.SourceRefs[0]] = true
			}
		}
		if documentPage.Pagination.NextCursor == nil {
			break
		}
		if pages >= 10 {
			t.Fatal("document plan did not terminate")
		}
		raw := scopeJSON(t, model.ExpandRequest{PackToken: documentData.PackToken, NextCursor: *documentPage.Pagination.NextCursor, MaxOutputBytes: int64Pointer(16384)})
		next, err := planService.Call(ctx, "mce4d_expand_pack", raw, options)
		if err != nil || next.IsError {
			t.Fatalf("document plan continuation failed: %v %+v", err, next)
		}
		documentPage = decode("mce4d_expand_pack", next)
	}
	if len(documentItems) != 3 || !documentItems[declRef] || !documentItems[wholeRule] || !documentItems[vocabularyRule] {
		t.Fatal("document plan omitted its selected anchor or trusted rule")
	}
	exploreRequest := []byte(`{"selector":{"kind":"workspace"},"task_kind":"explore","max_output_bytes":16384}`)
	exploreResult, err := planService.Call(ctx, "mce4d_context_pack", exploreRequest, options)
	if err != nil || exploreResult.IsError {
		t.Fatalf("workspace overview failed: %v %+v", err, exploreResult)
	}
	explorePage := decode("mce4d_context_pack", exploreResult)
	exploreItems := explorePage.Data.(model.PackData).Items
	if explorePage.Freshness.Status != "not_checked" || explorePage.Pagination.NextCursor == nil || len(exploreItems) == 0 || exploreItems[0].WhyIncluded != "overview_summary" {
		t.Fatal("workspace overview lost its summary or continuation")
	}
	exploreTyped, err := model.DecodePackRequestJSON(exploreRequest, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if err := planBackend.VerifyDelivery(ctx, principal, "mce4d_context_pack", exploreTyped, publicPage); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("plan token was accepted for an explore request: %v", err)
	}
	withoutTopic, err := service.Call(ctx, "mce4d_locate", []byte(`{"mode":"topic","query":"objective"}`), options)
	if err != nil || !withoutTopic.IsError || decode("mce4d_locate", withoutTopic).Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("topic ran without explicit config authority: %v", err)
	}
	topicApproved, aliasAllowed, topicPath := true, true, "vocabulary.json"
	topicProvider := func(_ context.Context, actor app.CorePrincipal, scope model.ScopeVector) (TopicConfig, error) {
		if actor != principal || len(scope.Members) != 1 || scope.Members[0].GenerationID != scopeGeneration {
			return TopicConfig{}, ErrAuthority
		}
		selected := topicPath
		return TopicConfig{Path: selected, Recheck: func(context.Context) error {
			if !topicApproved || topicPath != selected {
				return denied
			}
			return nil
		}, AuthorizeConfig: func(_ context.Context, _ overview.Scope, name string) error {
			if name != selected {
				return denied
			}
			return nil
		}, AuthorizeAlias: func(_ context.Context, _ overview.Scope, declaration overview.AliasDeclaration) error {
			if !aliasAllowed || declaration.Alias != "Objective" || declaration.Source.Path != "design.md" || declaration.Source.Range != (model.ByteRange{StartByte: 0, EndByte: 9}) || declaration.Entity.EntityID != "goal" && declaration.Entity.EntityID != "target" {
				return denied
			}
			return nil
		}}, nil
	}
	topicBackend, err := NewCoreBackendWithSources(config, reader, authority, codec, cache, nil, declarationProvider, topicProvider)
	if err != nil {
		t.Fatal(err)
	}
	topicService, err := app.NewCoreService(config, authority, topicBackend, nil)
	if err != nil {
		t.Fatal(err)
	}
	topicRaw := []byte(`{"mode":"topic","query":"objective","limit":1}`)
	topicFirst, err := topicService.Call(ctx, "mce4d_locate", topicRaw, options)
	if err != nil || topicFirst.IsError {
		t.Fatalf("topic locate failed: %v %+v", err, topicFirst)
	}
	topicPage := decode("mce4d_locate", topicFirst)
	topicCandidates := topicPage.Data.(model.LocateData).Candidates
	if len(topicCandidates) != 1 || topicCandidates[0].Ref.(*model.EntityRef).EntityID != "goal" || topicPage.Pagination.NextCursor == nil || len(topicPage.Warnings) != 1 || topicPage.Warnings[0].Code != "AMBIGUOUS_TOPIC" {
		t.Fatal("topic locate lost the first candidate or ambiguity")
	}
	topicContinuation := scopeJSON(t, model.LocateRequest{Mode: "topic", Query: stringPointer("objective"), Limit: 1, Cursor: topicPage.Pagination.NextCursor})
	topicSecond, err := topicService.Call(ctx, "mce4d_locate", topicContinuation, options)
	if err != nil || topicSecond.IsError {
		t.Fatalf("topic continuation failed: %v %+v", err, topicSecond)
	}
	topicLast := decode("mce4d_locate", topicSecond)
	lastCandidates := topicLast.Data.(model.LocateData).Candidates
	if len(lastCandidates) != 1 || lastCandidates[0].Ref.(*model.EntityRef).EntityID != "target" || topicLast.Pagination.NextCursor != nil || len(topicLast.Warnings) != 1 {
		t.Fatal("topic continuation omitted a candidate or ambiguity")
	}
	wrongTopic := scopeJSON(t, model.LocateRequest{Mode: "topic", Query: stringPointer("different"), Limit: 1, Cursor: topicPage.Pagination.NextCursor})
	invalidTopic, err := topicService.Call(ctx, "mce4d_locate", wrongTopic, options)
	if err != nil || !invalidTopic.IsError || decode("mce4d_locate", invalidTopic).Error.Code != "CURSOR_INVALID" {
		t.Fatalf("topic cursor accepted a changed query: %v", err)
	}
	topicApproved = false
	revokedTopic, err := topicService.Call(ctx, "mce4d_locate", topicContinuation, options)
	if err != nil || !revokedTopic.IsError || decode("mce4d_locate", revokedTopic).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("topic config revocation leaked a continuation: %v", err)
	}
	typedTopic, err := model.DecodeLocateRequestJSON(topicRaw, 32768)
	if err != nil {
		t.Fatal(err)
	}
	if err := topicBackend.VerifyDelivery(ctx, principal, "mce4d_locate", typedTopic, topicPage); err == nil {
		t.Fatal("topic config revocation passed final delivery")
	}
	topicApproved, aliasAllowed = true, false
	revokedTopic, err = topicService.Call(ctx, "mce4d_locate", topicContinuation, options)
	if err != nil || !revokedTopic.IsError || decode("mce4d_locate", revokedTopic).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("topic alias revocation leaked a continuation: %v", err)
	}
	aliasAllowed, topicPath = true, "go.mod"
	untrustedTopic, err := topicService.Call(ctx, "mce4d_locate", topicRaw, options)
	if err != nil || !untrustedTopic.IsError || decode("mce4d_locate", untrustedTopic).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("untrusted topic configuration was read: %v", err)
	}
	topicPath = "vocabulary.json"
	publicValidation := scopeJSON(t, model.ValidateRequest{PackToken: &publicData.PackToken})
	unverifiedPlan, err := planService.Call(ctx, "mce4d_validate_pack", publicValidation, options)
	if err != nil || !unverifiedPlan.IsError || decode("mce4d_validate_pack", unverifiedPlan).Error.Code != "FRESHNESS_UNKNOWN" {
		t.Fatalf("wired plan accepted missing workspace proof: %v", err)
	}
	previousName := graph.Symbols[0].Name
	graph.Symbols[0].Name = "tampered materialization"
	if summary, err := report.Read(ctx); err == nil || len(summary.Modules) != 0 {
		t.Fatalf("tampered overview escaped verification: %+v %v", summary, err)
	}
	graph.Symbols[0].Name = previousName
	allowed = false
	if summary, err := report.Read(ctx); err == nil || len(summary.Modules) != 0 {
		t.Fatalf("revoked overview leaked: %+v %v", summary, err)
	}
	allowed = true
	firstPage, err := service.Call(ctx, "mce4d_locate", []byte(`{"mode":"text","query":"sharedneedle","limit":1}`), options)
	if err != nil || firstPage.IsError {
		t.Fatalf("wired locate first page: %v %+v", err, firstPage)
	}
	firstResponse := decode("mce4d_locate", firstPage)
	if firstResponse.Pagination.NextCursor == nil || len(firstResponse.Data.(model.LocateData).Candidates) != 1 {
		t.Fatal("wired locate did not provide a continuation")
	}
	continuation, err := json.Marshal(model.LocateRequest{Mode: "text", Query: stringPointer("sharedneedle"), Limit: 2, Cursor: firstResponse.Pagination.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := service.Call(ctx, "mce4d_locate", continuation, options)
	if err != nil || secondPage.IsError {
		t.Fatalf("wired locate continuation: %v %+v", err, secondPage)
	}
	secondResponse := decode("mce4d_locate", secondPage)
	if secondResponse.Pagination.NextCursor != nil || len(secondResponse.Data.(model.LocateData).Candidates) != 1 {
		t.Fatal("wired locate did not finish")
	}
	firstFile := firstResponse.Data.(model.LocateData).Candidates[0].Ref.(*model.FileRef).Path
	secondFile := secondResponse.Data.(model.LocateData).Candidates[0].Ref.(*model.FileRef).Path
	if firstFile == secondFile {
		t.Fatal("wired locate duplicated a page")
	}
	changedQuery, err := json.Marshal(model.LocateRequest{Mode: "text", Query: stringPointer("different"), Limit: 2, Cursor: firstResponse.Pagination.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	badCursor, err := service.Call(ctx, "mce4d_locate", changedQuery, options)
	if err != nil || !badCursor.IsError || decode("mce4d_locate", badCursor).Error.Code != "CURSOR_INVALID" {
		t.Fatalf("changed continuation accepted: %v", err)
	}
	t.Run("mcp_canary", func(t *testing.T) { verifyMCPCanary(t, ctx, service, repo) })
	tooSmall, err := service.Call(ctx, "mce4d_context_pack", []byte(`{"selector":{"kind":"symbol","symbol_id":"target-main"},"task_kind":"explore","max_output_bytes":8192}`), options)
	if err != nil || !tooSmall.IsError || decode("mce4d_context_pack", tooSmall).Error.Code != "BUDGET_TOO_SMALL" {
		t.Fatalf("undersized envelope was accepted: %v", err)
	}
	packResult, err := service.Call(ctx, "mce4d_context_pack", []byte(`{"selector":{"kind":"symbol","symbol_id":"target-main"},"task_kind":"explore","max_output_bytes":16384}`), options)
	if err != nil || packResult.IsError {
		t.Fatalf("wired pack: %v %s", err, packResult.Blocks[0])
	}
	packResponse := decode("mce4d_context_pack", packResult)
	packData := packResponse.Data.(model.PackData)
	if packResponse.Freshness.Status != "not_checked" || packResponse.Pagination.NextCursor == nil || packData.PackToken == "" {
		t.Fatal("analysis pack lost cursor or freshness")
	}
	t.Run("restore_pinned_symbol_pack", func(t *testing.T) {
		guard := backend.packGuard(principal)
		original, err := cache.Get(ctx, packData.PackToken, principal.Digest, guard)
		if err != nil {
			t.Fatal(err)
		}
		request := []byte(`{"selector":{"kind":"symbol","symbol_id":"target-main"},"task_kind":"explore","max_output_bytes":16384}`)
		envelope, err := original.ArchiveEnvelope(ctx, principal.ID, request, 900, 16*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		claims, err := codec.VerifyPrincipal(ctx, envelope.Token, "pack", principal.Digest, guard)
		if err != nil {
			t.Fatal(err)
		}
		factory, err := backend.SymbolRestoreFactory(principal)
		if err != nil {
			t.Fatal(err)
		}
		wrong := claims.Binding
		wrong.PrincipalDigest = strings.Repeat("f", 64)
		if _, err := factory(ctx, wrong); err == nil {
			t.Fatal("different principal was accepted")
		}
		input, err := factory(ctx, claims.Binding)
		if err != nil {
			t.Fatalf("reopen pinned generation: %v", err)
		}
		input.Pack.Codec, input.Pack.Token, input.Pack.PrincipalDigest = codec, envelope.Token, principal.Digest
		input.Pack.Guard, input.Pack.MaxArchiveBytes = guard, 16*1024*1024
		restored, err := app.RestoreFreshnessEnvelope(ctx, envelope.Bytes, input, 16*1024*1024)
		if err != nil {
			t.Fatalf("restore signed materialization: %v", err)
		}
		pageOptions := packPageOptions(options)
		before, err := original.Continue(ctx, envelope.Token, *packResponse.Pagination.NextCursor, principal.Digest, guard, pageOptions)
		if err != nil {
			t.Fatal(err)
		}
		after, err := restored.Continue(ctx, envelope.Token, *packResponse.Pagination.NextCursor, principal.Digest, guard, pageOptions)
		if err != nil || !reflect.DeepEqual(before.Response.Data, after.Response.Data) || before.Response.Freshness.Status != after.Response.Freshness.Status {
			t.Fatalf("restored continuation changed the result: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(worktreeDir, "scratch", "pack"), 0700); err != nil {
			t.Fatal(err)
		}
		store, err := packstore.New(worktreeRoot, []*paths.Root{root, catalogRoot}, shared, registration.WorktreeID, 16*1024*1024, 16*1024*1024, 2,
			func(ctx context.Context) error {
				_, err := reader.Recheck(ctx, principal, claims.Binding.Scope)
				return err
			}, func(context.Context, int64) (packstore.BudgetReservation, error) {
				return scopeBudgetReservation{}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		retention, err := app.NewSymbolPackRetention(store, codec, 16*1024*1024, 900, factory)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := retention.Save(ctx, original, principal.Digest, principal.ID, request, guard)
		if err != nil || handle.Token() != envelope.Token {
			t.Fatalf("persist original materialization: %v", err)
		}
		freshOwner, err := app.NewSymbolPackRetention(store, codec, 16*1024*1024, 900, factory)
		if err != nil {
			t.Fatal(err)
		}
		handle, err = freshOwner.Get(ctx, envelope.Token, principal.Digest, principal.ID, guard)
		if err != nil {
			t.Fatalf("restore with a fresh owner: %v", err)
		}
		storedPage, err := handle.Continue(ctx, *packResponse.Pagination.NextCursor, pageOptions)
		if err != nil || !reflect.DeepEqual(before.Response.Data, storedPage.Response.Data) {
			t.Fatalf("persisted continuation changed the result: %v", err)
		}
		blockedPath = "main.go"
		if page, err := restored.Continue(ctx, envelope.Token, *packResponse.Pagination.NextCursor, principal.Digest, guard, pageOptions); err == nil || len(page.Blocks) != 0 {
			t.Fatal("restored pack leaked data after path revocation")
		}
		if page, err := handle.Continue(ctx, *packResponse.Pagination.NextCursor, pageOptions); err == nil || len(page.Blocks) != 0 {
			t.Fatal("persisted pack leaked data after path revocation")
		}
		blockedPath = ""
		allowed = false
		if _, err := factory(ctx, claims.Binding); err == nil {
			t.Fatal("restore accepted a revoked principal")
		}
		allowed = true
	})
	t.Run("retained_cli_symbol_across_services", func(t *testing.T) {
		t.Cleanup(func() { allowed, blockedPath = true, "" })
		if err := os.MkdirAll(filepath.Join(worktreeDir, "scratch", "pack"), 0700); err != nil {
			t.Fatal(err)
		}
		store, err := packstore.New(worktreeRoot, []*paths.Root{root, catalogRoot}, shared, registration.WorktreeID, 16*1024*1024, 32*1024*1024, 32,
			func(ctx context.Context) error {
				_, err := reader.Recheck(ctx, principal, first.Vector)
				return err
			}, func(context.Context, int64) (packstore.BudgetReservation, error) {
				return scopeBudgetReservation{}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		limited := *backend
		limited.limits.QueryResultCacheBytes = 1024
		if _, err := limited.WithCLIPackRetention(store); err == nil {
			t.Fatal("store exceeding effective byte quota was accepted")
		}
		limited = *backend
		limited.limits.MaxRetainedQueryResults = 1
		if _, err := limited.WithCLIPackRetention(store); err == nil {
			t.Fatal("store exceeding effective result count was accepted")
		}
		opened, released := 0, 0
		selectedConfig := config
		var lastBackend *CoreBackend
		run := func(storage app.SymbolPackStorage, action string, raw []byte) (model.Response, int) {
			t.Helper()
			open := func(_ context.Context, configPath, worktree, profile string) (*app.CoreService, func() error, error) {
				if configPath != "" || worktree != "" || profile != "" {
					return nil, nil, cli.ErrBindingDenied
				}
				effective, _, err := selectedConfig.StartupConfig()
				if err != nil {
					return nil, nil, err
				}
				freshCache, err := app.NewFreshnessPackCache(codec, 32, 32*1024*1024, minInt64(effective.Core.PackTtlSeconds, 900))
				if err != nil {
					return nil, nil, err
				}
				freshBackend, err := NewCoreBackend(selectedConfig, reader, authority, codec, freshCache, nil)
				if err == nil {
					freshBackend, err = freshBackend.WithCLIPackRetention(storage)
				}
				if err != nil {
					return nil, nil, err
				}
				service, err := app.NewCoreService(selectedConfig, authority, freshBackend, nil)
				if err != nil {
					return nil, nil, err
				}
				opened++
				lastBackend = freshBackend
				return service, func() error { released++; return nil }, nil
			}
			var out, errOut bytes.Buffer
			code := cli.Run(ctx, []string{"query", action, "--input-json", "-"}, io.NopCloser(bytes.NewReader(raw)), &out, &errOut, cli.Dependencies{CPUs: 4, OpenRetainedCore: open})
			if errOut.Len() != 0 {
				t.Fatalf("CLI %s diagnostic: %s", action, errOut.String())
			}
			name := map[string]string{"context": "mce4d_context_pack", "expand": "mce4d_expand_pack", "validate": "mce4d_validate_pack"}[action]
			response, err := registry.DecodeResponse(contracts.MCP, name, bytes.TrimSpace(out.Bytes()), 131072, 4)
			if err != nil {
				t.Fatalf("CLI %s response: %v", action, err)
			}
			return response, code
		}
		request := []byte(`{"selector":{"kind":"symbol","symbol_id":"target-main"},"task_kind":"explore","max_output_bytes":8192}`)
		failed, code := run(refusingPackCreate{store}, "context", request)
		if code == 0 || failed.Data != nil || failed.Error == nil {
			t.Fatal("a failed publication returned a pack token")
		}
		initial, code := run(store, "context", request)
		if code != 0 && code != 6 || initial.Error != nil || initial.Pagination.NextCursor == nil {
			t.Fatalf("initial retained CLI pack: exit=%d error=%+v", code, initial.Error)
		}
		data, ok := initial.Data.(model.PackData)
		if !ok || data.PackToken == "" {
			t.Fatal("retained CLI pack omitted its token")
		}
		expandRaw := scopeJSON(t, model.ExpandRequest{PackToken: data.PackToken, NextCursor: *initial.Pagination.NextCursor, MaxOutputBytes: int64Pointer(8192)})
		next, code := run(store, "expand", expandRaw)
		nextData, nextOK := next.Data.(model.PackData)
		if code != 0 && code != 6 || next.Error != nil || !nextOK || nextData.PackToken != data.PackToken || opened < 3 || opened != released {
			t.Fatalf("retained continuation: exit=%d error=%+v", code, next.Error)
		}
		validateRaw := scopeJSON(t, model.ValidateRequest{PackToken: &data.PackToken})
		validation, code := run(store, "validate", validateRaw)
		if code != 3 || validation.Error == nil || validation.Error.Code != "FRESHNESS_UNKNOWN" {
			t.Fatalf("validation without a workspace verifier was accepted: %+v", validation.Error)
		}
		blockedPath = "main.go"
		deniedPage, code := run(store, "expand", expandRaw)
		if code == 0 || deniedPage.Data != nil || deniedPage.Error == nil {
			t.Fatal("revoked source was returned by a later invocation")
		}
		blockedPath = ""
		claims, err := codec.VerifyPrincipal(ctx, data.PackToken, "pack", principal.Digest, backend.packGuard(principal))
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(worktreeDir, "scratch", "pack", claims.Binding.ResultID+".pack")
		original, err := os.ReadFile(file)
		if err != nil || len(original) < 2 {
			t.Fatalf("retained result unavailable: %v", err)
		}
		tampered := bytes.Clone(original)
		tampered[len(tampered)/2] ^= 1
		if err := os.WriteFile(file, tampered, 0600); err != nil {
			t.Fatal(err)
		}
		typedRequest, err := registry.DecodeRequest(contracts.MCP, "mce4d_context_pack", request, 32768, 4)
		if err != nil || lastBackend.VerifyDelivery(ctx, principal, "mce4d_context_pack", typedRequest, initial) == nil {
			t.Fatal("tampered result passed the final delivery check")
		}
		tamperedPage, code := run(store, "expand", expandRaw)
		if code == 0 || tamperedPage.Data != nil || tamperedPage.Error == nil {
			t.Fatal("tampered stored result was returned")
		}
		if err := os.WriteFile(file, original, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		missing, code := run(store, "expand", expandRaw)
		if code != 3 || missing.Data != nil || missing.Error == nil || missing.Error.Code != "RESULT_EXPIRED" {
			t.Fatalf("missing materialization was not reported: %+v", missing.Error)
		}
		if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing materialization was silently rebuilt")
		}
		var nonceBytes [32]byte
		if _, err := rand.Read(nonceBytes[:]); err != nil {
			t.Fatal(err)
		}
		nonce := hex.EncodeToString(nonceBytes[:])
		marker, err := os.OpenFile(filepath.Join(base, ".retained-cli-fixture"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		written, writeErr := io.WriteString(marker, nonce)
		if written != len(nonce) {
			writeErr = errors.Join(writeErr, io.ErrShortWrite)
		}
		if err := errors.Join(writeErr, marker.Close()); err != nil {
			t.Fatal(err)
		}
		processFirst, code := runRetainedCLIProcess(t, ctx, base, nonce, registration.WorktreeID, registration.ProfileID, "context", request, nil)
		processData, processOK := processFirst.Data.(model.PackData)
		if code != 0 && code != 6 || !processOK || processFirst.Pagination.NextCursor == nil {
			t.Fatalf("first CLI process failed: %+v", processFirst.Error)
		}
		processClaims, err := codec.VerifyPrincipal(ctx, processData.PackToken, "pack", principal.Digest, backend.packGuard(principal))
		if err != nil {
			t.Fatal(err)
		}
		processFile := filepath.Join(worktreeDir, "scratch", "pack", processClaims.Binding.ResultID+".pack")
		processInfo, err := os.Lstat(processFile)
		if err != nil || !processInfo.Mode().IsRegular() {
			t.Fatal("process-owned fixture result unavailable")
		}
		t.Cleanup(func() {
			cleanupRetainedFixtureFile(t, worktreeRoot, []*paths.Root{root, catalogRoot}, processFile, processInfo)
		})
		processExpand := scopeJSON(t, model.ExpandRequest{PackToken: processData.PackToken, NextCursor: *processFirst.Pagination.NextCursor, MaxOutputBytes: int64Pointer(8192)})
		processNext, code := runRetainedCLIProcess(t, ctx, base, nonce, registration.WorktreeID, registration.ProfileID, "expand", processExpand, nil)
		processNextData, processNextOK := processNext.Data.(model.PackData)
		if code != 0 && code != 6 || !processNextOK || processNextData.PackToken != processData.PackToken {
			t.Fatalf("second CLI process failed to restore the pack: %+v", processNext.Error)
		}
		selectedConfig, err = app.NewRuntimeConfigService(ctx, scopeShortTTLConfig{}, 4)
		if err != nil {
			t.Fatal(err)
		}
		short, code := run(store, "context", request)
		shortData, shortOK := short.Data.(model.PackData)
		if code != 0 && code != 6 || !shortOK || short.Pagination.NextCursor == nil {
			t.Fatalf("short-lived pack was not created: %+v", short.Error)
		}
		shortClaims, err := codec.VerifyPrincipal(ctx, shortData.PackToken, "pack", principal.Digest, backend.packGuard(principal))
		if err != nil {
			t.Fatal(err)
		}
		shortFile := filepath.Join(worktreeDir, "scratch", "pack", shortClaims.Binding.ResultID+".pack")
		shortInfo, err := os.Lstat(shortFile)
		if err != nil || !shortInfo.Mode().IsRegular() {
			t.Fatal("short-lived fixture result unavailable")
		}
		t.Cleanup(func() {
			cleanupRetainedFixtureFile(t, worktreeRoot, []*paths.Root{root, catalogRoot}, shortFile, shortInfo)
		})
		shortExpand := scopeJSON(t, model.ExpandRequest{PackToken: shortData.PackToken, NextCursor: *short.Pagination.NextCursor, MaxOutputBytes: int64Pointer(8192)})
		timer := time.NewTimer(6 * time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			t.Fatal(ctx.Err())
		}
		expired, code := run(store, "expand", shortExpand)
		if code != 3 || expired.Data != nil || expired.Error == nil || expired.Error.Code != "RESULT_EXPIRED" && expired.Error.Code != "CURSOR_EXPIRED" {
			t.Fatalf("expired result was returned: %+v", expired.Error)
		}
		if actual, err := os.ReadFile(filepath.Join(repo, "main.go")); err != nil || !bytes.Equal(actual, source) {
			t.Fatal("target fixture changed")
		}
	})
	packCursor := *packResponse.Pagination.NextCursor
	seenItems := len(packData.Items)
	for pages := 0; pages < 100; pages++ {
		expandRaw, err := json.Marshal(model.ExpandRequest{PackToken: packData.PackToken, NextCursor: packCursor, MaxOutputBytes: int64Pointer(16384)})
		if err != nil {
			t.Fatal(err)
		}
		expanded, err := service.Call(ctx, "mce4d_expand_pack", expandRaw, options)
		if err != nil || expanded.IsError {
			t.Fatalf("wired expand: %v %s", err, expanded.Blocks[0])
		}
		response := decode("mce4d_expand_pack", expanded)
		data := response.Data.(model.PackData)
		if data.PackToken != packData.PackToken || response.Freshness.Status != "not_checked" {
			t.Fatal("expanded pack rebound or claimed freshness")
		}
		seenItems += len(data.Items)
		if response.Pagination.NextCursor == nil {
			break
		}
		if *response.Pagination.NextCursor == packCursor || pages == 99 {
			t.Fatal("pack cursor did not progress")
		}
		packCursor = *response.Pagination.NextCursor
	}
	if seenItems < 2 {
		t.Fatal("pack omitted source or validation obligations")
	}
	validateRaw, err := json.Marshal(model.ValidateRequest{PackToken: &packData.PackToken})
	if err != nil {
		t.Fatal(err)
	}
	validateResult, err := service.Call(ctx, "mce4d_validate_pack", validateRaw, options)
	if err != nil || !validateResult.IsError || decode("mce4d_validate_pack", validateResult).Error.Code != "FRESHNESS_UNKNOWN" {
		t.Fatalf("missing workspace proof was accepted: %v", err)
	}
	home, sandbox := scopeRoot(t, base), scopeRoot(t, sandboxDir)
	metadataScanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	allowRoot := func(ctx context.Context) error {
		return access(ctx, principal, registration.WorktreeID, registration.ProfileID)
	}
	allowMetadata := func(ctx context.Context, _ string) error { return allowRoot(ctx) }
	if err := inventory.PrepareIgnoreSandbox(ctx, sandbox, allowRoot); err != nil {
		t.Fatal(err)
	}
	configSource := func(root *paths.Root, name string) inventory.GitConfigSource {
		return inventory.GitConfigSource{Root: root, RelativePath: name, Authorize: allowMetadata}
	}
	gitPlan := inventory.GitConfigPlan{System: configSource(home, "system"), XDG: configSource(home, "xdg"), Global: configSource(home, "global"), Repository: configSource(common, "config"), VerifyDiscovery: func(context.Context) error { return errors.Join(root.Verify(), common.Verify(), home.Verify()) },
		ResolveGlobalIgnore: func(_ context.Context, value *string) (*inventory.ApprovedGlobalIgnore, error) {
			if value != nil {
				return nil, inventory.ErrGitConfigUnsupported
			}
			return &inventory.ApprovedGlobalIgnore{Root: home, RelativePath: "ignore", Authorize: allowMetadata}, nil
		},
		ValidateWorktree: func(_ context.Context, fields inventory.GitConfigFields) error {
			if fields.Worktree != nil || fields.Bare != nil && *fields.Bare {
				return inventory.ErrGitConfigUnsupported
			}
			return nil
		}}
	gitConfig, err := inventory.InspectGitIgnoreConfig(ctx, git, gitPlan, metadataScanner, inventory.GitConfigBudget{MaxFileBytes: 4096, MaxTotalBytes: 8192, MaxOutputBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	inventoryRequest := inventory.LocalInventoryRequest{Root: root, Config: gitConfig, Scanner: metadataScanner, Sandbox: sandbox, GitBinary: git,
		IndexBudget: inventory.GitIndexBudget{MaxBytes: 1 << 20, MaxEntries: 1000, MaxPathBytes: 4096, MaxDecodedPathBytes: 1 << 20}, WalkBudget: inventory.WalkBudget{MaxVisited: 1000, MaxDirectories: 100, MaxDepth: 16, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20}, IgnoreCaptureBudget: inventory.IgnoreCaptureBudget{MaxFiles: 100, MaxFileBytes: 4096, MaxTotalBytes: 65536}, IgnoreBudget: inventory.IgnoreBudget{MaxSources: 100, MaxRules: 1000, MaxRuleBytes: 65536, MaxQueries: 1000, MaxQueryBytes: 65536, MaxOutputBytes: 65536}, MaxFileMapEntries: 1000,
		AuthorizeRoot: allowRoot, AuthorizePath: func(ctx context.Context, name string, directory bool) (bool, error) {
			if err := allowRoot(ctx); err != nil {
				return false, err
			}
			if directory {
				return true, nil
			}
			return authority.path(ctx, principal, first.Binding.Policy, name) == nil, nil
		}, AuthorizeMetadata: allowMetadata, AuthorizeSandbox: allowRoot, Exclude: func(string, bool) bool { return false }}
	expected, err := inventory.InventoryLocal(ctx, inventoryRequest)
	if err != nil {
		t.Fatal(err)
	}
	revalidate := indexer.RevalidateRequest{Inventory: inventoryRequest, Expected: expected, Snapshot: captured, MaxManifestBytes: 8192, MaxFileBytes: 1024, MaxSourceBytes: 8192, VerifyAuthority: func(ctx context.Context, manifest model.CaptureManifest) error {
		if manifest.WorktreeID != registration.WorktreeID || manifest.ProfileID != registration.ProfileID || manifest.PolicyDigest != registration.PolicyDigest {
			return ErrScopeChanged
		}
		_, err := reader.Recheck(ctx, principal, first.Vector)
		return err
	}, VerifyDependencies: func(ctx context.Context, digests []string) error {
		if len(digests) != 0 {
			return indexer.ErrSnapshotUnverifiable
		}
		return ctx.Err()
	}}
	workspace, err := NewLocalWorkspaceVerifier(reader, writer, scopeJob, first.Vector, revalidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"metadata_only", "read_error"} {
		manifest := captured.Manifest()
		manifest.SnapshotDigest = ""
		files := []snapshot.File{}
		for i := range manifest.Files {
			if manifest.Files[i].Path == "main.go" {
				manifest.Files[i].Status = state
				manifest.Files[i].ContentHash, manifest.Files[i].ByteLength = nil, nil
				continue
			}
			file, ok := captured.File(manifest.Files[i].Path)
			if !ok {
				t.Fatal("fixture capture is incomplete")
			}
			files = append(files, file)
		}
		incomplete, err := snapshot.Freeze(manifest, files, 8192, 8192)
		if err != nil {
			t.Fatal(err)
		}
		scope := first.Vector
		scope.Members = append([]model.ScopeMember(nil), scope.Members...)
		scope.Members[0].SnapshotDigest = incomplete.Manifest().SnapshotDigest
		scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 32768)
		if err != nil {
			t.Fatal(err)
		}
		request := revalidate
		request.Snapshot = incomplete
		if _, err := NewLocalWorkspaceVerifier(reader, writer, scopeJob, scope, request, nil); !errors.Is(err, app.ErrFreshnessUnknown) {
			t.Fatalf("%s workspace capture became verifiable: %v", state, err)
		}
	}
	badScope := first.Vector
	badScope.Members = append([]model.ScopeMember(nil), first.Vector.Members...)
	badScope.Members[0].RecordEpoch++
	if _, err := NewLocalWorkspaceVerifier(reader, writer, scopeJob, badScope, revalidate, nil); err == nil {
		t.Fatal("invalid pinned scope digest was accepted")
	}
	wrongMember := first.Vector.Members[0]
	wrongMember.RecordEpoch++
	if result, err := workspace(ctx, principal, wrongMember, captured); err == nil || result != app.WorkspaceUnknown {
		t.Fatalf("wrong scope was accepted: %s %v", result, err)
	}
	wrongWriter, err := NewLocalWorkspaceVerifier(reader, writer, "99999999-9999-4999-8999-999999999999", first.Vector, revalidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := wrongWriter(ctx, principal, first.Vector.Members[0], captured); err == nil || result != app.WorkspaceUnknown {
		t.Fatalf("wrong writer was accepted: %s %v", result, err)
	}
	wrongPrincipal := principal
	wrongPrincipal.ID = "other-fixture-actor"
	if result, err := workspace(ctx, wrongPrincipal, first.Vector.Members[0], captured); err == nil || result != app.WorkspaceUnknown {
		t.Fatalf("wrong principal was accepted: %s %v", result, err)
	}
	if result, err := workspace(context.Background(), principal, first.Vector.Members[0], captured); err == nil || result != app.WorkspaceUnknown {
		t.Fatalf("unbounded verification was accepted: %s %v", result, err)
	}
	blockedDependencies := revalidate
	blockedDependencies.VerifyDependencies = func(context.Context, []string) error { return denied }
	blockedWorkspace, err := NewLocalWorkspaceVerifier(reader, writer, scopeJob, first.Vector, blockedDependencies, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := blockedWorkspace(ctx, principal, first.Vector.Members[0], captured); err == nil || result != app.WorkspaceUnknown {
		t.Fatalf("unproved dependencies were accepted: %s %v", result, err)
	}
	// The verifier retains a private copy of the captured inventory.
	expected.Entries[0].Path = "fixture-mutated-after-construction.go"
	if _, err := NewLocalWorkspaceVerifier(reader, writer, scopeJob, first.Vector, revalidate, nil); err == nil {
		t.Fatal("mismatched captured inventory was accepted")
	}
	checkedBackend, err := NewCoreBackend(config, reader, authority, codec, cache, workspace)
	if err != nil {
		t.Fatal(err)
	}
	checkedService, err := app.NewCoreService(config, authority, checkedBackend, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("rename_cli_mcp_retained_result", func(t *testing.T) {
		verifyRetainedRenameCanary(t, ctx, checkedBackend, config, principal, first.Vector, captured, worktreeRoot, shared, worktreeDir, repo)
	})
	checkedStatus, err := checkedService.Call(ctx, "mce4d_workspace_status", []byte(`{"verify":"workspace"}`), options)
	if err != nil || checkedStatus.IsError {
		t.Fatalf("checked workspace status: %v", err)
	}
	checkedStatusResponse := decode("mce4d_workspace_status", checkedStatus)
	if checkedStatusResponse.Freshness.Status != "observed_current" || checkedStatusResponse.Freshness.Scope != "workspace" || checkedStatusResponse.Freshness.CheckedAt == nil || checkedStatusResponse.Freshness.ChangedCount == nil || *checkedStatusResponse.Freshness.ChangedCount != 0 || checkedStatusResponse.Readiness != "needs_context" {
		t.Fatal("workspace status did not report a bounded observation")
	}
	if err := checkedBackend.VerifyDelivery(ctx, principal, "mce4d_workspace_status", model.StatusRequest{}, checkedStatusResponse); !errors.Is(err, app.ErrCoreBackend) {
		t.Fatalf("unrequested workspace freshness claim passed delivery: %v", err)
	}
	lateBackend, err := NewCoreBackend(config, reader, authority, codec, cache, func(ctx context.Context, actor app.CorePrincipal, member model.ScopeMember, captured *snapshot.Snapshot) (app.WorkspaceObservation, error) {
		result, err := workspace(ctx, actor, member, captured)
		blockedPath = "main.go"
		return result, err
	})
	if err != nil {
		t.Fatal(err)
	}
	lateService, err := app.NewCoreService(config, authority, lateBackend, nil)
	if err != nil {
		t.Fatal(err)
	}
	lateStatus, err := lateService.Call(ctx, "mce4d_workspace_status", []byte(`{"verify":"workspace"}`), options)
	if err != nil || !lateStatus.IsError || decode("mce4d_workspace_status", lateStatus).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("late source revocation was ignored by workspace status: %v", err)
	}
	deniedStatus, err := checkedService.Call(ctx, "mce4d_workspace_status", []byte(`{"verify":"workspace"}`), options)
	if err != nil || !deniedStatus.IsError || decode("mce4d_workspace_status", deniedStatus).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("workspace status ignored revoked source access: %v", err)
	}
	blockedPath = ""
	checked, err := checkedService.Call(ctx, "mce4d_context_pack", []byte(`{"selector":{"kind":"symbol","symbol_id":"target-main"},"task_kind":"debug","max_output_bytes":16384}`), options)
	if err != nil || checked.IsError {
		t.Fatalf("checked pack: %v %s", err, checked.Blocks[0])
	}
	checkedResponse := decode("mce4d_context_pack", checked)
	checkedData := checkedResponse.Data.(model.PackData)
	if checkedResponse.Freshness.Status != "observed_current" || checkedResponse.Freshness.Scope != "workspace" || checkedData.EditPreconditionValidated || checkedResponse.Pagination.NextCursor == nil {
		t.Fatal("checked pack did not preserve observation semantics")
	}
	t.Run("restore_checked_symbol_pack", func(t *testing.T) {
		guard := checkedBackend.packGuard(principal)
		original, err := cache.Get(ctx, checkedData.PackToken, principal.Digest, guard)
		if err != nil {
			t.Fatal(err)
		}
		request := []byte(`{"selector":{"kind":"symbol","symbol_id":"target-main"},"task_kind":"debug","max_output_bytes":16384}`)
		factory, err := checkedBackend.SymbolRestoreFactory(principal)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(worktreeDir, "scratch", "pack"), 0700); err != nil {
			t.Fatal(err)
		}
		store, err := packstore.New(worktreeRoot, []*paths.Root{root, catalogRoot}, shared, registration.WorktreeID, 16*1024*1024, 32*1024*1024, 4,
			func(ctx context.Context) error {
				_, err := reader.Recheck(ctx, principal, first.Vector)
				return err
			}, func(context.Context, int64) (packstore.BudgetReservation, error) {
				return scopeBudgetReservation{}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		retention, err := app.NewSymbolPackRetention(store, codec, 16*1024*1024, 900, factory)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := retention.Save(ctx, original, principal.Digest, principal.ID, request, guard)
		if err != nil {
			t.Fatalf("persist checked pack: %v", err)
		}
		validation, err := handle.Validate(ctx)
		if err != nil || !validation.Data.EditPreconditionValidated || validation.Freshness.Status != "observed_current" {
			t.Fatalf("restored pack did not recheck workspace: %v", err)
		}
		file := filepath.Join(repo, "main.go")
		if err := os.WriteFile(file, append(append([]byte(nil), source...), []byte("// changed fixture\n")...), 0600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.WriteFile(file, source, 0600); err != nil {
				t.Errorf("restore fixture source: %v", err)
			}
		})
		if result, err := handle.Validate(ctx); err == nil || result.Data.EditPreconditionValidated {
			t.Fatal("restored pack accepted changed workspace bytes")
		}
	})
	t.Run("retained_cli_plan_across_processes", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Join(worktreeDir, "scratch", "pack"), 0700); err != nil {
			t.Fatal(err)
		}
		var nonceBytes [32]byte
		if _, err := rand.Read(nonceBytes[:]); err != nil {
			t.Fatal(err)
		}
		nonce := hex.EncodeToString(nonceBytes[:])
		marker, err := os.OpenFile(filepath.Join(base, ".retained-cli-plan-fixture"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		written, writeErr := io.WriteString(marker, nonce)
		if written != len(nonce) {
			writeErr = errors.Join(writeErr, io.ErrShortWrite)
		}
		if err := errors.Join(writeErr, marker.Close()); err != nil {
			t.Fatal(err)
		}
		items := []overview.DocumentDeclaration{{Kind: "objective", Source: declRef}, {Kind: "constraint", Source: constraintRef}, {Kind: "constraint", Source: wholeRule}, {Kind: "constraint", Source: vocabularyRule}}
		request := []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","intent":"Review the design","max_output_bytes":8192}`)
		firstPage, code := runRetainedCLIProcess(t, ctx, base, nonce, registration.WorktreeID, registration.ProfileID, "context", request, items)
		data, ok := firstPage.Data.(model.PackData)
		if code != 0 && code != 6 || !ok || firstPage.Pagination.NextCursor == nil || data.PackToken == "" {
			t.Fatalf("first plan CLI process failed: %+v", firstPage.Error)
		}
		claims, err := codec.VerifyPrincipal(ctx, data.PackToken, "pack", principal.Digest, backend.packGuard(principal))
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(worktreeDir, "scratch", "pack", claims.Binding.ResultID+".pack")
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("plan process fixture result unavailable")
		}
		t.Cleanup(func() { cleanupRetainedFixtureFile(t, worktreeRoot, []*paths.Root{root, catalogRoot}, file, info) })
		expand := scopeJSON(t, model.ExpandRequest{PackToken: data.PackToken, NextCursor: *firstPage.Pagination.NextCursor, MaxOutputBytes: int64Pointer(8192)})
		nextPage, code := runRetainedCLIProcess(t, ctx, base, nonce, registration.WorktreeID, registration.ProfileID, "expand", expand, items)
		nextData, nextOK := nextPage.Data.(model.PackData)
		if code != 0 && code != 6 || !nextOK || nextData.PackToken != data.PackToken {
			t.Fatalf("second plan CLI process failed: %+v", nextPage.Error)
		}
	})
	checkedValidateRaw, err := json.Marshal(model.ValidateRequest{PackToken: &checkedData.PackToken})
	if err != nil {
		t.Fatal(err)
	}
	validated, err := checkedService.Call(ctx, "mce4d_validate_pack", checkedValidateRaw, options)
	if err != nil || validated.IsError || !decode("mce4d_validate_pack", validated).Data.(model.ValidateData).EditPreconditionValidated {
		t.Fatalf("live workspace validation: %v", err)
	}
	checkedPlanRequest := planRequest
	checkedPlanRequest.FreshnessMode = "checked_workspace"
	checkedPlanRaw, err := json.Marshal(checkedPlanRequest)
	if err != nil {
		t.Fatal(err)
	}
	checkedPlanDigest, err := model.RequestDigest("context_pack", principal.ID, "PackRequest", checkedPlanRequest, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	checkedPlanBinding := planBinding
	checkedPlanBinding.RequestDigest = checkedPlanDigest
	checkedPlanValidation := planValidation
	checkedPlanValidation.VerifyWorkspace = func(ctx context.Context, member model.ScopeMember, captured *snapshot.Snapshot) (app.WorkspaceObservation, error) {
		return workspace(ctx, principal, member, captured)
	}
	checkedPlan, checkedPlanToken, err := app.NewFreshnessPlan(ctx, app.FreshnessPlanInput{Plan: plan, Codec: codec, Binding: checkedPlanBinding, Guard: planGuard, RequestJSON: checkedPlanRaw, PrincipalID: principal.ID, Coverage: graph.Coverage, Scanner: scanner, Validation: checkedPlanValidation, CaptureLive: func(ctx context.Context, _ model.ScopeMember, name string) (snapshot.File, error) {
		return inventory.CaptureFile(ctx, root, scanner, name, 1024, func(ctx context.Context, path string) error {
			return authority.path(ctx, principal, first.Binding.Policy, path)
		})
	}, RequestLimit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.AdmitRetained(ctx, checkedPlan, checkedPlanToken, principal.Digest, planGuard, 65536); err != nil {
		t.Fatal(err)
	}
	checkedPlanPage, err := checkedPlan.First(ctx, packPageOptions(options))
	if err != nil {
		t.Fatal(err)
	}
	checkedPlanWire := decode("mce4d_context_pack", app.CoreCallResult{Blocks: checkedPlanPage.Blocks})
	if checkedPlanWire.Freshness.Status != "observed_current" || checkedPlanWire.Pagination.NextCursor == nil {
		t.Fatal("plan did not verify the workspace")
	}
	planValidationResult, err := checkedPlan.Validate(ctx, checkedPlanToken, principal.Digest, planGuard)
	if err != nil || !planValidationResult.Data.EditPreconditionValidated {
		t.Fatalf("plan workspace validation: %v", err)
	}
	checkedPlanBackend, err := NewCoreBackendWithPlan(config, reader, authority, codec, cache, workspace, declarationProvider)
	if err != nil {
		t.Fatal(err)
	}
	checkedPlanService, err := app.NewCoreService(config, authority, checkedPlanBackend, nil)
	if err != nil {
		t.Fatal(err)
	}
	wiredChecked, err := checkedPlanService.Call(ctx, "mce4d_context_pack", []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","freshness_mode":"checked_workspace","max_output_bytes":16384}`), options)
	if err != nil || wiredChecked.IsError {
		t.Fatalf("wired checked plan: %v %+v", err, wiredChecked)
	}
	wiredCheckedPage := decode("mce4d_context_pack", wiredChecked)
	wiredCheckedData := wiredCheckedPage.Data.(model.PackData)
	if wiredCheckedPage.Freshness.Status != "observed_current" || wiredCheckedPage.Pagination.NextCursor == nil || wiredCheckedData.EditPreconditionValidated {
		t.Fatal("wired plan did not observe workspace freshness")
	}
	wiredCheckedValidation := scopeJSON(t, model.ValidateRequest{PackToken: &wiredCheckedData.PackToken})
	wiredValidated, err := checkedPlanService.Call(ctx, "mce4d_validate_pack", wiredCheckedValidation, options)
	if err != nil || wiredValidated.IsError || !decode("mce4d_validate_pack", wiredValidated).Data.(model.ValidateData).EditPreconditionValidated {
		t.Fatalf("wired plan workspace validation: %v", err)
	}
	documentInfo, err := os.Stat(filepath.Join(repo, "design.md"))
	if err != nil {
		t.Fatal(err)
	}
	changedDocument := append([]byte(nil), documentSource...)
	changedDocument[0] = 'X'
	if err := os.WriteFile(filepath.Join(repo, "design.md"), changedDocument, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(repo, "design.md"), documentInfo.ModTime(), documentInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	if page, err := checkedPlan.Continue(ctx, checkedPlanToken, *checkedPlanWire.Pagination.NextCursor, principal.Digest, planGuard, packPageOptions(options)); !errors.Is(err, app.ErrFreshnessChanged) || len(page.Blocks) != 0 {
		t.Fatalf("changed plan document leaked: %v", err)
	}
	wiredCheckedExpand := scopeJSON(t, model.ExpandRequest{PackToken: wiredCheckedData.PackToken, NextCursor: *wiredCheckedPage.Pagination.NextCursor, MaxOutputBytes: int64Pointer(16384)})
	wiredChanged, err := checkedPlanService.Call(ctx, "mce4d_expand_pack", wiredCheckedExpand, options)
	if err != nil || !wiredChanged.IsError || decode("mce4d_expand_pack", wiredChanged).Error.Code != "SOURCE_PRECONDITION_FAILED" {
		t.Fatalf("wired plan returned changed document: %v %+v", err, wiredChanged)
	}
	if err := os.WriteFile(filepath.Join(repo, "design.md"), documentSource, 0600); err != nil {
		t.Fatal(err)
	}
	changed := []byte(strings.Replace(string(source), "Example()", "ExAmple()", 1))
	if len(changed) != len(source) {
		t.Fatal("fixture changed file length")
	}
	beforeInfo, err := os.Stat(filepath.Join(repo, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(repo, "main.go"), beforeInfo.ModTime(), beforeInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	verifyWorkspace := "workspace"
	if err := checkedBackend.VerifyDelivery(ctx, principal, "mce4d_workspace_status", model.StatusRequest{Verify: &verifyWorkspace}, checkedStatusResponse); !errors.Is(err, app.ErrFreshnessChanged) {
		t.Fatalf("stale current status passed delivery: %v", err)
	}
	changedStatus, err := checkedService.Call(ctx, "mce4d_workspace_status", []byte(`{"verify":"workspace"}`), options)
	if err != nil || changedStatus.IsError {
		t.Fatalf("changed workspace status: %v", err)
	}
	changedFreshness := decode("mce4d_workspace_status", changedStatus)
	if changedFreshness.Status != "partial" || changedFreshness.Freshness.Status != "changed" || changedFreshness.Freshness.Scope != "workspace" || changedFreshness.Freshness.ChangedCount != nil {
		t.Fatal("changed workspace was reported current or with a guessed count")
	}
	if validation, err := checkedPlan.Validate(ctx, checkedPlanToken, principal.Digest, planGuard); !errors.Is(err, app.ErrFreshnessChanged) || validation.Data.EditPreconditionValidated {
		t.Fatalf("changed workspace validated for plan: %v", err)
	}
	wiredChanged, err = checkedPlanService.Call(ctx, "mce4d_validate_pack", wiredCheckedValidation, options)
	if err != nil || !wiredChanged.IsError || decode("mce4d_validate_pack", wiredChanged).Error.Code != "SOURCE_PRECONDITION_FAILED" {
		t.Fatalf("wired plan validated changed workspace: %v %+v", err, wiredChanged)
	}
	checkedCursor := *checkedResponse.Pagination.NextCursor
	changedBlocked := false
	assertUnchangedRefs := func(refs []model.SourceRef) {
		t.Helper()
		for _, source := range refs {
			if source.Path == "main.go" {
				t.Fatal("changed returned file leaked through continuation")
			}
		}
	}
	for pages := 0; pages < 100; pages++ {
		checkedExpandRaw := scopeJSON(t, model.ExpandRequest{PackToken: checkedData.PackToken, NextCursor: checkedCursor, MaxOutputBytes: int64Pointer(16384)})
		changedPage, err := checkedService.Call(ctx, "mce4d_expand_pack", checkedExpandRaw, options)
		if err != nil {
			t.Fatal(err)
		}
		response := decode("mce4d_expand_pack", changedPage)
		if changedPage.IsError {
			if response.Error.Code != "SOURCE_PRECONDITION_FAILED" {
				t.Fatalf("unexpected changed-source error: %s", response.Error.Code)
			}
			changedBlocked = true
			break
		}
		for _, gap := range append(append([]model.Gap{}, response.Coverage.Gaps...), response.Warnings...) {
			assertUnchangedRefs(gap.SourceRefs)
		}
		for _, item := range response.Data.(model.PackData).Items {
			assertUnchangedRefs(item.SourceRefs)
			if item.FragmentOf != nil {
				assertUnchangedRefs([]model.SourceRef{*item.FragmentOf})
			}
			for _, evidence := range item.EvidenceRefs {
				assertUnchangedRefs(evidence.SourceRefs)
			}
			for _, limitation := range item.Limitations {
				assertUnchangedRefs(limitation.SourceRefs)
			}
		}
		if response.Pagination.NextCursor == nil || *response.Pagination.NextCursor == checkedCursor {
			t.Fatal("changed source was never reached or cursor did not advance")
		}
		checkedCursor = *response.Pagination.NextCursor
	}
	if !changedBlocked {
		t.Fatal("changed source did not block continuation")
	}
	changedResult, err := checkedService.Call(ctx, "mce4d_validate_pack", checkedValidateRaw, options)
	if err != nil || !changedResult.IsError || decode("mce4d_validate_pack", changedResult).Error.Code != "SOURCE_PRECONDITION_FAILED" {
		t.Fatalf("changed workspace validated: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	filesFirst, err := service.Call(ctx, "mce4d_locate", []byte(`{"mode":"file","query":".go","limit":1}`), options)
	if err != nil || filesFirst.IsError {
		t.Fatalf("file locate failed: %v %+v", err, filesFirst)
	}
	filesPage := decode("mce4d_locate", filesFirst)
	fileCandidates := filesPage.Data.(model.LocateData).Candidates
	if len(fileCandidates) != 1 || filesPage.Pagination.NextCursor == nil {
		t.Fatal("file locate did not return a signed continuation")
	}
	fileRef, ok := fileCandidates[0].Ref.(*model.FileRef)
	if !ok || fileRef.ContentHash == nil {
		t.Fatal("file locate omitted captured identity")
	}
	fileNextRequest := scopeJSON(t, model.LocateRequest{Mode: "file", Query: stringPointer(".go"), Limit: 1, Cursor: filesPage.Pagination.NextCursor})
	filesNext, err := service.Call(ctx, "mce4d_locate", fileNextRequest, options)
	if err != nil || filesNext.IsError {
		t.Fatalf("file locate continuation failed: %v %+v", err, filesNext)
	}
	filesPage2 := decode("mce4d_locate", filesNext)
	secondCandidates := filesPage2.Data.(model.LocateData).Candidates
	if len(secondCandidates) != 1 || filesPage2.Pagination.NextCursor != nil || secondCandidates[0].Ref.(*model.FileRef).Path == fileRef.Path {
		t.Fatal("file locate duplicated or omitted a captured file")
	}
	changedFileRequest := scopeJSON(t, model.LocateRequest{Mode: "file", Query: stringPointer("main"), Limit: 1, Cursor: filesPage.Pagination.NextCursor})
	invalidFileCursor, err := service.Call(ctx, "mce4d_locate", changedFileRequest, options)
	if err != nil || !invalidFileCursor.IsError || decode("mce4d_locate", invalidFileCursor).Error.Code != "CURSOR_INVALID" {
		t.Fatalf("file cursor accepted another query: %v", err)
	}
	for _, query := range []string{"MAIN.GO", "%"} {
		request := scopeJSON(t, model.LocateRequest{Mode: "file", Query: &query, Limit: 10})
		empty, err := service.Call(ctx, "mce4d_locate", request, options)
		if err != nil || empty.IsError || len(decode("mce4d_locate", empty).Data.(model.LocateData).Candidates) != 0 {
			t.Fatalf("file query was not literal and case-sensitive: %v", err)
		}
	}
	blockedPath = "other.go"
	deniedFiles, err := service.Call(ctx, "mce4d_locate", []byte(`{"mode":"file","query":".go","limit":1}`), options)
	if err != nil || !deniedFiles.IsError || decode("mce4d_locate", deniedFiles).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("file locate hid a denied match: %v", err)
	}
	blockedPath = ""
	unsupported, err := service.Call(ctx, "mce4d_locate", []byte(`{"mode":"file","query":"main","kinds":["file"]}`), options)
	if err != nil || !unsupported.IsError || decode("mce4d_locate", unsupported).Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("unsupported file filter was not explicit: %v", err)
	}
	modules, err := service.Call(ctx, "mce4d_locate", []byte(`{"mode":"module","query":"example.invalid"}`), options)
	if err != nil || modules.IsError {
		t.Fatalf("module locate failed: %v %+v", err, modules)
	}
	modulePage := decode("mce4d_locate", modules)
	moduleCandidates := modulePage.Data.(model.LocateData).Candidates
	if len(moduleCandidates) != 1 || modulePage.Pagination.NextCursor != nil || moduleCandidates[0].Ref.(*model.ModuleRef).ModuleID != moduleID || len(moduleCandidates[0].SourceRefs) != 1 || moduleCandidates[0].SourceRefs[0].Path != "go.mod" {
		t.Fatal("module locate lost its declared identity or source")
	}
	blockedPath = "go.mod"
	deniedModules, err := service.Call(ctx, "mce4d_locate", []byte(`{"mode":"module","query":"example.invalid"}`), options)
	if err != nil || !deniedModules.IsError || decode("mce4d_locate", deniedModules).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("module locate returned a revoked source: %v %+v", err, deniedModules)
	}
	blockedPath = ""
	unsupported, err = service.Call(ctx, "mce4d_locate", []byte(`{"mode":"module","query":"fixture","kinds":["module"]}`), options)
	if err != nil || !unsupported.IsError || decode("mce4d_locate", unsupported).Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("unsupported module filter was not explicit: %v", err)
	}
	allowed = false
	blocked, err := service.Call(ctx, "mce4d_workspace_status", []byte(`{}`), options)
	if err != nil || !blocked.IsError || decode("mce4d_workspace_status", blocked).Data != nil {
		t.Fatalf("revoked principal received status: %v", err)
	}
	if result, err := reader.Recheck(ctx, principal, first.Vector); !errors.Is(err, denied) || len(result.Vector.Members) != 0 {
		t.Fatalf("revocation leaked scope: %+v %v", result, err)
	}
	allowed = true
	calls := 0
	reader.Access = func(ctx context.Context, p app.CorePrincipal, worktree, profile string) error {
		if err := access(ctx, p, worktree, profile); err != nil {
			return err
		}
		calls++
		if calls == 7 {
			_, err := recordsDB.ExecContext(ctx, `UPDATE records_meta SET record_epoch=record_epoch+1`)
			return err
		}
		return nil
	}
	if result, err := reader.Resolve(ctx, principal, ""); !errors.Is(err, ErrScopeChanged) || len(result.Vector.Members) != 0 {
		t.Fatalf("cross-store change accepted: %+v %v (calls %d)", result, err, calls)
	}
	reader.Access = access
	if _, err := reader.Recheck(ctx, principal, first.Vector); !errors.Is(err, ErrScopeChanged) {
		t.Fatalf("record epoch change accepted: %v", err)
	}
	staleExpandRaw, err := json.Marshal(model.ExpandRequest{PackToken: packData.PackToken, NextCursor: packCursor})
	if err != nil {
		t.Fatal(err)
	}
	staleExpand, err := service.Call(ctx, "mce4d_expand_pack", staleExpandRaw, options)
	if err != nil || !staleExpand.IsError || decode("mce4d_expand_pack", staleExpand).Error.Code != "CURSOR_INVALID" {
		t.Fatalf("record change did not invalidate continuation: %v", err)
	}
	second, err := reader.Resolve(ctx, principal, scopeGeneration)
	if err != nil || second.Records.RecordEpoch != 1 {
		t.Fatalf("new scope: %+v %v", second, err)
	}
	tx, err := recordsDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO record_versions(record_id,version,record_kind,declared_status,payload_json,source_refs_json,actor_id,reason,payload_hash,created_at) VALUES('fixture',1,'decision','approved','{}','[]','fixture','fixture',?,'now')`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO record_heads(record_id,version) VALUES('fixture',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE records_meta SET record_epoch=2`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	third, err := reader.Resolve(ctx, principal, scopeGeneration)
	if err != nil || third.Records.ApprovedHeads != 1 || third.Records.RecordEpoch != 2 {
		t.Fatalf("approved heads: %+v %v", third, err)
	}
	if none, err := reader.NoApprovedRecords(ctx, principal, third.Vector); err != nil || none {
		t.Fatalf("false empty proof: %v %v", none, err)
	}
	if retainedPlan != nil {
		if page, err := retainedPlan.Continue(ctx, retainedPlanCursor, packPageOptions(options)); err == nil || len(page.Blocks) != 0 {
			t.Fatal("old retained plan ignored the changed record epoch")
		}
	}
	incomplete, err := service.Call(ctx, "mce4d_context_pack", []byte(`{"selector":{"kind":"symbol","symbol_id":"target-main"},"task_kind":"explore","max_output_bytes":16384}`), options)
	if err != nil || !incomplete.IsError || decode("mce4d_context_pack", incomplete).Error.Code != "CONTEXT_INCOMPLETE" {
		t.Fatalf("unread approved record was hidden: %v", err)
	}
	incompletePlan, err := planService.Call(ctx, "mce4d_context_pack", publicRequest, options)
	if err != nil || !incompletePlan.IsError || decode("mce4d_context_pack", incompletePlan).Error.Code != "CONTEXT_INCOMPLETE" {
		t.Fatalf("wired plan hid an unread approved record: %v", err)
	}
}

func stringPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64    { return &value }
