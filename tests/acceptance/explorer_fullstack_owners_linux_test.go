//go:build linux

package acceptance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	domain "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/ts-provider"
)

const fullstackBytes = 65536

type fullstackMember struct {
	root, source, job string
	facts             *index.Store
	writer            *runtime.Writer
	capture           *snapshot.Snapshot
	scanner           *policy.Scanner
	member            model.ScopeMember
}

func bufferSHA(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func makeFullstackMember(t *testing.T, ctx context.Context, root, service, worktree, generation, job string,
	profile model.EffectiveProfile, provider string, dependencies []string, files map[string]string,
) *fullstackMember {
	t.Helper()
	profileID, err := model.ProfileDigest(profile, fullstackBytes)
	requireFullstack(t, err)
	scanner, err := policy.NewScanner(fullstackBytes)
	requireFullstack(t, err)
	m := &fullstackMember{root: root, source: filepath.Join(root, "input"), job: job, scanner: scanner}
	capture := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profileID,
		PolicyDigest: bufferSHA([]byte("fixture policy " + service)), ProviderDigest: provider,
		Files: []model.ManifestFile{}, DependencyInputDigests: dependencies}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var frozen []snapshot.File
	for _, name := range names {
		file, err := snapshot.NewFile(name, []byte(files[name]), fullstackBytes)
		requireFullstack(t, err)
		out := filepath.Join(m.source, filepath.FromSlash(name))
		requireFullstack(t, os.MkdirAll(filepath.Dir(out), 0700))
		requireFullstack(t, os.WriteFile(out, file.Bytes(), 0600))
		hash, length := file.ContentHash(), int64(file.Len())
		capture.Files = append(capture.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &length})
		frozen = append(frozen, file)
	}
	m.capture, err = snapshot.Freeze(capture, frozen, fullstackBytes, fullstackBytes)
	requireFullstack(t, err)
	state := filepath.Join(root, "state")
	requireFullstack(t, os.Mkdir(state, 0700))
	stateRoot, err := paths.OpenRoot(state)
	requireFullstack(t, err)
	t.Cleanup(func() { requireFullstack(t, stateRoot.Close()) })
	lease, err := runtime.AcquireLifecycle(ctx, stateRoot, worktree, ports.ExclusiveLease, time.Second)
	requireFullstack(t, err)
	t.Cleanup(func() { requireFullstack(t, lease.Close()) })
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(state, "index.sqlite")}).String()
	db, err := sql.Open("sqlite", dsn)
	requireFullstack(t, err)
	t.Cleanup(func() { requireFullstack(t, db.Close()) })
	db.SetMaxOpenConns(1)
	identity, err := index.Initialize(ctx, db, worktree, lease)
	requireFullstack(t, err)
	m.writer, err = runtime.AcquireWriter(ctx, lease, job, time.Second)
	requireFullstack(t, err)
	writer := m.writer
	t.Cleanup(func() { requireFullstack(t, writer.Close()) })
	m.facts, err = index.Open(ctx, db, db, worktree, lease)
	requireFullstack(t, err)
	m.member = model.ScopeMember{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, ServiceID: &service,
		SnapshotDigest: m.capture.Manifest().SnapshotDigest, PolicyDigest: capture.PolicyDigest,
		PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1,
		IndexIncarnation: identity.Incarnation, RecordsIncarnation: generation}
	return m
}

func (m *fullstackMember) authorizeGeneration(ctx context.Context, worktree, profile, digest string, epoch int64) error {
	pin := m.member
	if worktree != pin.WorktreeID || profile != pin.ProfileID || digest != pin.PolicyDigest || epoch != pin.PolicyEpoch {
		return index.ErrCaptureMismatch
	}
	return ctx.Err()
}

func (m *fullstackMember) authorizePath(ctx context.Context, worktree, path, digest string, epoch int64) error {
	if _, ok := m.capture.File(path); !ok {
		return index.ErrCaptureMismatch
	}
	return m.authorizeGeneration(ctx, worktree, m.member.ProfileID, digest, epoch)
}

func (m *fullstackMember) publish(t *testing.T, ctx context.Context,
	produce func(func(context.Context, model.ProviderBatch) error) (model.ProviderDone, error),
) {
	t.Helper()
	stage := index.CaptureWrite{GenerationID: m.member.GenerationID, JobID: m.job, PolicyEpoch: 1,
		Snapshot: m.capture, Scanner: m.scanner, Authorize: m.authorizePath, Writer: m.writer,
		MaxManifestBytes: fullstackBytes, MaxSourceBytes: fullstackBytes}
	requireFullstack(t, m.facts.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: stage.GenerationID,
		JobID: stage.JobID, PolicyEpoch: 1, Snapshot: m.capture, Writer: m.writer,
		Authorize: m.authorizeGeneration, MaxManifestBytes: fullstackBytes}))
	var classes []index.FileClassification
	for _, file := range m.capture.Manifest().Files {
		classes = append(classes, index.FileClassification{Path: file.Path, Kind: "text"})
	}
	requireFullstack(t, m.facts.StageFiles(ctx, index.StageFilesRequest{Capture: stage, Files: classes}))
	stream, err := m.facts.BeginProviderStage(ctx, stage, m.authorizeGeneration)
	requireFullstack(t, err)
	done, err := produce(func(ctx context.Context, batch model.ProviderBatch) error {
		_, err := m.facts.StageProviderBatch(ctx, index.StageProviderRequest{Capture: stage,
			Stream: stream, Batch: batch, MaxFrameBytes: fullstackBytes})
		return err
	})
	requireFullstack(t, err)
	requireFullstack(t, m.facts.SealProviderStage(ctx, index.SealProviderRequest{Capture: stage,
		Stream: stream, Done: done, MaxFrameBytes: fullstackBytes, AuthorizeGeneration: m.authorizeGeneration}))
	requireFullstack(t, m.facts.PublishProvider(ctx, index.PublishProviderRequest{Capture: stage, Stream: stream,
		MaxFrameBytes: fullstackBytes, MaxFacts: 500, AuthorizeGeneration: m.authorizeGeneration,
		Revalidate: func(ctx context.Context, s *snapshot.Snapshot) error { return m.checkCapture(ctx, s) },
		VerifyProvider: func(ctx context.Context, s *snapshot.Snapshot, terminal model.ProviderDone) error {
			if terminal.JobID != m.job || terminal.VerifyInputDigest(s.Manifest().SnapshotDigest) != nil {
				return index.ErrCaptureMismatch
			}
			return ctx.Err()
		}}))
	requireFullstack(t, m.facts.ReleaseProviderStage(ctx, stage, stream, m.authorizeGeneration))
}

func (m *fullstackMember) checkCapture(ctx context.Context, s *snapshot.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Manifest().SnapshotDigest != m.member.SnapshotDigest {
		return index.ErrCaptureMismatch
	}
	for _, entry := range s.Manifest().Files {
		raw, err := os.ReadFile(filepath.Join(m.source, filepath.FromSlash(entry.Path)))
		if err != nil {
			return err
		}
		if entry.ContentHash == nil || bufferSHA(raw) != *entry.ContentHash {
			return index.ErrCaptureMismatch
		}
	}
	return nil
}

func (m *fullstackMember) sourceRef(name string) model.SourceRef {
	file, _ := m.capture.File(name)
	return model.SourceRef{WorktreeID: m.member.WorktreeID, ProfileID: m.member.ProfileID, GenerationID: m.member.GenerationID,
		Path: name, ContentHash: file.ContentHash(), Range: model.ByteRange{EndByte: int64(file.Len())}}
}

func (m *fullstackMember) readSource(ctx context.Context, ref model.SourceRef) (string, error) {
	pin := m.member
	if ref.WorktreeID != pin.WorktreeID || ref.ProfileID != pin.ProfileID || ref.GenerationID != pin.GenerationID {
		return "", index.ErrCaptureMismatch
	}
	file, err := m.facts.ReadPublishedFile(ctx, index.PublishedFileRequest{ProfileID: pin.ProfileID, GenerationID: pin.GenerationID,
		Path: ref.Path, AuthorizeGeneration: m.authorizeGeneration, AuthorizePath: m.authorizePath,
		Scanner: m.scanner, MaxManifestBytes: fullstackBytes, MaxFileBytes: fullstackBytes})
	if err != nil {
		return "", err
	}
	if file.IndexIncarnation != pin.IndexIncarnation || file.SnapshotDigest != pin.SnapshotDigest ||
		file.PolicyDigest != pin.PolicyDigest || file.PolicyEpoch != pin.PolicyEpoch || file.ContentHash != ref.ContentHash ||
		ref.ValidateCapturedBytes(file.Bytes) != nil {
		return "", index.ErrCaptureMismatch
	}
	return string(file.Bytes[ref.Range.StartByte:ref.Range.EndByte]), nil
}

func makeFrontendOwner(t *testing.T, ctx context.Context, files map[string]string) (*fullstackMember, domain.FrontendAnalysis, model.SymbolFact) {
	t.Helper()
	compiler, node := os.Getenv("MCE4D_T044_COMPILER"), os.Getenv("MCE4D_T055_NODE")
	if compiler == "" || node == "" {
		t.Skip("requires the pinned T043 TypeScript and Node installation")
	}
	root := t.TempDir()
	frontend, profile, err := tsprovider.NewFrontend(tsprovider.Installation{Node: node, ScratchParent: root,
		CompilerRoot: filepath.Join(compiler, "compiler"), CompilerArchive: filepath.Join(compiler, "typescript-5.9.3.tgz"),
		CompilerManifest: filepath.Join(compiler, "compiler-files.json")}, []string{"app/tsconfig.json"}, "typed_when_available")
	requireFullstack(t, err)
	m := makeFullstackMember(t, ctx, root, "web", stackUUID(1), stackUUID(11), stackUUID(21),
		profile, profile.ProviderDigest, []string{}, files)
	raw, err := json.Marshal(m.capture.Manifest())
	requireFullstack(t, err)
	manifestPath := filepath.Join(m.source, "manifest.json")
	requireFullstack(t, os.WriteFile(manifestPath, raw, 0600))
	deadline, _ := ctx.Deadline()
	job := ports.ProviderJob{Start: model.ProviderStart{ProviderProtocol: 1, Type: "analyze_start",
		JobID: m.job, GenerationID: m.member.GenerationID, ProviderID: tsprovider.ProviderID, ProviderDigest: profile.ProviderDigest,
		Profile: profile, ProfileID: m.member.ProfileID, SnapshotRoot: m.source, ManifestPath: manifestPath,
		ManifestDigest: bufferSHA(raw), MaxFrameBytes: fullstackBytes, DeadlineAt: deadline.UTC().Format(time.RFC3339Nano)},
		Capture: providers.SnapshotInput(m.capture), Recheck: func(ctx context.Context) error { return m.checkCapture(ctx, m.capture) }}
	analysis, err := frontend.Scan(ctx, job)
	requireFullstack(t, err)
	var owner model.SymbolFact
	m.publish(t, ctx, func(sink func(context.Context, model.ProviderBatch) error) (model.ProviderDone, error) {
		return frontend.Analyze(ctx, job, func(ctx context.Context, batch model.ProviderBatch) error {
			for _, fact := range batch.Symbols {
				if fact.Name == "placeOrder" && fact.Kind == "function" && fact.Path != nil && *fact.Path == "app/orders.tsx" {
					owner = fact
				}
			}
			return sink(ctx, batch)
		})
	})
	return m, analysis, owner
}

func makeGoOwner(t *testing.T, ctx context.Context, files map[string]string) (*fullstackMember, *flowview.Program) {
	t.Helper()
	root := t.TempDir()
	goRoot := stdruntime.GOROOT()
	goPath := filepath.Join(goRoot, "bin", "go")
	t.Setenv("PATH", filepath.Dir(goPath))
	bin, err := os.ReadFile(goPath)
	requireFullstack(t, err)
	toolchain := bufferSHA(bin)
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{},
		ModuleRoots: []string{"."}, ToolchainDigest: toolchain, ProviderDigest: bufferSHA([]byte("guarded Go fixture provider")),
		ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{}}
	m := makeFullstackMember(t, ctx, root, "orders", stackUUID(2), stackUUID(12), stackUUID(22),
		profile, profile.ProviderDigest, []string{toolchain}, files)
	for _, dir := range []string{"cache", "modcache", "gopath", "home", "tmp"} {
		requireFullstack(t, os.Mkdir(filepath.Join(root, dir), 0700))
	}
	binRoot, err := paths.OpenRoot(filepath.Dir(goPath))
	requireFullstack(t, err)
	defer binRoot.Close()
	tool, err := runtime.OpenGoExecutable(ctx, runtime.GoExecutableInput{BinRoot: binRoot, ExpectedSHA256: toolchain,
		MaxBytes: 128 << 20, Authorize: func(ctx context.Context) error { return ctx.Err() }})
	requireFullstack(t, err)
	defer tool.Close()
	env, _, err := golang.LoaderEnvironment(profile, goRoot, root, m.source)
	requireFullstack(t, err)
	list := exec.CommandContext(ctx, goPath, "list", "-deps", "-f", "{{if .Standard}}{{.ImportPath}}{{end}}", "net/http", "database/sql")
	list.Dir, list.Env = root, env
	allowed, err := list.Output()
	requireFullstack(t, err)
	stdRoot, err := paths.OpenRoot(filepath.Join(goRoot, "src"))
	requireFullstack(t, err)
	defer stdRoot.Close()
	stdScanner, err := policy.NewScanner(2 << 20)
	requireFullstack(t, err)
	stdlib, err := golang.NewTrustedStdlib(ctx, golang.TrustedStdlibInput{SourceRoot: stdRoot, GoRoot: goRoot,
		Profile: profile, Snapshot: m.capture, DependencyDigest: toolchain, AllowedPackages: strings.Fields(string(allowed)),
		Scanner: stdScanner, MaxManifestBytes: fullstackBytes, MaxFileBytes: 2 << 20, MaxFiles: 8192, MaxSourceBytes: 64 << 20,
		AuthorizePath: func(ctx context.Context, _ string) error { return ctx.Err() },
		VerifyAuthority: func(ctx context.Context, _, _, digest string, _ []string) (bool, error) {
			return digest == toolchain, ctx.Err()
		}})
	requireFullstack(t, err)
	scratch, err := paths.OpenRoot(root)
	requireFullstack(t, err)
	defer scratch.Close()
	var mapping inventory.FileMap
	for _, file := range m.capture.Manifest().Files {
		mapping.Entries = append(mapping.Entries, inventory.FileMapEntry{Path: file.Path,
			Origin: "untracked", State: "candidate", Stages: []uint8{}})
	}
	pre := golang.LoaderPreflightInput{Snapshot: m.capture, Profile: profile, Inventory: mapping, PolicyEpoch: 1, Scanner: m.scanner,
		AuthorizeScope: m.authorizeGeneration, AuthorizePath: m.authorizePath,
		VerifyClosure: func(ctx context.Context, capture *snapshot.Snapshot, seen inventory.FileMap) (bool, error) {
			return slices.EqualFunc(seen.Entries, mapping.Entries, func(a, b inventory.FileMapEntry) bool { return a.Path == b.Path }),
				m.checkCapture(ctx, capture)
		},
		MaxManifestBytes: fullstackBytes, MaxFileBytes: fullstackBytes, MaxSourceBytes: fullstackBytes,
		VerifyDependencies: func(ctx context.Context, _ *model.GoProfile, digests []string) (bool, error) {
			return slices.Equal(digests, []string{toolchain}), ctx.Err()
		},
		VerifyToolchain: golang.LoaderToolchainVerifier(tool, goRoot, func(ctx context.Context, p *model.GoProfile) (bool, error) {
			return p.ToolchainDigest == toolchain, ctx.Err()
		})}
	load := golang.CapturedLoaderInput{Preflight: pre, GoRoot: goRoot, ScratchRoot: root, WorkspaceRoot: m.source,
		Tool: tool, Stdlib: stdlib, MaxOutputBytes: fullstackBytes,
		VerifyScratch: golang.LoaderScratchVerifier(scratch, func(ctx context.Context) error { return ctx.Err() })}
	const packageID, moduleID = "fixture-orders", "fixture-api"
	program, err := flowview.Load(ctx, flowview.Input{Graph: golang.GraphInput{Typed: golang.TypedInput{Loader: load,
		PackageKey: packageID, ModuleKey: moduleID, MaxNodes: 20000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000,
		MaxOutputBytes: 2 << 20}, MaxRelations: 1000, MaxOutputBytes: 2 << 20}, Member: m.member, ServeMuxModern: true})
	requireFullstack(t, err)
	syntax, err := golang.AnalyzeSyntaxFile(ctx, golang.SyntaxInput{Snapshot: m.capture, Profile: profile, Path: "handler.go",
		PackageKey: packageID, ImportPath: "example.invalid/orders", ModuleKey: moduleID, PackageName: "orders", Variant: "production",
		Scanner: m.scanner, MaxManifestBytes: fullstackBytes, MaxFileBytes: fullstackBytes,
		MaxNodes: 20000, MaxDepth: 256, MaxFacts: 1000, MaxOutputBytes: fullstackBytes})
	requireFullstack(t, err)
	m.publish(t, ctx, func(sink func(context.Context, model.ProviderBatch) error) (model.ProviderDone, error) {
		if err := sink(ctx, model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: m.job, Sequence: 1,
			Packages: []model.PackageFact{syntax.Package}, Symbols: syntax.Symbols, Relations: []model.RelationFact{},
			Diagnostics: append([]model.DiagnosticFact{}, syntax.Diagnostics...)}); err != nil {
			return model.ProviderDone{}, err
		}
		return model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: m.job, Sequence: 2, Status: "partial",
			InputDigest: m.member.SnapshotDigest, Capabilities: []model.Capability{{Name: "go_syntax", ProviderID: "embedded-go-syntax",
				ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "FIXTURE_CAPABILITY_SCOPE",
				Reason: "This fixture publishes Go source symbols and verifies the typed route separately.", SourceRefs: []model.SourceRef{}}}}, nil
	})
	return m, program
}
