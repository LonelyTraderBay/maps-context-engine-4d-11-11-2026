package compose

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

type windowsRenameOwner struct {
	state            *sqlite.WindowsScopedPools
	reader           *ScopeReader
	principal        app.CorePrincipal
	binding          catalog.Binding
	limits           model.CoreRuntimeLimits
	verify           WorkspaceVerifier
	goRoot           string
	goTool, provider model.AllowedExecutable
	limiter          *lsp.SessionLimiter
	mu               sync.Mutex
	proof            *runtime.GoToolchainProof
	server           *paths.Root
	release          func() error
	active           int
	closed           bool
}

func attachWindowsRename(ctx context.Context, state *sqlite.WindowsScopedPools, b *CoreBackend,
	actor app.CorePrincipal, installation string) (_ *CoreBackend, _ func() error, resultErr error) {
	if state.Summary.Stage != "READY" || !state.HasRenameContainer() {
		return b, state.Close, nil
	}
	binding, err := b.Reader.Catalog.ReadBinding(ctx, b.Reader.WorktreeID, b.Reader.ProfileID,
		func(ctx context.Context, w, p string) error { return b.Reader.Access(ctx, actor, w, p) })
	if err != nil {
		return nil, nil, err
	}
	p := binding.Profile
	goTool, goOK := nativeRenameExecutable(binding.Policy, "go.exe")
	provider, lspOK := nativeRenameExecutable(binding.Policy, "gopls.exe")
	if p == nil || p.AnalysisMode != "typed_when_available" || !goOK || !lspOK {
		return b, state.Close, nil
	}
	bin := filepath.Dir(goTool.Path)
	goRoot := filepath.Dir(bin)
	target, err := state.Target.CanonicalPath()
	common, commonErr := state.CommonGit.CanonicalPath()
	if err := errors.Join(err, commonErr); err != nil {
		return nil, nil, err
	}
	if filepath.Base(bin) != "bin" || windowsPathsOverlap(goRoot, target) ||
		windowsPathsOverlap(goRoot, common) || windowsPathsOverlap(goRoot, installation) {
		return nil, nil, app.ErrValidationAuthority
	}
	limits := b.limits
	maxResult, quota := min(limits.RenameResultMaxBytes, 32<<20), min(limits.RenameResultsMaxBytes, 128<<20)
	if maxResult < 1024 || quota < maxResult || limits.MaxRetainedQueryResults < 1 {
		return nil, nil, app.ErrCoreLimit
	}
	root, lease, err := state.PrepareRenameStorage(ctx)
	if err != nil {
		return nil, nil, err
	}
	scanner, err := policy.NewScanner(32 << 20)
	if err != nil {
		return nil, nil, err
	}
	store, err := lsp.NewBudgetedResultStore(root, lease, binding.WorktreeID, scanner, maxResult, quota,
		min(limits.RenameResultTtlSeconds, 900), int(min(limits.MaxRetainedQueryResults, 32)), state.Check,
		func(ctx context.Context, bytes int64) (packstore.BudgetReservation, error) {
			return state.ReserveRenameCache(ctx, limits.MaxWorktreeCacheBytes, bytes)
		})
	if err != nil {
		return nil, nil, err
	}
	limiter, err := lsp.NewSessionLimiter(limits.MaxLspSessions)
	if err != nil {
		return nil, nil, err
	}
	o := &windowsRenameOwner{state: state, reader: b.Reader, principal: actor, binding: binding, limits: limits,
		verify: b.verifyWorkspace, goRoot: goRoot, goTool: goTool, provider: provider, limiter: limiter}
	out, err := b.WithRenamePreview(RenameOwner{Store: store, ProviderDigest: provider.SHA256, Check: o.Check, Open: o.Open})
	if err != nil {
		return nil, nil, err
	}
	return out, func() error {
		if err := o.Close(); err != nil {
			return err
		}
		return state.Close()
	}, nil
}

func windowsPathsOverlap(a, b string) bool {
	in := func(root, name string) bool {
		rel, err := filepath.Rel(root, name)
		return err == nil && filepath.IsLocal(rel)
	}
	return in(a, b) || in(b, a)
}

func (o *windowsRenameOwner) Check(ctx context.Context, p app.CorePrincipal, scope model.ScopeVector) error {
	if ctx == nil || p != o.principal || !boundedSingleScope(scope) {
		return app.ErrValidationAuthority
	}
	current, err := o.reader.Recheck(ctx, p, scope)
	if err != nil {
		return coreStoreError(err)
	}
	if !reflect.DeepEqual(current.Binding, o.binding) {
		return app.ErrValidationScopeChanged
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return app.ErrValidationAuthority
	}
	if o.proof == nil {
		proof, err := runtime.InspectGoToolchain(ctx, o.goRoot)
		if err != nil {
			return errors.Join(app.ErrValidationAuthority, err)
		}
		if proof.Digest() != o.binding.Profile.ToolchainDigest {
			return errors.Join(app.ErrValidationAuthority, proof.Close())
		}
		server, release, err := o.state.PinRenameProvider(ctx, o.provider, o.limits.MaxWorktreeCacheBytes)
		if err != nil {
			return errors.Join(app.ErrValidationAuthority, err, proof.Close())
		}
		if err := lsp.VerifyWindowsServer(ctx, server, o.provider.SHA256); err != nil {
			return errors.Join(app.ErrValidationAuthority, err, proof.Close(), release())
		}
		o.proof, o.server, o.release = proof, server, release
	}
	return errors.Join(o.proof.CheckPath(ctx, "bin/go.exe"), o.proof.CheckPath(ctx, "src"),
		o.proof.CheckPath(ctx, "pkg/tool/windows_amd64"), o.server.Verify())
}

func (o *windowsRenameOwner) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active != 0 {
		return app.ErrCoreBackend
	}
	o.closed = true
	if o.proof == nil {
		return nil
	}
	err := errors.Join(o.proof.Close(), o.release())
	o.proof, o.server, o.release = nil, nil, nil
	return err
}

func (o *windowsRenameOwner) Open(ctx context.Context, actor app.CorePrincipal, scope model.ScopeVector, req model.RenameInitialRequest,
	captured *snapshot.Snapshot) (out lsp.SessionConfig, closeSession func() error, resultErr error) {
	if err := o.Check(ctx, actor, scope); err != nil {
		return out, nil, err
	}
	p := o.binding.Profile
	if captured == nil || p.Goos != "windows" || p.Goarch != "amd64" || p.CgoEnabled || p.TestsEnabled ||
		p.ModuleMode != "readonly" || !slices.Equal(p.ModuleRoots, []string{"."}) || len(p.DependencyRoots) != 0 ||
		len(o.binding.Policy.DependencyRoots) != 0 {
		return out, nil, app.ErrCoreUnsupported
	}
	manifest := captured.Manifest()
	dirs := map[string]bool{}
	for _, f := range manifest.Files {
		if f.Status != "captured" || strings.HasSuffix(f.Path, "_test.go") {
			return out, nil, app.ErrCoreUnsupported
		}
		if strings.HasSuffix(f.Path, ".go") {
			dirs[path.Dir(f.Path)] = true
		}
	}
	if len(dirs) == 0 || len(dirs) > 32 || len(manifest.Files) > 128 {
		return out, nil, app.ErrCoreLimit
	}
	m := scope.Members[0]
	checkWorkspace := func(ctx context.Context) error {
		status, err := o.verify(ctx, actor, m, captured)
		if err != nil {
			return err
		}
		if status != app.WorkspaceObservedCurrent {
			return app.ErrFreshnessChanged
		}
		return nil
	}
	indexCapture, err := newWindowsIndexCapture(sqlite.WindowsIndexScope{Binding: o.binding, Target: o.state.Target, CommonGit: o.state.CommonGit, LinkedGit: o.state.LinkedGit,
		Check: o.state.Check, ReadIgnore: o.state.ReadIgnore}, o.limits)
	if err != nil {
		return out, nil, err
	}
	inv, err := indexCapture.inventory(ctx)
	if err != nil {
		return out, nil, err
	}
	if !slices.Equal(manifest.DependencyInputDigests, []string{inv.InputDigest}) || inv.DeniedCount != 0 ||
		inv.IgnoredCount != 0 || inv.InvalidPathCount != 0 {
		return out, nil, app.ErrCoreUnsupported
	}
	inv.Entries = slices.DeleteFunc(inv.Entries, func(e inventory.FileMapEntry) bool { return e.Directory })
	if err := checkWorkspace(ctx); err != nil {
		return out, nil, err
	}
	scratch, err := o.state.OpenRenameScratch(ctx, captured, o.limits.MaxWorktreeCacheBytes)
	if err != nil {
		return out, nil, err
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return out, nil, errors.Join(app.ErrValidationAuthority, scratch.Close(ctx))
	}
	o.active++
	o.mu.Unlock()
	var once sync.Once
	var closeErr error
	close := func() error {
		once.Do(func() {
			closeErr = scratch.Close(ctx)
			o.mu.Lock()
			o.active--
			o.mu.Unlock()
		})
		return closeErr
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, close())
			out, closeSession = lsp.SessionConfig{}, nil
		}
	}()
	verifyScratch := golanganalysis.WindowsLoaderScratchVerifier(scratch.Root, scratch.Reader, scratch.Check)
	selected, err := o.selectWindowsDocuments(ctx, captured, inv, dirs, scratch, verifyScratch, checkWorkspace)
	if err != nil {
		return out, nil, err
	}
	if !selected[req.Selector.Path] {
		return out, nil, app.ErrCoreUnsupported
	}
	var documents []lsp.CapturedDocument
	version := int64(1)
	for _, item := range manifest.Files {
		if !selected[item.Path] {
			continue
		}
		file, ok := captured.File(item.Path)
		if !ok {
			return out, nil, app.ErrValidationPrecondition
		}
		documents = append(documents, lsp.CapturedDocument{File: file, Version: &version, Ref: model.SourceRef{WorktreeID: m.WorktreeID,
			ProfileID: m.ProfileID, GenerationID: m.GenerationID, Path: item.Path, ContentHash: file.ContentHash()}})
	}
	return lsp.SessionConfig{ServerBinRoot: o.server, ExpectedSHA256: o.provider.SHA256, Documents: documents, Limiter: o.limiter,
		WindowsContainerSID: scratch.Reader.String(), StartupTimeoutMs: o.limits.LspStartupTimeoutMs, RenameTimeoutMs: o.limits.RenameTimeoutMs,
		Environment: runtime.GoEnvironmentInput{GoRoot: o.goRoot, ScratchRoot: scratch.Path, WorkspaceRoot: scratch.Input,
			GOOS: p.Goos, GOARCH: p.Goarch, BuildTags: append([]string{}, p.BuildTags...)},
		Check: func(ctx context.Context) error {
			ok, err := verifyScratch(ctx, scratch.Path, scratch.Input, captured)
			if !ok || err != nil {
				return errors.Join(app.ErrValidationAuthority, err)
			}
			// The server only sees its read-only capture. The loader checks
			// the workspace before/after selection and the core checks it
			// again before releasing the result.
			return o.Check(ctx, actor, scope)
		}}, close, nil
}

func (o *windowsRenameOwner) selectWindowsDocuments(ctx context.Context, captured *snapshot.Snapshot,
	inv inventory.FileMap, dirs map[string]bool, scratch *sqlite.WindowsRenameScratch,
	verify func(context.Context, string, string, *snapshot.Snapshot) (bool, error), checkWorkspace func(context.Context) error,
) (_ map[string]bool, resultErr error) {
	names := make([]string, 0, len(dirs))
	for dir := range dirs {
		names = append(names, dir)
	}
	sort.Strings(names)
	scanner, err := policy.NewScanner(2 << 20)
	if err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	err = withWindowsCapturedGo(ctx, o.binding, captured, inv, scratch, o.goRoot, o.goTool, o.proof, scanner, names,
		o.state.Check, checkWorkspace, verify, func(loader golanganalysis.CapturedLoaderInput) error {
			loaded, err := golanganalysis.LoadCapturedPackages(ctx, loader)
			if err != nil {
				return err
			}
			if !loaded.BuildSelectionVerified || len(loaded.Packages) != 1 {
				return app.ErrCoreUnsupported
			}
			for _, file := range loaded.Packages[0].Files {
				selected[file] = true
			}
			return nil
		})
	return selected, err
}

func withWindowsCapturedGo(ctx context.Context, binding catalog.Binding, captured *snapshot.Snapshot,
	inv inventory.FileMap, scratch *sqlite.WindowsRenameScratch, goRoot string, goTool model.AllowedExecutable,
	proof *runtime.GoToolchainProof, scanner *policy.Scanner, dirs []string, checkScope, checkWorkspace func(context.Context) error,
	verify func(context.Context, string, string, *snapshot.Snapshot) (bool, error),
	use func(golanganalysis.CapturedLoaderInput) error,
) (resultErr error) {
	p := binding.Profile
	manifest := captured.Manifest()
	bin, err := paths.OpenRoot(filepath.Join(goRoot, "bin"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, bin.Close()) }()
	tool, err := runtime.OpenGoExecutable(ctx, runtime.GoExecutableInput{BinRoot: bin, ExpectedSHA256: goTool.SHA256,
		MaxBytes: 128 << 20, Authorize: func(ctx context.Context) error { return proof.CheckPath(ctx, "bin/go.exe") }})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, tool.Close()) }()
	stdRoot, err := paths.OpenRoot(filepath.Join(goRoot, "src"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, stdRoot.Close()) }()
	authorize := func(ctx context.Context, w, id, digest string, epoch int64) error {
		if w != binding.WorktreeID || id != binding.ProfileID || digest != binding.PolicyDigest || epoch != binding.PolicyEpoch {
			return ErrScopeChanged
		}
		return checkScope(ctx)
	}
	goPaths := func(ctx context.Context) error {
		return errors.Join(proof.CheckPath(ctx, "src"), proof.CheckPath(ctx, "pkg/tool/windows_amd64"))
	}
	preflight := golanganalysis.LoaderPreflightInput{Snapshot: captured, Profile: p, Inventory: inv, PolicyEpoch: binding.PolicyEpoch,
		Scanner: scanner, AuthorizeScope: authorize, AuthorizePath: func(ctx context.Context, w, file, digest string, epoch int64) error {
			if policy.CheckConfiguredPath(file, binding.Policy.DeniedGlobs) != nil {
				return ErrAuthority
			}
			return authorize(ctx, w, binding.ProfileID, digest, epoch)
		}, VerifyClosure: func(ctx context.Context, snap *snapshot.Snapshot, observed inventory.FileMap) (bool, error) {
			if snap.Manifest().SnapshotDigest != manifest.SnapshotDigest || !sameNativeLoaderInventory(observed, inv) {
				return false, nil
			}
			err := checkWorkspace(ctx)
			return err == nil, err
		},
		VerifyToolchain: golanganalysis.LoaderToolchainVerifier(tool, goRoot, func(ctx context.Context, profile *model.GoProfile) (bool, error) {
			err := goPaths(ctx)
			return err == nil && reflect.DeepEqual(profile, p) && proof.Digest() == profile.ToolchainDigest, err
		}),
		VerifyDependencies: func(ctx context.Context, profile *model.GoProfile, digests []string) (bool, error) {
			err := goPaths(ctx)
			return err == nil && reflect.DeepEqual(profile, p) && slices.Equal(digests, []string{inv.InputDigest}), err
		}, MaxManifestBytes: windowsManifestLimit, MaxFileBytes: 64 << 10, MaxSourceBytes: 2 << 20}
	packages := proof.StandardPackages()
	for _, dir := range dirs {
		stdlib, err := golanganalysis.NewTrustedStdlib(ctx, golanganalysis.TrustedStdlibInput{SourceRoot: stdRoot, GoRoot: goRoot,
			Profile: p, Snapshot: captured, DependencyDigest: inv.InputDigest, AllowedPackages: packages, Scanner: scanner,
			MaxManifestBytes: windowsManifestLimit, MaxFileBytes: 2 << 20, MaxFiles: 8192, MaxSourceBytes: 64 << 20,
			AuthorizePath: func(ctx context.Context, file string) error { return proof.CheckPath(ctx, "src/"+file) },
			VerifyAuthority: func(ctx context.Context, root, toolchain, dep string, allowed []string) (bool, error) {
				err := goPaths(ctx)
				return err == nil && root == filepath.Join(goRoot, "src") && toolchain == p.ToolchainDigest &&
					dep == inv.InputDigest && slices.Equal(allowed, packages), err
			}})
		if err != nil {
			return err
		}
		if err := use(golanganalysis.CapturedLoaderInput{Preflight: preflight,
			GoRoot: goRoot, ScratchRoot: scratch.Path, WorkspaceRoot: scratch.Input, PackageDir: dir, Tool: tool,
			Stdlib: stdlib, VerifyScratch: verify, MaxOutputBytes: 65536}); err != nil {
			return err
		}
	}
	return proof.Check(ctx)
}
