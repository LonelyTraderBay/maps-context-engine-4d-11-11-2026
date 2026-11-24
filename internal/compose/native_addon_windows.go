package compose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

type windowsFactStage interface {
	stage(context.Context, *index.Store, index.CaptureWrite, index.ProviderStageStream, model.ProviderDone) (model.ProviderDone, map[string]string, error)
}

type windowsAddonFacts struct {
	batches []model.ProviderBatch
	done    model.ProviderDone
}

const windowsAddonFrame = 1 << 20

func (f *windowsAddonFacts) stage(ctx context.Context, store *index.Store, write index.CaptureWrite, stream index.ProviderStageStream,
	expected model.ProviderDone) (model.ProviderDone, map[string]string, error) {
	if f.done.JobID != write.JobID || f.done.InputDigest != expected.InputDigest {
		return model.ProviderDone{}, nil, index.ErrCaptureMismatch
	}
	for _, batch := range f.batches {
		if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: write, Stream: stream,
			Batch: batch, MaxFrameBytes: windowsAddonFrame}); err != nil {
			return model.ProviderDone{}, nil, err
		}
	}
	return f.done, nil, nil
}

type windowsAddon struct {
	directory, executable, digest, id, programName string
	program                                        []byte
	analyze                                        func(context.Context, *paths.Root, *paths.Root, string, ports.ProviderJob, ports.ProviderSink) (model.ProviderDone, error)
}

func (c windowsIndexCapture) analyzeAddon(ctx context.Context, installation string, frozen *snapshot.Snapshot, generation string, cacheLimit int64) (_ *windowsAddonFacts, resultErr error) {
	addon := c.addon
	approved, ok := nativeRenameExecutable(c.scope.Binding.Policy, addon.executable)
	home := filepath.Join(installation, "control", addon.directory)
	if !ok || approved.Path != filepath.Join(home, addon.executable) || approved.SHA256 != addon.digest {
		return nil, app.ErrCoreUnsupported
	}
	if windowsPathsOverlap(home, c.scope.Binding.CommonDirectory) {
		return nil, app.ErrCoreAuthentication
	}
	for _, target := range c.scope.Binding.Policy.AllowedRoots {
		if windowsPathsOverlap(home, target) {
			return nil, app.ErrCoreAuthentication
		}
	}
	tool, err := c.scope.OpenRuntime(ctx, addon.directory)
	if err != nil {
		return nil, errors.Join(app.ErrCoreUnsupported, err)
	}
	defer func() { resultErr = errors.Join(resultErr, tool.Close()) }()
	manifest := frozen.Manifest()
	raw, err := json.Marshal(manifest)
	if err != nil || len(raw) > windowsManifestLimit {
		return nil, app.ErrCoreLimit
	}
	scope := c.scope
	name := "mce4d-provider-" + generation + ".json"
	facts := &windowsAddonFacts{}
	err = scope.WithScratch(ctx, sqlite.WindowsProviderCapture{Snapshot: frozen, Generation: generation, ManifestName: name,
		ProgramName: addon.programName, Manifest: raw, Program: addon.program, CacheLimit: cacheLimit}, func(input *paths.Root, script string) error {
		root, err := input.CanonicalPath()
		if err != nil {
			return err
		}
		deadline, _ := ctx.Deadline()
		job := ports.ProviderJob{Capture: providers.SnapshotInput(frozen), Recheck: func(ctx context.Context) error {
			return errors.Join(scope.Check(ctx), tool.Verify(), input.Verify())
		}, Start: model.ProviderStart{
			ProviderProtocol: 1, Type: "analyze_start", ProviderID: addon.id, ProviderDigest: manifest.ProviderDigest,
			JobID: scope.JobID, GenerationID: generation, ProfileID: manifest.ProfileID, Profile: scope.Binding.AddonProfile,
			SnapshotRoot: root, ManifestPath: filepath.Join(root, name), ManifestDigest: fmt.Sprintf("%x", sha256.Sum256(raw)),
			MaxFrameBytes: windowsAddonFrame, DeadlineAt: deadline.UTC().Format(time.RFC3339Nano),
		}}
		facts.done, err = addon.analyze(ctx, tool, input, script, job, func(_ context.Context, batch model.ProviderBatch) error {
			facts.batches = append(facts.batches, batch)
			return nil
		})
		return err
	})
	if errors.Is(err, providers.ErrAuthority) {
		return nil, errors.Join(app.ErrCoreUnsupported, err)
	}
	return facts, err
}
