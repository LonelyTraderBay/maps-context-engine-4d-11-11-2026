//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/sys/unix"
)

type nativeRenameOwner struct {
	mu                  sync.Mutex
	n                   *nativeScope
	reader              *ScopeReader
	principal           app.CorePrincipal
	goBinary, lspBinary model.AllowedExecutable
	goRoot              string
	proof               *runtime.GoToolchainProof
	serverRoot          *paths.Root
	serverFile          *os.File
	serverStat          unix.Stat_t
	limiter             *lsp.SessionLimiter
	active              int
	closed              bool
}

// Construction reads no source/toolchain and starts no process. Approvals come
// only from the real bound policy/profile, never executable discovery or a tool
// request. Proof work remains lazy, outside the MCP startup handshake.
func attachNativeRename(ctx context.Context, n *nativeScope, b *CoreBackend, p app.CorePrincipal) (*CoreBackend, error) {
	profile := n.binding.Profile
	goBinary, goOK := nativeRenameExecutable(n.binding.Policy, "go")
	lspBinary, lspOK := nativeRenameExecutable(n.binding.Policy, "gopls")
	if profile.AnalysisMode != "typed_when_available" || !goOK || !lspOK {
		return b, nil
	}
	root, lease, protected, err := n.stores.CacheResources(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureNativeRenameDirectory(ctx, n, root, protected); err != nil {
		return nil, err
	}
	maxResult := minInt64(n.effective.Core.RenameResultMaxBytes, 32<<20)
	quota := minInt64(n.effective.Core.RenameResultsMaxBytes, 128<<20)
	count := minInt64(n.effective.Core.MaxRetainedQueryResults, 32)
	if maxResult < 1024 || quota < maxResult || count < 1 {
		return nil, app.ErrCoreLimit
	}
	scanner, err := policy.NewScanner(32 << 20)
	if err != nil {
		return nil, err
	}
	store, err := lsp.NewBudgetedResultStore(root, lease, n.binding.WorktreeID, scanner, maxResult, quota, minInt64(n.effective.Core.RenameResultTtlSeconds, 900), int(count),
		func(ctx context.Context) error {
			return errors.Join(n.stores.Check(ctx), runtime.CheckCleanupRoot(root, protected))
		},
		func(ctx context.Context, bytes int64) (packstore.BudgetReservation, error) {
			return n.stores.ReserveCache(ctx, bytes)
		})
	if err != nil {
		return nil, err
	}
	limiter, err := lsp.NewSessionLimiter(n.effective.Core.MaxLspSessions)
	if err != nil {
		return nil, err
	}
	o := &nativeRenameOwner{n: n, reader: b.Reader, principal: p, goBinary: goBinary, lspBinary: lspBinary, goRoot: filepath.Dir(filepath.Dir(goBinary.Path)), limiter: limiter}
	if filepath.Base(filepath.Dir(goBinary.Path)) != "bin" {
		return nil, app.ErrCoreUnsupported
	}
	configured, err := b.WithRenamePreview(RenameOwner{Store: store, ProviderDigest: lspBinary.SHA256, Check: o.Check, Open: o.Open})
	if err != nil {
		return nil, err
	}
	n.rename = o
	return configured, nil
}

func (o *nativeRenameOwner) checkProofLocked(ctx context.Context) error {
	if o.closed {
		return app.ErrValidationAuthority
	}
	if o.proof == nil {
		proof, err := runtime.InspectGoToolchain(ctx, o.goRoot)
		if err != nil {
			return errors.Join(app.ErrValidationAuthority, err)
		}
		if proof.Digest() != o.n.binding.Profile.ToolchainDigest {
			_ = proof.Close()
			return app.ErrValidationAuthority
		}
		root, err := paths.OpenRoot(o.goRoot)
		if err != nil {
			_ = proof.Close()
			return err
		}
		protected := append(append([]*paths.Root{}, o.n.state.TargetRoots()...), o.n.state.StateRoot())
		err = errors.Join(runtime.CheckCleanupRoot(root, protected), root.Close())
		if err != nil {
			_ = proof.Close()
			return errors.Join(app.ErrValidationAuthority, err)
		}
		if runtime.CheckAdministratorPath(o.lspBinary.Path) != nil {
			_ = proof.Close()
			return app.ErrValidationAuthority
		}
		serverRoot, err := paths.OpenRoot(filepath.Dir(o.lspBinary.Path))
		if err != nil {
			_ = proof.Close()
			return err
		}
		file, err := serverRoot.OpenRegular("gopls")
		if err != nil {
			_ = serverRoot.Close()
			_ = proof.Close()
			return err
		}
		var before, after unix.Stat_t
		h := sha256.New()
		statErr := unix.Fstat(int(file.Fd()), &before)
		n, readErr := io.Copy(h, io.LimitReader(file, (256<<20)+1))
		afterErr := unix.Fstat(int(file.Fd()), &after)
		if statErr != nil || readErr != nil || afterErr != nil || n > 256<<20 || !sameNativeExecutable(before, after) || before.Uid != 0 || before.Nlink != 1 || before.Mode&0022 != 0 || hex.EncodeToString(h.Sum(nil)) != o.lspBinary.SHA256 {
			_ = file.Close()
			_ = serverRoot.Close()
			_ = proof.Close()
			return app.ErrValidationAuthority
		}
		o.proof, o.serverRoot, o.serverFile, o.serverStat = proof, serverRoot, file, after
	}
	if err := o.proof.Check(ctx); err != nil {
		return errors.Join(app.ErrValidationAuthority, err)
	}
	var observed unix.Stat_t
	if runtime.CheckAdministratorPath(o.lspBinary.Path) != nil || o.serverRoot.Verify() != nil || unix.Lstat(o.lspBinary.Path, &observed) != nil || !sameNativeExecutable(o.serverStat, observed) {
		return app.ErrValidationAuthority
	}
	return ctx.Err()
}

func sameNativeExecutable(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Size == b.Size && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Ctim == b.Ctim && a.Mtim == b.Mtim
}

func (o *nativeRenameOwner) Check(ctx context.Context, p app.CorePrincipal, scope model.ScopeVector) error {
	if ctx == nil || p != o.principal || !boundedSingleScope(scope) || scope.Members[0].WorktreeID != o.n.binding.WorktreeID || scope.Members[0].ProfileID != o.n.binding.ProfileID {
		return app.ErrValidationAuthority
	}
	current, err := o.reader.Recheck(ctx, p, scope)
	if err != nil {
		return coreStoreError(err)
	}
	if !reflect.DeepEqual(current.Binding, o.n.binding) {
		return app.ErrValidationScopeChanged
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.checkProofLocked(ctx)
}

func (o *nativeRenameOwner) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	if o.active != 0 {
		return app.ErrCoreBackend
	}
	o.closed = true
	var err error
	if o.proof != nil {
		err = errors.Join(err, o.proof.Close())
	}
	if o.serverFile != nil {
		err = errors.Join(err, o.serverFile.Close())
	}
	if o.serverRoot != nil {
		err = errors.Join(err, o.serverRoot.Close())
	}
	return err
}

func (o *nativeRenameOwner) Open(ctx context.Context, p app.CorePrincipal, scope model.ScopeVector, request model.RenameInitialRequest, captured *snapshot.Snapshot) (config lsp.SessionConfig, closeSession func() error, resultErr error) {
	if err := runtime.CheckPrivateToolUmask(ctx); err != nil {
		return config, nil, app.ErrCoreUnsupported
	}
	if err := o.Check(ctx, p, scope); err != nil {
		return config, nil, err
	}
	profile := o.n.binding.Profile
	if profile.CgoEnabled || profile.TestsEnabled || len(profile.ModuleRoots) != 1 || profile.ModuleRoots[0] != "." || profile.ModuleMode != "readonly" || len(profile.DependencyRoots) != 0 || len(o.n.binding.Policy.DependencyRoots) != 0 {
		return config, nil, app.ErrCoreUnsupported
	}
	if captured == nil || !app.WorkspaceCaptureComparable(captured.Manifest()) || len(captured.Manifest().Files) > 128 {
		return config, nil, app.ErrFreshnessUnknown
	}
	i, err := openNativeInventory(ctx, o.n, false)
	if err != nil {
		return config, nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			resultErr = errors.Join(resultErr, i.Close())
		}
	}()
	files, err := i.Observe(ctx)
	if err != nil {
		return config, nil, err
	}
	manifest := captured.Manifest()
	if len(manifest.DependencyInputDigests) != 1 || manifest.DependencyInputDigests[0] != files.InputDigest || files.IgnoredCount != 0 || files.DeniedCount != 0 || files.InvalidPathCount != 0 {
		return config, nil, app.ErrCoreUnsupported
	}
	if err := i.observeCaptured(ctx, nativeCapture{snapshot: captured, files: files, blocked: map[string]string{}}, captured, false); err != nil {
		return config, nil, err
	}
	directories := map[string]bool{}
	for _, item := range manifest.Files {
		if item.Status != "captured" || strings.HasSuffix(item.Path, "_test.go") {
			return config, nil, app.ErrCoreUnsupported
		}
		if strings.HasSuffix(item.Path, ".go") {
			directories[path.Dir(item.Path)] = true
		}
	}
	if len(directories) == 0 || len(directories) > 32 {
		return config, nil, app.ErrCoreLimit
	}
	scratch, err := newNativeRenameScratch(ctx, o.n, captured, o.goRoot, o.serverRoot)
	if err != nil {
		return config, nil, err
	}
	o.mu.Lock()
	o.active++
	o.mu.Unlock()
	closed := false
	closeSession = func() error {
		if closed {
			return nil
		}
		closed = true
		defer func() { o.mu.Lock(); o.active--; o.mu.Unlock() }()
		return scratch.Close(ctx)
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, closeSession())
			config = lsp.SessionConfig{}
			closeSession = nil
		}
	}()
	binRoot, err := paths.OpenRoot(filepath.Join(o.goRoot, "bin"))
	if err != nil {
		return config, closeSession, err
	}
	defer func() { resultErr = errors.Join(resultErr, binRoot.Close()) }()
	tool, err := runtime.OpenGoExecutable(ctx, runtime.GoExecutableInput{BinRoot: binRoot, ExpectedSHA256: o.goBinary.SHA256, MaxBytes: 128 << 20, Authorize: func(ctx context.Context) error { return o.proof.CheckPath(ctx, "bin/go") }})
	if err != nil {
		return config, closeSession, err
	}
	defer func() { resultErr = errors.Join(resultErr, tool.Close()) }()
	scanner, err := policy.NewScanner(2 << 20)
	if err != nil {
		return config, closeSession, err
	}
	authorizeScope := func(ctx context.Context, w, profileID, digest string, epoch int64) error {
		m := scope.Members[0]
		if w != m.WorktreeID || profileID != m.ProfileID || digest != m.PolicyDigest || epoch != m.PolicyEpoch {
			return ErrScopeChanged
		}
		return o.Check(ctx, p, scope)
	}
	authorizePath := func(ctx context.Context, w, name, digest string, epoch int64) error {
		m := scope.Members[0]
		if w != m.WorktreeID || digest != m.PolicyDigest || epoch != m.PolicyEpoch || policy.CheckConfiguredPath(name, o.n.binding.Policy.DeniedGlobs) != nil {
			return ErrAuthority
		}
		return o.n.stores.Check(ctx)
	}
	closure := func(ctx context.Context, snap *snapshot.Snapshot, observed inventory.FileMap) (bool, error) {
		if snap.Manifest().SnapshotDigest != manifest.SnapshotDigest || !sameNativeLoaderInventory(observed, files) {
			return false, nil
		}
		if err := scratch.Check(ctx); err != nil {
			return false, err
		}
		err := i.observeCaptured(ctx, nativeCapture{snapshot: snap, files: files, blocked: map[string]string{}}, snap, false)
		return err == nil, err
	}
	preflight := golanganalysis.LoaderPreflightInput{Snapshot: captured, Profile: profile, Inventory: files, PolicyEpoch: scope.Members[0].PolicyEpoch, Scanner: scanner, AuthorizeScope: authorizeScope, AuthorizePath: authorizePath, VerifyClosure: closure,
		VerifyToolchain: golanganalysis.LoaderToolchainVerifier(tool, o.goRoot, func(ctx context.Context, candidate *model.GoProfile) (bool, error) {
			if !reflect.DeepEqual(candidate, profile) {
				return false, nil
			}
			err := o.proof.Check(ctx)
			return err == nil, err
		}),
		VerifyDependencies: func(ctx context.Context, candidate *model.GoProfile, digests []string) (bool, error) {
			if !reflect.DeepEqual(candidate, profile) || !reflect.DeepEqual(digests, manifest.DependencyInputDigests) || len(candidate.DependencyRoots) != 0 {
				return false, nil
			}
			err := o.proof.Check(ctx)
			return err == nil, err
		}, MaxManifestBytes: 1 << 20, MaxFileBytes: 64 << 10, MaxSourceBytes: 2 << 20}
	stdlibRoot, err := paths.OpenRoot(filepath.Join(o.goRoot, "src"))
	if err != nil {
		return config, closeSession, err
	}
	defer func() { resultErr = errors.Join(resultErr, stdlibRoot.Close()) }()
	allowed := o.proof.StandardPackages()
	dirs := make([]string, 0, len(directories))
	for dir := range directories {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	selected := map[string]bool{}
	for _, dir := range dirs {
		stdlib, err := golanganalysis.NewTrustedStdlib(ctx, golanganalysis.TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: o.goRoot, Profile: profile, Snapshot: captured, DependencyDigest: files.InputDigest, AllowedPackages: allowed, Scanner: scanner, MaxManifestBytes: 1 << 20, MaxFileBytes: 2 << 20, MaxFiles: 8192, MaxSourceBytes: 64 << 20,
			AuthorizePath: func(ctx context.Context, name string) error {
				if model.ValidateRelativePath(name, false) != nil {
					return ErrAuthority
				}
				return o.proof.CheckPath(ctx, "src/"+name)
			},
			VerifyAuthority: func(ctx context.Context, root, toolchain, dependency string, packages []string) (bool, error) {
				if root != filepath.Join(o.goRoot, "src") || toolchain != profile.ToolchainDigest || dependency != files.InputDigest || !reflect.DeepEqual(packages, allowed) {
					return false, nil
				}
				err := o.proof.Check(ctx)
				return err == nil, err
			}})
		if err != nil {
			return config, closeSession, err
		}
		loaded, err := golanganalysis.LoadCapturedPackages(ctx, golanganalysis.CapturedLoaderInput{Preflight: preflight, GoRoot: o.goRoot, ScratchRoot: scratch.path, WorkspaceRoot: filepath.Join(scratch.path, "input"), PackageDir: dir, Tool: tool, Stdlib: stdlib, VerifyScratch: golanganalysis.LoaderScratchVerifier(scratch.root, func(ctx context.Context) error { return scratch.Check(ctx) }), MaxOutputBytes: 65536})
		if err != nil {
			return config, closeSession, err
		}
		if !loaded.BuildSelectionVerified || len(loaded.Packages) != 1 {
			return config, closeSession, app.ErrCoreUnsupported
		}
		for _, name := range loaded.Packages[0].Files {
			selected[name] = true
		}
	}
	documents := make([]lsp.CapturedDocument, 0, len(selected))
	version := int64(1)
	for _, item := range manifest.Files {
		if !selected[item.Path] {
			continue
		}
		file, ok := captured.File(item.Path)
		if !ok {
			return config, closeSession, app.ErrValidationPrecondition
		}
		m := scope.Members[0]
		documents = append(documents, lsp.CapturedDocument{File: file, Version: &version, Ref: model.SourceRef{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: m.GenerationID, Path: item.Path, ContentHash: file.ContentHash()}})
	}
	if !selected[request.Selector.Path] {
		return config, closeSession, app.ErrCoreUnsupported
	}
	check := func(ctx context.Context) error {
		if closed {
			return app.ErrValidationAuthority
		}
		if err := o.Check(ctx, p, scope); err != nil {
			return err
		}
		if err := scratch.Check(ctx); err != nil {
			return err
		}
		return i.observeCaptured(ctx, nativeCapture{snapshot: captured, files: files, blocked: map[string]string{}}, captured, false)
	}
	// i's inventory descriptors are needed by Session.Check after this method
	// returns. Transfer their close into the same one-shot scratch owner.
	originalClose := closeSession
	closeSession = func() error {
		if closed {
			return nil
		}
		return errors.Join(originalClose(), i.Close())
	}
	transferred = true
	return lsp.SessionConfig{ServerBinRoot: o.serverRoot, ExpectedSHA256: o.lspBinary.SHA256, Environment: runtime.GoEnvironmentInput{GoRoot: o.goRoot, ScratchRoot: scratch.path, WorkspaceRoot: filepath.Join(scratch.path, "input"), GOOS: profile.Goos, GOARCH: profile.Goarch, BuildTags: append([]string{}, profile.BuildTags...)}, Documents: documents, Check: check, Limiter: o.limiter, StartupTimeoutMs: o.n.effective.Core.LspStartupTimeoutMs, RenameTimeoutMs: o.n.effective.Core.RenameTimeoutMs}, closeSession, nil
}
