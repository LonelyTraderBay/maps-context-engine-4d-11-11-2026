package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
)

const windowsManifestLimit = 1 << 20

// RunWindowsIndex publishes Go syntax from the authorized Windows Git
// inventory without executing project code.
func RunWindowsIndex(ctx context.Context, installation, selected, worktree, profile string, cpus int, full, watch bool,
	progress func(NativeIndexProgress) error) error {
	if selected != "" && selected != filepath.Join(installation, "runtime.json") {
		return sqlite.ErrProfileDenied
	}
	for {
		result, poll, err := runWindowsIndexOnce(ctx, installation, worktree, profile, cpus, full)
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

func runWindowsIndexOnce(ctx context.Context, installation, worktree, profile string, cpus int, full bool) (NativeIndexProgress, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var poll time.Duration
	result, err := sqlite.RunWindowsIndexMutation(ctx, installation, worktree, profile, func(scope sqlite.WindowsIndexScope) (sqlite.WindowsIndexPublish[NativeIndexProgress], int64, error) {
		config, err := app.NewRuntimeConfigService(ctx, windowsIndexConfig{read: scope.ReadConfig}, cpus)
		if err != nil {
			return nil, 0, err
		}
		effective, _, err := config.StartupConfig()
		if err != nil {
			return nil, 0, err
		}
		poll, err = nativeMilliseconds(effective.Core.WatchPollIntervalMs)
		if err != nil {
			return nil, 0, err
		}
		binding := scope.Binding
		if (binding.Profile.AnalysisMode != "syntax" && binding.Profile.AnalysisMode != "typed_when_available") || binding.Profile.Goos != "windows" ||
			len(binding.Profile.DependencyRoots) != 0 || len(binding.Policy.DependencyRoots) != 0 {
			return nil, 0, app.ErrCoreUnsupported
		}
		c, err := newWindowsIndexCapture(scope, effective.Core)
		if err != nil {
			return nil, 0, err
		}
		frozen, err := c.capture(ctx)
		if err != nil {
			return nil, 0, err
		}
		return func(store *index.Store, writer *runtime.Writer, job string) (NativeIndexProgress, error) {
			return c.publish(ctx, store, writer, job, frozen, full)
		}, effective.Core.MaxWorktreeCacheBytes, nil
	})
	if err != nil {
		err = errors.Join(err, ctx.Err())
	}
	return result, poll, err
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
}

func newWindowsIndexCapture(scope sqlite.WindowsIndexScope, limits model.CoreRuntimeLimits) (windowsIndexCapture, error) {
	scanner, err := policy.NewScanner(windowsManifestLimit)
	if err != nil {
		return windowsIndexCapture{}, err
	}
	policy := scope.Binding.Policy
	return windowsIndexCapture{scope: scope, scanner: scanner, maxEntries: int(min(limits.MaxIndexedFiles, 100000)),
		maxFile:  int(min(policy.MaxFileBytes, limits.MaxFileBytes, windowsManifestLimit)),
		maxBytes: min(policy.MaxSourceBytes, limits.MaxSourceBytesPerSnapshot, windowsManifestLimit)}, nil
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
		Root: c.scope.Target, Metadata: c.scope.CommonGit, Scanner: c.scanner, MaxEntries: c.maxEntries,
		AuthorizeRoot: c.scope.Check, ReadGlobal: c.scope.ReadIgnore,
		AuthorizePath: func(ctx context.Context, name string, directory bool) (bool, error) {
			return policy.CheckConfiguredPath(name, b.Policy.DeniedGlobs) == nil, ctx.Err()
		},
		Exclude: func(name string, directory bool) bool { return !directory && !nativeTextPath(name) },
	})
}

func (c windowsIndexCapture) capture(ctx context.Context) (*snapshot.Snapshot, error) {
	b := c.scope.Binding
	fileMap, err := c.inventory(ctx)
	switch {
	case errors.Is(err, inventory.ErrIndexBudget), errors.Is(err, inventory.ErrCaptureLimit),
		errors.Is(err, inventory.ErrWalkBudget), errors.Is(err, inventory.ErrIgnoreBudget):
		return nil, errors.Join(app.ErrCoreLimit, err)
	case errors.Is(err, inventory.ErrIndexUnsupported), errors.Is(err, inventory.ErrGitConfigUnsupported),
		errors.Is(err, inventory.ErrIgnoreUnsupported), errors.Is(err, os.ErrNotExist):
		return nil, errors.Join(app.ErrCoreUnsupported, err)
	case err != nil:
		return nil, err
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: b.WorktreeID, ProfileID: b.ProfileID,
		PolicyDigest: b.PolicyDigest, ProviderDigest: b.Profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{fileMap.InputDigest}}
	files := []snapshot.File{}
	var total int64
	for _, item := range fileMap.Entries {
		if item.Directory {
			continue // Directory boundaries are bound by the inventory digest.
		}
		name := item.Path
		if item.State == "missing" {
			manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "missing"})
			continue
		}
		excluded := model.ManifestFile{Path: name, Status: "excluded"}
		if item.State != "candidate" || item.SkipWorktree || item.IntentToAdd || !nativeTextPath(name) {
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
		if length > c.maxBytes-total {
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

func (c windowsIndexCapture) publish(ctx context.Context, store *index.Store, writer *runtime.Writer, job string,
	frozen *snapshot.Snapshot, full bool) (_ NativeIndexProgress, resultErr error) {
	b := c.scope.Binding
	manifest := frozen.Manifest()
	authorize := func(ctx context.Context, worktree, profile, policy string, epoch int64) error {
		if worktree != b.WorktreeID || profile != b.ProfileID || policy != b.PolicyDigest || epoch != b.PolicyEpoch {
			return ErrScopeChanged
		}
		return c.scope.Check(ctx)
	}
	revalidate := func(ctx context.Context, expected *snapshot.Snapshot) error {
		now, err := c.capture(ctx)
		if err != nil {
			return err
		}
		if now.Manifest().SnapshotDigest != expected.Manifest().SnapshotDigest {
			return ErrScopeChanged
		}
		return nil
	}
	if err := revalidate(ctx, frozen); err != nil {
		return NativeIndexProgress{}, err
	}
	previous, err := store.ReadGeneration(ctx, b.ProfileID, "", authorize, windowsManifestLimit)
	if err != nil && !errors.Is(err, index.ErrSnapshotUnavailable) {
		return NativeIndexProgress{}, err
	}
	if err == nil && !full && previous.SnapshotDigest == manifest.SnapshotDigest && previous.ProviderDigest == b.Profile.ProviderDigest {
		return NativeIndexProgress{GenerationID: previous.GenerationID, SnapshotDigest: manifest.SnapshotDigest, Files: len(manifest.Files), Unchanged: true}, nil
	}
	generation, err := nativeOperationID()
	if err != nil {
		return NativeIndexProgress{}, err
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
	syntax, err := stageNativeGoSyntax(ctx, store, write, stream, b)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job,
		Sequence: syntax.sequence, Status: "partial", InputDigest: manifest.SnapshotDigest,
		Capabilities: []model.Capability{{Name: "go_syntax", ProviderID: "embedded-go-syntax", ProviderVersion: "1", Status: "partial"}},
		Gaps:         []model.Gap{{Code: "TYPED_ANALYSIS_UNAVAILABLE", Reason: "The Windows index uses Go syntax and does not verify type information.", SourceRefs: []model.SourceRef{}}}}
	if syntax.unavailableModules > 0 {
		done.Gaps = append(done.Gaps, model.Gap{Code: "MODULE_MANIFEST_UNAVAILABLE",
			Reason: "Some Go files have no verified module identity.", Count: &syntax.unavailableModules, SourceRefs: []model.SourceRef{}})
	}
	if err := store.SealProviderStage(ctx, index.SealProviderRequest{Capture: write, Stream: stream, Done: done,
		MaxFrameBytes: 16 << 20, AuthorizeGeneration: authorize}); err != nil {
		return NativeIndexProgress{}, err
	}
	err = store.PublishProvider(ctx, index.PublishProviderRequest{Capture: write, Stream: stream, AuthorizeGeneration: authorize,
		Revalidate: revalidate, VerifyProvider: func(ctx context.Context, snap *snapshot.Snapshot, observed model.ProviderDone) error {
			if observed.InputDigest != manifest.SnapshotDigest || observed.JobID != job || observed.Sequence != syntax.sequence ||
				snap.Manifest().SnapshotDigest != manifest.SnapshotDigest {
				return index.ErrCaptureMismatch
			}
			for name, hash := range syntax.analyzed {
				file, ok := snap.File(name)
				if !ok || file.ContentHash() != hash {
					return index.ErrCaptureMismatch
				}
			}
			return c.scope.Check(ctx)
		}, MaxFacts: 1000000, MaxFrameBytes: 16 << 20})
	if err != nil {
		return NativeIndexProgress{}, err
	}
	return NativeIndexProgress{GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, Files: len(manifest.Files)}, nil
}
