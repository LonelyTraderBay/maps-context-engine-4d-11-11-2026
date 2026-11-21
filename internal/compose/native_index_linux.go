//go:build linux

package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// RunNativeIndex is an explicit foreground command. Watch owns one worktree
// writer, takes short SQL transactions, and releases every resource on return.
// It publishes captured syntax and any bounded, independently verified Go facts.
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
		if err != nil {
			// A nested authority check can refuse access when its context expires.
			err = errors.Join(err, bounded.Err())
		}
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
	if err := stageNativeFiles(ctx, store, write, authorize); err != nil {
		return NativeIndexProgress{}, err
	}
	stream, err = store.BeginProviderStage(ctx, write, authorize)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	started = true
	syntax, err := stageNativeGoSyntax(ctx, store, write, stream, n.binding)
	if err != nil {
		return NativeIndexProgress{}, err
	}
	sequence, candidates := syntax.sequence, syntax.candidates
	analyzed, unavailableModules, ambiguousPackages := syntax.analyzed, syntax.unavailableModules, syntax.ambiguousPackages
	var typed *nativeTypedIndex
	if !ambiguousPackages && unavailableModules == 0 {
		typed, err = i.typedIndex(ctx, captured, candidates)
		if err != nil {
			return NativeIndexProgress{}, err
		}
	}
	if typed != nil {
		defer func() { resultErr = errors.Join(resultErr, typed.proof.Close()) }()
		if len(typed.relations) != 0 {
			batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: job, Sequence: sequence, Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{}, Relations: typed.relations, Diagnostics: []model.DiagnosticFact{}}
			if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: write, Stream: stream, Batch: batch, MaxFrameBytes: 16 << 20}); err != nil {
				return NativeIndexProgress{}, err
			}
			sequence++
		}
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: job, Sequence: sequence, Status: "partial", InputDigest: manifest.SnapshotDigest, Capabilities: []model.Capability{{Name: "go_syntax", ProviderID: "embedded-go-syntax", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "BUILD_SELECTION_UNVERIFIED", Reason: "Captured syntax only; build selection, import identity and typed dependency closure are not verified.", SourceRefs: []model.SourceRef{}}}}
	if typed != nil {
		done.Capabilities = append(done.Capabilities, model.Capability{Name: "go_semantics", ProviderID: "embedded-go-captured", ProviderVersion: "1", Status: "partial"})
		done.Gaps = []model.Gap{{Code: "GRAPH_COVERAGE_PARTIAL", Reason: "Static Go relations are limited to verified local package closures and do not establish runtime dispatch.", SourceRefs: []model.SourceRef{}}}
		if typed.selected != len(candidates) {
			done.Gaps = append(done.Gaps, model.Gap{Code: "BUILD_SELECTION_UNVERIFIED", Reason: "Some captured Go packages had no verified typed closure.", SourceRefs: []model.SourceRef{}})
		}
	}
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
		if typed != nil {
			if err := typed.proof.Check(ctx); err != nil {
				return err
			}
		}
		return n.recheckBinding(ctx)
	}, MaxFacts: 1000000, MaxFrameBytes: 16 << 20})
	if err != nil {
		return NativeIndexProgress{}, err
	}
	published = true
	return NativeIndexProgress{GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, Files: len(manifest.Files), Typed: typed != nil}, nil
}
