package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	powershellprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/powershell-provider"
	pythonprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/python-provider"
	tsprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/ts-provider"
)

const (
	windowsManifestLimit   = 1 << 20
	windowsGoSyntaxVersion = "2"
)

var errWindowsSnapshotChanged = errors.New("Windows index snapshot changed during reconciliation")

func windowsCaptureChanged(err error) bool {
	return err == errWindowsSnapshotChanged || err == inventory.ErrWalkChanged || err == inventory.ErrCaptureChanged
}

// RunWindowsIndex publishes Go facts from an authorized Windows Git capture.
// Typed analysis uses the approved offline toolchain against protected scratch.
func RunWindowsIndex(ctx context.Context, installation, selected, worktree, profile string, cpus int, full, watch bool,
	progress func(NativeIndexProgress) error) error {
	if selected != "" && selected != filepath.Join(installation, "runtime.json") {
		return sqlite.ErrProfileDenied
	}
	for {
		result, poll, deferred, err := runWindowsIndexOnce(ctx, installation, worktree, profile, cpus, full, watch)
		if err != nil {
			return err
		}
		if !deferred && progress != nil {
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

func runWindowsIndexOnce(ctx context.Context, installation, worktree, profile string, cpus int, full, watch bool) (_ NativeIndexProgress, _ time.Duration, _ bool, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var poll time.Duration
	var deferred bool
	var typed *windowsTypedGo
	defer func() {
		if typed != nil {
			resultErr = errors.Join(resultErr, typed.proof.Close())
		}
	}()
	result, err := sqlite.RunWindowsIndexMutation(ctx, installation, worktree, profile, func(scope sqlite.WindowsIndexScope) (sqlite.WindowsIndexPlan[NativeIndexProgress], error) {
		var plan sqlite.WindowsIndexPlan[NativeIndexProgress]
		config, err := app.NewRuntimeConfigService(ctx, windowsIndexConfig{read: scope.ReadConfig}, cpus)
		if err != nil {
			return plan, err
		}
		effective, _, err := config.StartupConfig()
		if err != nil {
			return plan, err
		}
		poll, err = nativeMilliseconds(effective.Core.WatchPollIntervalMs)
		if err != nil {
			return plan, err
		}
		binding := scope.Binding
		if len(binding.Policy.DependencyRoots) != 0 || binding.Profile != nil &&
			(binding.Profile.Goos != "windows" || len(binding.Profile.DependencyRoots) != 0) {
			return plan, app.ErrCoreUnsupported
		}
		c, err := newWindowsIndexCapture(scope, effective.Core)
		if err != nil {
			return plan, err
		}
		frozen, err := c.capture(ctx)
		if watch && c.scope.Binding.AddonProfile != nil && windowsCaptureChanged(err) {
			frozen, err = c.capture(ctx)
		}
		if err != nil {
			return plan, err
		}
		var facts windowsFactStage
		var generation string
		if c.powershell != nil {
			facts, err = c.analyzePowerShell(ctx, installation, frozen)
			if err != nil {
				return plan, err
			}
		} else if c.addon != nil {
			generation, err = nativeOperationID()
			if err != nil {
				return plan, err
			}
			facts, err = c.analyzeAddon(ctx, installation, frozen, generation, effective.Core.MaxWorktreeCacheBytes)
			if err != nil {
				return plan, err
			}
		}
		if binding.Profile != nil && binding.Profile.AnalysisMode == "typed_when_available" {
			typed, err = c.analyzeTypedGo(ctx, frozen, effective.Core.MaxWorktreeCacheBytes)
			if watch && windowsCaptureChanged(err) {
				deferred = true
				return plan, errWindowsSnapshotChanged
			}
			if err != nil {
				return plan, err
			}
		}
		if (facts != nil || binding.Profile != nil && binding.Profile.AnalysisMode == "typed_when_available") && !full {
			// Recheck the capture before writing a receipt. If a save
			// arrived during this scan, reconcile it on the next poll.
			plan.Reuse = func(store *index.Store) (NativeIndexProgress, bool, error) {
				err := c.revalidate(ctx, frozen)
				if watch && windowsCaptureChanged(err) {
					deferred = true
					return NativeIndexProgress{}, true, nil
				}
				if err != nil {
					return NativeIndexProgress{}, false, err
				}
				current, err := c.indexProgress(ctx, store, frozen, false)
				return current, current.Unchanged, err
			}
		}
		plan.CacheLimit = effective.Core.MaxWorktreeCacheBytes
		plan.Publish = func(store *index.Store, writer *runtime.Writer, job string) (NativeIndexProgress, error) {
			return c.publish(ctx, store, writer, job, frozen, full, generation, facts, typed)
		}
		return plan, nil
	})
	if deferred && err == errWindowsSnapshotChanged {
		return NativeIndexProgress{}, poll, true, nil
	}
	if err != nil {
		err = errors.Join(err, ctx.Err())
	}
	return result, poll, deferred, err
}

type windowsIndexConfig struct {
	read func(context.Context, int) ([]byte, bool, error)
}

func (c windowsIndexConfig) Read(ctx context.Context, limit int) ([]byte, bool, error) {
	return c.read(ctx, limit)
}

func (windowsIndexConfig) Set(context.Context, []byte, int) (ports.RuntimeConfigWriteResult, error) {
	return ports.RuntimeConfigWriteResult{State: "not_applied"}, app.ErrAdminDenied
}

type windowsIndexCapture struct {
	scope               sqlite.WindowsIndexScope
	scanner             *policy.Scanner
	maxEntries, maxFile int
	maxBytes            int64
	powershell          *model.PowerShellProfile
	addon               *windowsAddon
}

func newWindowsIndexCapture(scope sqlite.WindowsIndexScope, limits model.CoreRuntimeLimits) (windowsIndexCapture, error) {
	var ps *model.PowerShellProfile
	var addon *windowsAddon
	switch profile := scope.Binding.AddonProfile.(type) {
	case *model.PowerShellProfile:
		ps = profile
	case *model.TSProfile:
		addon = &windowsAddon{directory: "typescript", executable: "node.exe", digest: profile.NodeDigest, id: tsprovider.ProviderID,
			programName: "worker.cjs", program: tsprovider.WindowsProgram(), analyze: tsprovider.AnalyzeWindows}
	case *model.PythonProfile:
		program, err := pythonprovider.WindowsProgram()
		if err != nil {
			return windowsIndexCapture{}, err
		}
		addon = &windowsAddon{directory: "python", executable: "python.exe", digest: profile.InterpreterDigest, id: pythonprovider.ProviderID,
			programName: "worker.py", program: program, analyze: pythonprovider.AnalyzeWindows}
	}
	if scope.Binding.Profile == nil && ps == nil && addon == nil {
		return windowsIndexCapture{}, app.ErrCoreUnsupported
	}
	scanner, err := policy.NewScanner(windowsManifestLimit)
	if err != nil {
		return windowsIndexCapture{}, err
	}
	policy := scope.Binding.Policy
	maxFile := min(policy.MaxFileBytes, limits.MaxFileBytes, windowsManifestLimit)
	if ps != nil {
		maxFile = min(maxFile, powershellprovider.MaxSourceBytes)
	}
	return windowsIndexCapture{scope: scope, scanner: scanner, maxEntries: int(min(limits.MaxIndexedFiles, 100000)),
		maxFile: int(maxFile), maxBytes: min(policy.MaxSourceBytes, limits.MaxSourceBytesPerSnapshot, windowsManifestLimit), powershell: ps, addon: addon}, nil
}

func (c windowsIndexCapture) capturePath(name string) bool {
	switch c.scope.Binding.AddonProfile.(type) {
	case *model.PowerShellProfile:
		return powershellprovider.SupportsPath(name)
	case *model.TSProfile:
		switch strings.ToLower(filepath.Ext(name)) {
		case ".ts", ".tsx", ".cts", ".mts", ".js", ".jsx", ".cjs", ".mjs", ".json":
			return true
		default:
			return false
		}
	case *model.PythonProfile:
		for _, component := range strings.Split(name, "/") {
			switch strings.ToLower(component) {
			case ".venv", "venv", "site-packages", "__pycache__", "setup.py", "sitecustomize.py", "usercustomize.py":
				return false
			}
		}
		return strings.EqualFold(filepath.Ext(name), ".py")
	default:
		switch strings.ToLower(filepath.Base(name)) {
		case "contributors", "codeowners":
			return true
		}
		return nativeTextPath(name)
	}
}

func (c windowsIndexCapture) authorizePath(ctx context.Context, name string) error {
	if err := policy.CheckConfiguredPath(name, c.scope.Binding.Policy.DeniedGlobs); err != nil {
		return err
	}
	return c.scope.Check(ctx)
}

func (c windowsIndexCapture) inventory(ctx context.Context) (inventory.FileMap, error) {
	b := c.scope.Binding
	return inventory.InventoryWindowsLocal(ctx, inventory.WindowsInventoryRequest{
		Root: c.scope.Target, Metadata: c.scope.CommonGit, LinkedGit: c.scope.LinkedGit, Scanner: c.scanner, MaxEntries: c.maxEntries,
		AuthorizeRoot: c.scope.Check, ReadGlobal: c.scope.ReadIgnore,
		AuthorizePath: func(ctx context.Context, name string, directory bool) (bool, error) {
			return policy.CheckConfiguredPath(name, b.Policy.DeniedGlobs) == nil, ctx.Err()
		},
		Exclude: func(name string, directory bool) bool { return !directory && !c.capturePath(name) },
	})
}

func (c windowsIndexCapture) capture(ctx context.Context) (*snapshot.Snapshot, error) {
	b := c.scope.Binding
	fileMap, err := c.inventory(ctx)
	switch {
	case errors.Is(err, inventory.ErrIndexBudget), errors.Is(err, inventory.ErrCaptureLimit),
		errors.Is(err, inventory.ErrWalkBudget), errors.Is(err, inventory.ErrIgnoreBudget), errors.Is(err, runtime.ErrGitIndexLimit):
		return nil, errors.Join(app.ErrCoreLimit, err)
	case errors.Is(err, inventory.ErrIndexUnsupported), errors.Is(err, inventory.ErrGitConfigUnsupported),
		errors.Is(err, inventory.ErrIgnoreUnsupported), errors.Is(err, os.ErrNotExist):
		return nil, errors.Join(app.ErrCoreUnsupported, err)
	case err != nil:
		return nil, err
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: b.WorktreeID, ProfileID: b.ProfileID,
		PolicyDigest: b.PolicyDigest, ProviderDigest: b.ProviderDigest(), Files: []model.ManifestFile{}, DependencyInputDigests: []string{fileMap.InputDigest}}
	files := []snapshot.File{}
	var total int64
	for _, item := range fileMap.Entries {
		if item.Directory {
			continue // Directory boundaries are bound by the inventory digest.
		}
		name := item.Path
		// Keep policy-denied names out of the provider manifest. The
		// inventory digest still binds the observed inventory, including
		// exclusions, for the pre-publication recheck.
		if b.AddonProfile != nil && policy.CheckConfiguredPath(name, b.Policy.DeniedGlobs) != nil {
			continue
		}
		if item.State == "missing" {
			manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "missing"})
			continue
		}
		excluded := model.ManifestFile{Path: name, Status: "excluded"}
		if item.State != "candidate" || item.SkipWorktree || item.IntentToAdd || !c.capturePath(name) {
			reason := "OUTSIDE_CAPTURE_SCOPE"
			excluded.ReasonCode = &reason
			manifest.Files = append(manifest.Files, excluded)
			continue
		}
		file, err := inventory.CaptureFile(ctx, c.scope.Target, c.scanner, name, c.maxFile, c.authorizePath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "missing"})
			continue
		case errors.Is(err, policy.ErrSecretBlocked), errors.Is(err, policy.ErrInvalidText), errors.Is(err, inventory.ErrCaptureLimit), errors.Is(err, policy.ErrContentLimit):
			reason := "OUTSIDE_CAPTURE_SCOPE"
			excluded.ReasonCode = &reason
			manifest.Files = append(manifest.Files, excluded)
			continue
		case err != nil:
			return nil, err
		}
		hash, length := file.ContentHash(), int64(file.Len())
		if length > c.maxBytes-total || c.powershell != nil && len(files) == powershellprovider.MaxFiles {
			return nil, app.ErrCoreLimit
		}
		total += length
		files = append(files, file)
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &length})
	}
	if err := c.scope.Check(ctx); err != nil {
		return nil, err
	}
	return snapshot.Freeze(manifest, files, windowsManifestLimit, c.maxBytes)
}

func (c windowsIndexCapture) authorizeGeneration(ctx context.Context, worktree, profile, policy string, epoch int64) error {
	b := c.scope.Binding
	if worktree != b.WorktreeID || profile != b.ProfileID || policy != b.PolicyDigest || epoch != b.PolicyEpoch {
		return ErrScopeChanged
	}
	return c.scope.Check(ctx)
}

func (c windowsIndexCapture) revalidate(ctx context.Context, expected *snapshot.Snapshot) error {
	now, err := c.capture(ctx)
	if err != nil {
		return err
	}
	if now.Manifest().SnapshotDigest != expected.Manifest().SnapshotDigest {
		return errWindowsSnapshotChanged
	}
	return nil
}

func (c windowsIndexCapture) indexProgress(ctx context.Context, store *index.Store, frozen *snapshot.Snapshot, full bool) (NativeIndexProgress, error) {
	manifest := frozen.Manifest()
	binding := c.scope.Binding
	// The index owner can inspect prior metadata to decide whether to
	// rebuild. Reuse still requires the current policy digest and epoch.
	authorizePrevious := func(ctx context.Context, worktree, profile, _ string, _ int64) error {
		if worktree != binding.WorktreeID || profile != binding.ProfileID {
			return ErrScopeChanged
		}
		return c.scope.Check(ctx)
	}
	matches := func(previous index.GenerationObservation) bool {
		return previous.SnapshotDigest == manifest.SnapshotDigest && previous.PolicyDigest == binding.PolicyDigest &&
			previous.PolicyEpoch == binding.PolicyEpoch && previous.ProviderDigest == manifest.ProviderDigest
	}
	var previous index.GenerationObservation
	var coverage model.Coverage
	var err error
	if !full && binding.Profile != nil && binding.Profile.AnalysisMode == "typed_when_available" {
		previous, coverage, err = store.ReadGenerationWithCoverage(ctx, manifest.ProfileID, "", authorizePrevious,
			windowsManifestLimit, 16<<20, c.scanner, matches)
	} else {
		previous, err = store.ReadGeneration(ctx, manifest.ProfileID, "", authorizePrevious, windowsManifestLimit)
	}
	if err != nil && !errors.Is(err, index.ErrSnapshotUnavailable) {
		return NativeIndexProgress{}, err
	}
	progress := NativeIndexProgress{SnapshotDigest: manifest.SnapshotDigest, Files: len(manifest.Files)}
	if c.powershell != nil {
		progress.ProviderID, progress.ProviderStatus = powershellprovider.ID, "partial"
	}
	if c.addon != nil {
		progress.ProviderID, progress.ProviderStatus = c.addon.id, "partial"
	}
	if err == nil && !full && matches(previous) {
		for _, capability := range coverage.Capabilities {
			if capability.Name == "go_semantics" && capability.ProviderID == "embedded-go-captured" {
				progress.Typed = true
				break
			}
		}
		progress.GenerationID, progress.Unchanged = previous.GenerationID, true
		if c.addon != nil && previous.State == "READY" {
			progress.ProviderStatus = "complete_in_supported_scope"
		}
	}
	return progress, nil
}

func (c windowsIndexCapture) publish(ctx context.Context, store *index.Store, writer *runtime.Writer, job string,
	frozen *snapshot.Snapshot, full bool, generation string, facts windowsFactStage, typed *windowsTypedGo) (_ NativeIndexProgress, resultErr error) {
	b := c.scope.Binding
	manifest := frozen.Manifest()
	authorize := c.authorizeGeneration
	if err := c.revalidate(ctx, frozen); err != nil {
		return NativeIndexProgress{}, err
	}
	progress, err := c.indexProgress(ctx, store, frozen, full)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	if progress.Unchanged {
		return progress, nil
	}
	if generation == "" {
		generation, err = nativeOperationID()
		if err != nil {
			return NativeIndexProgress{}, err
		}
	}
	write := index.CaptureWrite{GenerationID: generation, JobID: job, PolicyEpoch: b.PolicyEpoch, Snapshot: frozen, Scanner: c.scanner,
		Writer: writer, MaxManifestBytes: windowsManifestLimit, MaxSourceBytes: c.maxBytes,
		Authorize: func(ctx context.Context, worktree, name, policy string, epoch int64) error {
			if worktree != b.WorktreeID || policy != b.PolicyDigest || epoch != b.PolicyEpoch {
				return ErrScopeChanged
			}
			return c.authorizePath(ctx, name)
		}}
	if err := store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: generation, JobID: job, PolicyEpoch: b.PolicyEpoch,
		Snapshot: frozen, Writer: writer, Authorize: authorize, MaxManifestBytes: windowsManifestLimit}); err != nil {
		return NativeIndexProgress{}, err
	}
	if err := stageNativeFiles(ctx, store, write, authorize); err != nil {
		return NativeIndexProgress{}, err
	}
	stream, err := store.BeginProviderStage(ctx, write, authorize)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, store.ReleaseProviderStage(ctx, write, stream, authorize))
	}()
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Status: "partial", InputDigest: manifest.SnapshotDigest}
	var analyzed map[string]string
	if facts != nil {
		done, analyzed, err = facts.stage(ctx, store, write, stream, done)
	} else {
		var syntax nativeSyntaxStage
		syntax, err = stageNativeGoSyntax(ctx, store, write, stream, b, true)
		if err != nil {
			return NativeIndexProgress{}, err
		}
		analyzed = syntax.analyzed
		done.Sequence = syntax.sequence
		done.Capabilities = []model.Capability{{Name: "go_syntax", ProviderID: "embedded-go-syntax", ProviderVersion: windowsGoSyntaxVersion, Status: "partial"}}
		done.Gaps = []model.Gap{{Code: "TYPED_ANALYSIS_UNAVAILABLE", Reason: "The Windows index uses Go syntax and does not verify type information.", SourceRefs: []model.SourceRef{}}}
		if typed != nil {
			if len(typed.relations) != 0 {
				batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: done.Sequence,
					Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{}, Relations: typed.relations, Diagnostics: []model.DiagnosticFact{}}
				if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: write, Stream: stream, Batch: batch, MaxFrameBytes: 16 << 20}); err != nil {
					return NativeIndexProgress{}, err
				}
				done.Sequence++
			}
			done.Capabilities = append(done.Capabilities, model.Capability{Name: "go_semantics", ProviderID: "embedded-go-captured", ProviderVersion: "1", Status: "partial"})
			done.Gaps = []model.Gap{{Code: "GRAPH_COVERAGE_PARTIAL", Reason: "Static Go relations cover verified local package closures. Runtime dispatch and test variants are outside this coverage.", SourceRefs: []model.SourceRef{}}}
			if typed.selected != typed.packages {
				done.Gaps = append(done.Gaps, model.Gap{Code: "BUILD_SELECTION_UNVERIFIED", Reason: "Some captured Go packages had no verified typed closure.", SourceRefs: []model.SourceRef{}})
			}
		}
		if syntax.unavailableModules > 0 {
			done.Gaps = append(done.Gaps, model.Gap{Code: "MODULE_MANIFEST_UNAVAILABLE",
				Reason: "Some Go files have no verified module identity.", Count: &syntax.unavailableModules, SourceRefs: []model.SourceRef{}})
		}
	}
	if err != nil {
		return NativeIndexProgress{}, err
	}
	if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: write, Stream: stream, Done: done,
		MaxFrameBytes: 16 << 20, AuthorizeGeneration: authorize}); err != nil {
		return NativeIndexProgress{}, err
	}
	err = store.PublishProvider(ctx, index.PublishProviderRequest{Capture: write, Stream: stream, AuthorizeGeneration: authorize,
		Revalidate: c.revalidate, VerifyProvider: func(ctx context.Context, snap *snapshot.Snapshot, observed model.ProviderDone) error {
			if observed.InputDigest != manifest.SnapshotDigest || observed.JobID != job || observed.Sequence != done.Sequence ||
				snap.Manifest().SnapshotDigest != manifest.SnapshotDigest {
				return index.ErrCaptureMismatch
			}
			for name, hash := range analyzed {
				file, ok := snap.File(name)
				if !ok || file.ContentHash() != hash {
					return index.ErrCaptureMismatch
				}
			}
			if typed != nil {
				if err := typed.proof.Check(ctx); err != nil {
					return err
				}
			}
			return c.scope.Check(ctx)
		}, MaxFacts: 1000000, MaxFrameBytes: 16 << 20})
	if err != nil {
		return NativeIndexProgress{}, err
	}
	progress.GenerationID = generation
	progress.Typed = typed != nil
	if facts != nil {
		progress.ProviderStatus = done.Status
	}
	return progress, nil
}
