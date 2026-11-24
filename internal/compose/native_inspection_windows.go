package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

// OpenWindowsInspection composes the normal core readers over an authenticated
// controller inspection session. It does not activate the staged installation.
func OpenWindowsInspection(ctx context.Context, installation, selected, worktree, profile string, cpus int) (_ *app.CoreService, _ func() error, resultErr error) {
	backend, config, release, err := openWindowsInspectionBackend(ctx, installation, selected, worktree, profile, cpus, true)
	if err != nil {
		return nil, nil, err
	}
	service, err := app.NewCoreService(config, backend.Authority, backend, nil)
	if err != nil {
		return nil, nil, errors.Join(err, release())
	}
	return service, release, nil
}

func openWindowsInspectionBackend(ctx context.Context, installation, selected, worktree, profile string, cpus int, rename bool) (_ *CoreBackend, _ *app.RuntimeConfigService, _ func() error, resultErr error) {
	if selected != "" && selected != filepath.Join(installation, "runtime.json") {
		return nil, nil, nil, sqlite.ErrProfileDenied
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := sqlite.OpenWindowsScopedPools(bounded, installation, worktree, profile)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, state.Close())
		}
	}()

	config, err := app.NewRuntimeConfigService(bounded, windowsInspectionConfig{pools: state}, cpus)
	if err != nil {
		return nil, nil, nil, err
	}
	effective, _, err := config.StartupConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	principalID := "installation:" + state.Summary.InstallationID
	digest := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	principal := app.CorePrincipal{
		ID: principalID, Digest: digest("MCE4D-OS-PRINCIPAL1\x00" + principalID),
		BindingDigest: digest("MCE4D-BINDING1\x00" + principalID + "\x00" + worktree + "\x00" + profile),
	}
	scopedAccess := func(check func(context.Context) error) ScopeAccess {
		return func(ctx context.Context, candidate app.CorePrincipal, w, id string) error {
			if candidate != principal || w != worktree || id != profile {
				return ErrAuthority
			}
			return check(ctx)
		}
	}
	access := scopedAccess(state.Check)
	reader := &ScopeReader{Catalog: state.Catalog, Index: state.Index, Records: state.Records, WorktreeRoot: state.Target,
		CommonRoot: state.CommonGit, LinkedRelative: state.LinkedGit, WorktreeID: worktree, ProfileID: profile, Access: access,
		MaxHeads: 100000, MaxManifestBytes: 64 << 20, observationAccess: scopedAccess(state.CheckObservationBinding)}
	boundPath := func(ctx context.Context, p app.CorePrincipal, config model.PolicyConfig, name string) error {
		if p != principal {
			return ErrAuthority
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := policy.CheckConfiguredPath(name, config.DeniedGlobs); err != nil {
			return err
		}
		file, err := state.Target.OpenRegular(name)
		if err != nil {
			return err
		}
		return errors.Join(file.Close(), state.Target.Verify(), state.CommonGit.Verify())
	}
	authority := &BoundCoreAuthority{Reader: reader,
		AuthenticatePrincipal: func(context.Context) (app.CorePrincipal, error) { return principal, nil },
		CheckMetadataPath:     policy.CheckConfiguredPath,
		AuthorizeBoundPath:    boundPath,
		AuthorizePath: func(ctx context.Context, p app.CorePrincipal, config model.PolicyConfig, name string) error {
			if err := access(ctx, p, worktree, profile); err != nil {
				return err
			}
			return boundPath(ctx, p, config, name)
		},
	}
	key, err := state.CursorKey(bounded)
	if errors.Is(err, os.ErrNotExist) {
		key, err = contextpack.GenerateCursorKey()
	}
	defer clear(key[:])
	if err != nil {
		return nil, nil, nil, err
	}
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		return nil, nil, nil, err
	}
	cache, err := app.NewFreshnessPackCache(codec, int(effective.Core.MaxRetainedQueryResults), effective.Core.QueryResultCacheBytes, min(effective.Core.PackTtlSeconds, 900))
	if err != nil {
		return nil, nil, nil, err
	}
	verify := windowsWorkspaceVerifier(state, reader, principal, effective.Core)
	backend, err := NewCoreBackend(config, reader, authority, codec, cache, verify)
	if err != nil {
		return nil, nil, nil, err
	}
	release := state.Close
	if rename {
		backend, release, err = attachWindowsRename(bounded, state, backend, principal, installation)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return backend, config, release, nil
}

func windowsWorkspaceVerifier(state *sqlite.WindowsScopedPools, reader *ScopeReader, principal app.CorePrincipal, limits model.CoreRuntimeLimits) WorkspaceVerifier {
	return func(ctx context.Context, actor app.CorePrincipal, member model.ScopeMember, captured *snapshot.Snapshot) (app.WorkspaceObservation, error) {
		if ctx == nil || actor != principal || captured == nil {
			return app.WorkspaceUnknown, ErrAuthority
		}
		manifest := captured.Manifest()
		if manifest.WorktreeID != member.WorktreeID || manifest.ProfileID != member.ProfileID || manifest.PolicyDigest != member.PolicyDigest ||
			manifest.SnapshotDigest != member.SnapshotDigest || model.VerifyCaptureDigest(manifest, windowsManifestLimit) != nil ||
			!app.WorkspaceCaptureComparable(manifest) {
			return app.WorkspaceUnknown, app.ErrFreshnessUnknown
		}
		check := func() (ScopeObservation, error) {
			current, err := reader.Resolve(ctx, actor, member.GenerationID)
			if err == nil && (len(current.Vector.Members) != 1 || current.Vector.Members[0] != member) {
				err = ErrScopeChanged
			}
			return current, err
		}
		current, err := check()
		if err != nil {
			return app.WorkspaceUnknown, err
		}
		capture, err := newWindowsIndexCapture(sqlite.WindowsIndexScope{Binding: current.Binding, Target: state.Target,
			CommonGit: state.CommonGit, LinkedGit: state.LinkedGit, Check: state.Check, ReadIgnore: state.ReadIgnore}, limits)
		if err != nil {
			return app.WorkspaceUnknown, err
		}
		observed, err := capture.capture(ctx)
		if err != nil {
			return app.WorkspaceUnknown, err
		}
		if _, err := check(); err != nil {
			return app.WorkspaceUnknown, err
		}
		if observed.Manifest().SnapshotDigest != manifest.SnapshotDigest {
			return app.WorkspaceChanged, nil
		}
		return app.WorkspaceObservedCurrent, nil
	}
}

type windowsInspectionConfig struct{ pools *sqlite.WindowsScopedPools }

func (c windowsInspectionConfig) Read(ctx context.Context, maxBytes int) ([]byte, bool, error) {
	return c.pools.ReadConfig(ctx, maxBytes)
}

func (windowsInspectionConfig) Set(context.Context, []byte, int) (ports.RuntimeConfigWriteResult, error) {
	return ports.RuntimeConfigWriteResult{State: "not_applied"}, app.ErrAdminDenied
}
