//go:build linux

package compose

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"
	"time"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type NativeIndexProgress struct {
	GenerationID, SnapshotDigest string
	Files                        int
	Unchanged                    bool
}

func nativeMilliseconds(value int64) (time.Duration, error) {
	if value < 1 || value > int64((1<<63-1)/time.Millisecond) {
		return 0, app.ErrCoreInput
	}
	return time.Duration(value) * time.Millisecond, nil
}

// RunNativeIndex is an explicit foreground command. Watch owns one worktree
// writer, takes short SQL transactions, and releases every resource on return.
// It runs only the embedded syntax analyzer and policy-pinned Git metadata tools.
func RunNativeIndex(ctx context.Context, installation, selected, worktree, profile string, cpus int, full, watch bool, progress func(NativeIndexProgress) error) (resultErr error) {
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	n, err := openNativeScope(startup, installation, selected, worktree, profile, cpus)
	if err != nil {
		cancel()
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, n.Close()) }()
	if n.binding.Profile.AnalysisMode != "syntax" && n.binding.Profile.AnalysisMode != "typed_when_available" || n.binding.Profile.ProviderDigest != n.state.ExecutableDigest() {
		cancel()
		return app.ErrCoreUnsupported
	}
	job, err := nativeOperationID()
	if err != nil {
		cancel()
		return err
	}
	_, lease, _, err := n.stores.CacheResources(startup)
	if err != nil {
		cancel()
		return err
	}
	wait, err := nativeMilliseconds(n.effective.Core.WriterLockWaitMs)
	if err != nil {
		cancel()
		return err
	}
	writer, err := runtime.AcquireWriter(startup, lease, job, wait)
	cancel()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	deadline, err := nativeMilliseconds(n.effective.Core.IndexTimeoutMs)
	if err != nil {
		return err
	}
	poll, err := nativeMilliseconds(n.effective.Core.WatchPollIntervalMs)
	if err != nil {
		return err
	}
	for {
		bounded, cancel := context.WithTimeout(ctx, deadline)
		result, err := n.indexOnce(bounded, writer, job, full)
		cancel()
		if err != nil {
			return err
		}
		if progress != nil {
			if err := progress(result); err != nil {
				return err
			}
		}
		if !watch {
			return nil
		}
		full = false
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type nativeCapture struct {
	snapshot *snapshot.Snapshot
	files    inventory.FileMap
	blocked  map[string]string
}

func (i *nativeInventory) capture(ctx context.Context) (nativeCapture, error) {
	files, err := i.Observe(ctx)
	if err != nil {
		return nativeCapture{}, err
	}
	maximum := minInt64(i.scope.binding.Policy.MaxFileBytes, i.scope.effective.Core.MaxFileBytes, 2<<20)
	totalLimit := minInt64(i.scope.binding.Policy.MaxSourceBytes, i.scope.effective.Core.MaxSourceBytesPerSnapshot, 512<<20)
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: i.scope.binding.WorktreeID, ProfileID: i.scope.binding.ProfileID, PolicyDigest: i.scope.binding.PolicyDigest, ProviderDigest: i.scope.binding.Profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	captured := []snapshot.File{}
	blocked := map[string]string{}
	var total int64
	for _, entry := range files.Entries {
		if entry.State != "candidate" || entry.Directory {
			continue
		}
		file, err := inventory.CaptureFile(ctx, i.scope.target, i.request.Scanner, entry.Path, int(maximum), func(ctx context.Context, _ string) error { return i.scope.recheckBinding(ctx) })
		if err != nil {
			reason := ""
			switch {
			case errors.Is(err, policy.ErrSecretBlocked):
				reason = "SECRET_BLOCKED"
			case errors.Is(err, policy.ErrInvalidText):
				reason = "BINARY_FILE"
			case errors.Is(err, inventory.ErrCaptureLimit), errors.Is(err, policy.ErrContentLimit):
				reason = "FILE_TOO_LARGE"
			}
			if reason == "" {
				return nativeCapture{}, err
			}
			blocked[entry.Path] = reason
			continue
		}
		if int64(file.Len()) > totalLimit-total {
			return nativeCapture{}, app.ErrCoreLimit
		}
		total += int64(file.Len())
		captured = append(captured, file)
	}
	if len(blocked) > 0 {
		previous := i.request.Exclude
		i.request.Exclude = func(name string, directory bool) bool { return blocked[name] != "" || previous(name, directory) }
		files, err = i.Observe(ctx)
		if err != nil {
			return nativeCapture{}, err
		}
	}
	byPath := map[string]snapshot.File{}
	for _, file := range captured {
		byPath[file.Path()] = file
	}
	for _, entry := range files.Entries {
		item := model.ManifestFile{Path: entry.Path, Status: "excluded"}
		if entry.State == "candidate" && !entry.Directory {
			file, ok := byPath[entry.Path]
			if !ok {
				return nativeCapture{}, indexer.ErrSnapshotChanged
			}
			hash, length := file.ContentHash(), int64(file.Len())
			item.Status = "captured"
			item.ContentHash = &hash
			item.ByteLength = &length
		} else if entry.State == "missing" {
			item.Status = "missing"
		} else {
			reason := blocked[entry.Path]
			if reason == "" {
				reason = "OUTSIDE_CAPTURE_SCOPE"
			}
			item.ReasonCode = &reason
		}
		manifest.Files = append(manifest.Files, item)
	}
	manifest.DependencyInputDigests = []string{files.InputDigest}
	frozen, err := snapshot.Freeze(manifest, captured, 64<<20, totalLimit)
	return nativeCapture{snapshot: frozen, files: files, blocked: blocked}, err
}

func (i *nativeInventory) revalidate(ctx context.Context, capture nativeCapture, frozen *snapshot.Snapshot) error {
	return i.observeCaptured(ctx, capture, frozen, true)
}

func (i *nativeInventory) observeCaptured(ctx context.Context, capture nativeCapture, frozen *snapshot.Snapshot, publication bool) error {
	verify := func(ctx context.Context, manifest model.CaptureManifest) error {
		if err := i.scope.recheckBinding(ctx); err != nil {
			return err
		}
		for name, reason := range capture.blocked {
			_, err := inventory.CaptureFile(ctx, i.scope.target, i.request.Scanner, name, int(minInt64(i.scope.binding.Policy.MaxFileBytes, i.scope.effective.Core.MaxFileBytes, 2<<20)), func(ctx context.Context, _ string) error { return i.scope.recheckBinding(ctx) })
			valid := reason == "SECRET_BLOCKED" && errors.Is(err, policy.ErrSecretBlocked) || reason == "BINARY_FILE" && errors.Is(err, policy.ErrInvalidText) || reason == "FILE_TOO_LARGE" && (errors.Is(err, inventory.ErrCaptureLimit) || errors.Is(err, policy.ErrContentLimit))
			if !valid {
				return indexer.ErrSnapshotChanged
			}
		}
		return nil
	}
	request := indexer.RevalidateRequest{Inventory: i.request, Expected: capture.files, Snapshot: frozen, MaxManifestBytes: 64 << 20, MaxFileBytes: int(minInt64(i.scope.binding.Policy.MaxFileBytes, i.scope.effective.Core.MaxFileBytes, 2<<20)), MaxSourceBytes: minInt64(i.scope.binding.Policy.MaxSourceBytes, i.scope.effective.Core.MaxSourceBytesPerSnapshot, 512<<20), VerifyAuthority: verify, VerifyDependencies: func(ctx context.Context, digests []string) error {
		if len(digests) != 1 || digests[0] != capture.files.InputDigest {
			return indexer.ErrSnapshotChanged
		}
		return i.scope.recheckBinding(ctx)
	}}
	if i.linked != nil {
		if !publication {
			return indexer.ObserveCapturedLinked(ctx, request, i.linked, i.scope.common)
		}
		return indexer.RevalidateLinked(ctx, request, i.linked, i.scope.common)
	}
	if !publication {
		return indexer.ObserveCapturedLocal(ctx, request)
	}
	return indexer.RevalidateLocal(ctx, request)
}

func (n *nativeScope) indexOnce(ctx context.Context, writer *runtime.Writer, job string, full bool) (_ NativeIndexProgress, resultErr error) {
	i, err := openNativeInventory(ctx, n, true)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, i.Close()) }()
	captured, err := i.capture(ctx)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	if err := i.revalidate(ctx, captured, captured.snapshot); err != nil {
		return NativeIndexProgress{}, err
	}
	store, err := n.stores.Index.Store()
	if err != nil {
		return NativeIndexProgress{}, err
	}
	authorize := func(ctx context.Context, w, p, d string, e int64) error {
		if w != n.binding.WorktreeID || p != n.binding.ProfileID || d != n.binding.PolicyDigest || e != n.binding.PolicyEpoch {
			return ErrScopeChanged
		}
		return n.recheckBinding(ctx)
	}
	manifest := captured.snapshot.Manifest()
	authorizePath := func(ctx context.Context, w, name, d string, e int64) error {
		if w != n.binding.WorktreeID || d != n.binding.PolicyDigest || e != n.binding.PolicyEpoch || policy.CheckConfiguredPath(name, n.binding.Policy.DeniedGlobs) != nil {
			return ErrScopeChanged
		}
		return n.recheckBinding(ctx)
	}
	// A writer may inspect the old head's metadata after policy administration
	// so it can replace a stale generation. No old source/fact is reused under
	// the new policy, and the public query authorizer remains strict.
	previous, err := store.ReadGeneration(ctx, n.binding.ProfileID, "", func(ctx context.Context, w, p, _ string, _ int64) error {
		if w != n.binding.WorktreeID || p != n.binding.ProfileID {
			return ErrScopeChanged
		}
		return n.recheckBinding(ctx)
	}, 64<<20)
	if err != nil && !errors.Is(err, index.ErrSnapshotUnavailable) {
		return NativeIndexProgress{}, err
	}
	if err == nil && !full && previous.SnapshotDigest == manifest.SnapshotDigest && previous.PolicyDigest == n.binding.PolicyDigest && previous.PolicyEpoch == n.binding.PolicyEpoch && previous.ProviderDigest == n.binding.Profile.ProviderDigest {
		return NativeIndexProgress{GenerationID: previous.GenerationID, SnapshotDigest: manifest.SnapshotDigest, Files: len(manifest.Files), Unchanged: true}, nil
	}
	generation, err := nativeOperationID()
	if err != nil {
		return NativeIndexProgress{}, err
	}
	write := index.CaptureWrite{GenerationID: generation, JobID: job, PolicyEpoch: n.binding.PolicyEpoch, Snapshot: captured.snapshot, Scanner: i.request.Scanner, Authorize: authorizePath, Writer: writer, MaxManifestBytes: 64 << 20, MaxSourceBytes: minInt64(n.binding.Policy.MaxSourceBytes, n.effective.Core.MaxSourceBytesPerSnapshot, 512<<20)}
	if err := store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: generation, JobID: job, PolicyEpoch: n.binding.PolicyEpoch, Snapshot: captured.snapshot, Writer: writer, Authorize: authorize, MaxManifestBytes: 64 << 20}); err != nil {
		return NativeIndexProgress{}, err
	}
	var stream index.ProviderStageStream
	published := false
	started := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !published && !errors.Is(resultErr, sqliteowner.ErrNativeOutcomeUnknown) && !errors.Is(resultErr, index.ErrCommitUnknown) && !errors.Is(resultErr, index.ErrAfterCommit) {
			resultErr = errors.Join(resultErr, store.CancelBuildingGeneration(cleanup, index.CancelBuildingRequest{Capture: write, Stream: stream, Authorize: authorize}))
		}
		if started {
			resultErr = errors.Join(resultErr, store.ReleaseProviderStage(cleanup, write, stream, authorize))
		}
	}()
	classes := make([]index.FileClassification, len(manifest.Files))
	for j, file := range manifest.Files {
		kind := "text"
		generated := false
		if strings.HasSuffix(file.Path, ".go") {
			kind = "go"
			if source, ok := captured.snapshot.File(file.Path); ok {
				parsed, _ := parser.ParseFile(token.NewFileSet(), file.Path, source.Bytes(), parser.PackageClauseOnly|parser.ParseComments)
				generated = parsed != nil && ast.IsGenerated(parsed)
			}
		}
		classes[j] = index.FileClassification{Path: file.Path, Kind: kind, Generated: generated}
	}
	for offset := 0; offset < len(classes); offset += 256 {
		end := min(offset+256, len(classes))
		if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: write, Offset: offset, Files: classes[offset:end]}); err != nil {
			return NativeIndexProgress{}, err
		}
	}
	stream, err = store.BeginProviderStage(ctx, write, authorize)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	started = true
	sequence := int64(1)
	packages := map[string]bool{}
	analyzed := map[string]string{}
	modules := nativeSyntaxModules(captured.snapshot, n.binding.Profile)
	var unavailableModules int64
	for _, file := range manifest.Files {
		if file.Status != "captured" || !strings.HasSuffix(file.Path, ".go") || strings.HasSuffix(file.Path, "_test.go") && !n.binding.Profile.TestsEnabled {
			continue
		}
		source, _ := captured.snapshot.File(file.Path)
		parsed, _ := parser.ParseFile(token.NewFileSet(), file.Path, source.Bytes(), parser.PackageClauseOnly)
		packageName := "invalid"
		if parsed != nil && parsed.Name != nil && token.IsIdentifier(parsed.Name.Name) {
			packageName = parsed.Name.Name
		}
		directory := path.Dir(file.Path)
		moduleRoot, modulePath, ok := nativeSyntaxModuleFor(modules, directory)
		if !ok {
			unavailableModules++
			continue
		}
		variant := "production"
		if strings.HasSuffix(file.Path, "_test.go") {
			variant = "internal_test"
			if strings.HasSuffix(packageName, "_test") {
				packageName = strings.TrimSuffix(packageName, "_test")
				variant = "external_test"
			}
		}
		moduleKey, err := overview.GoModuleID(n.binding.WorktreeID, path.Join(moduleRoot, "go.mod"))
		if err != nil {
			return NativeIndexProgress{}, err
		}
		parts := make([]model.CanonicalValue, 0, 5)
		for _, value := range []string{moduleKey, moduleRoot, directory, packageName, variant} {
			part, err := model.CanonicalString(value)
			if err != nil {
				return NativeIndexProgress{}, err
			}
			parts = append(parts, part)
		}
		_, packageKey, err := model.EncodeCanonical("go-syntax-package-location-v1", model.CanonicalArray(parts), 32768)
		if err != nil {
			return NativeIndexProgress{}, err
		}
		suffix := strings.TrimPrefix(strings.TrimPrefix(directory, moduleRoot), "/")
		if moduleRoot == "." {
			suffix = directory
		}
		importPath := modulePath
		if suffix != "" && suffix != "." {
			importPath += "/" + suffix
		}
		if variant == "external_test" {
			importPath += "_test"
		}
		result, err := golanganalysis.AnalyzeSyntaxFile(ctx, golanganalysis.SyntaxInput{Snapshot: captured.snapshot, Profile: n.binding.Profile, Path: file.Path, PackageKey: packageKey, ImportPath: importPath, ModuleKey: moduleKey, PackageName: packageName, Variant: variant, Scanner: i.request.Scanner, MaxManifestBytes: 64 << 20, MaxFileBytes: 2 << 20, MaxNodes: 1000000, MaxDepth: 512, MaxFacts: 100000, MaxOutputBytes: 16 << 20})
		if err != nil {
			return NativeIndexProgress{}, err
		}
		batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: sequence, Packages: []model.PackageFact{}, Symbols: result.Symbols, Relations: []model.RelationFact{}, Diagnostics: result.Diagnostics}
		if !packages[packageKey] {
			batch.Packages = append(batch.Packages, result.Package)
			packages[packageKey] = true
		}
		if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: write, Stream: stream, Batch: batch, MaxFrameBytes: 16 << 20}); err != nil {
			return NativeIndexProgress{}, err
		}
		sequence++
		analyzed[file.Path] = result.ContentHash
		if len(result.Diagnostics) > 0 {
			failures := []index.ParseFailure{}
			for _, d := range result.Diagnostics {
				if d.Code == "GO_PARSE_ERROR" {
					failures = append(failures, index.ParseFailure{Path: file.Path, DiagnosticID: d.DiagnosticID})
				}
			}
			if len(failures) > 0 {
				if err := store.MarkParseErrors(ctx, index.MarkParseErrorsRequest{Capture: write, Failures: failures}); err != nil {
					return NativeIndexProgress{}, err
				}
			}
		}
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Sequence: sequence, Status: "partial", InputDigest: manifest.SnapshotDigest, Capabilities: []model.Capability{{Name: "go_syntax", ProviderID: "embedded-go-syntax", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "BUILD_SELECTION_UNVERIFIED", Reason: "Captured syntax only; build selection, import identity and typed dependency closure are not verified.", SourceRefs: []model.SourceRef{}}}}
	if unavailableModules > 0 {
		done.Gaps = append(done.Gaps, model.Gap{Code: "MODULE_MANIFEST_UNAVAILABLE", Reason: "Captured Go files without a valid configured nearest module were not assigned an import identity.", Count: &unavailableModules, SourceRefs: []model.SourceRef{}})
	}
	if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: write, Stream: stream, Done: done, MaxFrameBytes: 16 << 20, AuthorizeGeneration: authorize}); err != nil {
		return NativeIndexProgress{}, err
	}
	err = store.PublishProvider(ctx, index.PublishProviderRequest{Capture: write, Stream: stream, AuthorizeGeneration: authorize, Revalidate: func(ctx context.Context, snap *snapshot.Snapshot) error { return i.revalidate(ctx, captured, snap) }, VerifyProvider: func(ctx context.Context, snap *snapshot.Snapshot, observed model.ProviderDone) error {
		if observed.InputDigest != manifest.SnapshotDigest || observed.JobID != job || observed.Sequence != sequence || snap.Manifest().SnapshotDigest != manifest.SnapshotDigest {
			return index.ErrCaptureMismatch
		}
		for name, hash := range analyzed {
			file, ok := snap.File(name)
			if !ok || file.ContentHash() != hash {
				return index.ErrCaptureMismatch
			}
		}
		return n.recheckBinding(ctx)
	}, MaxFacts: 1000000, MaxFrameBytes: 16 << 20})
	if err != nil {
		return NativeIndexProgress{}, err
	}
	published = true
	return NativeIndexProgress{GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, Files: len(manifest.Files)}, nil
}

type nativeSyntaxModule struct {
	importPath string
	configured bool
}

func nativeSyntaxModules(captured *snapshot.Snapshot, profile *model.GoProfile) map[string]nativeSyntaxModule {
	configured := map[string]bool{}
	for _, root := range profile.ModuleRoots {
		configured[root] = true
	}
	modules := map[string]nativeSyntaxModule{}
	for _, entry := range captured.Manifest().Files {
		if path.Base(entry.Path) != "go.mod" {
			continue
		}
		root := path.Dir(entry.Path)
		item := nativeSyntaxModule{configured: configured[root]}
		if entry.Status == "captured" {
			if file, ok := captured.File(entry.Path); ok {
				parsed, err := modfile.Parse(entry.Path, file.Bytes(), nil)
				if err == nil && parsed.Module != nil && module.CheckPath(parsed.Module.Mod.Path) == nil && len(parsed.Module.Mod.Path) <= 512 {
					item.importPath = parsed.Module.Mod.Path
				}
			}
		}
		modules[root] = item
	}
	return modules
}

func nativeSyntaxModuleFor(modules map[string]nativeSyntaxModule, directory string) (string, string, bool) {
	for current := directory; ; current = path.Dir(current) {
		if item, ok := modules[current]; ok {
			return current, item.importPath, item.configured && item.importPath != ""
		}
		if current == "." {
			return "", "", false
		}
	}
}
