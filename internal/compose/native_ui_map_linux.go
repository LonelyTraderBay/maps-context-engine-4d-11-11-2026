//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/uimap"
)

type UIMapImport struct {
	BuildMapID string        `json:"build_map_id"`
	BindingID  string        `json:"binding_id,omitempty"`
	Status     string        `json:"status"`
	WriterWait time.Duration `json:"-"`
}

func RunNativeUIMapImport(ctx context.Context, installation, config, worktree, artifactPath string, cpus int) (out UIMapImport, resultErr error) {
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return out, err
	}
	artifact, err := state.ReadUIBuild(ctx, artifactPath, worktree)
	if err := errors.Join(err, state.Close()); err != nil {
		return out, err
	}
	return importNativeUIMap(ctx, installation, config, worktree, "", "", artifact, cpus)
}

func importNativeUIMap(ctx context.Context, installation, config, worktree, profileID, buildID string,
	artifact uimap.ImportedBuild, cpus int) (out UIMapImport, resultErr error) {
	build, _, err := artifact.Manifest()
	if err != nil || len(build.Sources) > 8192 || len(build.Anchors) > 8192 ||
		profileID != "" && build.ProfileID != profileID || buildID != "" && build.BuildMapID != buildID {
		return out, uimap.ErrBuildArtifact
	}
	n, err := openNativeScope(ctx, installation, config, worktree, build.ProfileID, cpus)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, n.Close()) }()
	for _, file := range build.Sources {
		if policy.CheckConfiguredPath(file.Path, n.binding.Policy.DeniedGlobs) != nil {
			return out, ErrAuthority
		}
	}
	scanner, err := policy.NewScanner(int(minInt64(n.effective.Core.MaxFileBytes, n.binding.Policy.MaxFileBytes, 2<<20)))
	if err != nil {
		return out, err
	}
	if scanner.CheckContent([]byte(build.ProviderVersion)) != nil {
		return out, ErrAuthority
	}
	idx, err := n.stores.Index.Store()
	if err != nil {
		return out, err
	}
	recordStore, err := n.stores.Records.Store()
	if err != nil {
		return out, err
	}
	_, lease, _, err := n.stores.CacheResources(ctx)
	if err != nil {
		return out, err
	}
	wait, err := nativeMilliseconds(n.effective.Core.WriterLockWaitMs)
	if err != nil {
		return out, err
	}
	job, err := nativeOperationID()
	if err != nil {
		return out, err
	}
	waitStarted := time.Now()
	writer, err := runtime.AcquireWriter(ctx, lease, job, wait)
	out.WriterWait = time.Since(waitStarted)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	profile := build.ProfileID
	auth := index.UIWriteAuthority{Writer: writer, JobID: job, Authorize: func(ctx context.Context, identity index.Identity) error {
		if identity.WorktreeID != worktree {
			return ErrAuthority
		}
		return n.recheckBinding(ctx)
	}}
	generation := func(ctx context.Context, w, p, digest string, epoch int64) error {
		if w != worktree || p != profile || digest != n.binding.PolicyDigest || epoch != n.binding.PolicyEpoch {
			return ErrScopeChanged
		}
		return n.recheckBinding(ctx)
	}
	principal := app.CorePrincipal{ID: "installation:" + n.state.InstallationID(),
		Digest:        uiMapDigest("MCE4D-OS-PRINCIPAL1\x00" + n.state.InstallationID()),
		BindingDigest: uiMapDigest("MCE4D-BINDING1\x00" + n.state.InstallationID() + "\x00" + worktree + "\x00" + profile)}
	reader := ScopeReader{Catalog: n.catalog, Index: idx, Records: recordStore, WorktreeRoot: n.target,
		CommonRoot: n.common, LinkedRelative: n.linked, WorktreeID: worktree, ProfileID: profile,
		MaxHeads: 100000, MaxManifestBytes: 16 << 20,
		Access: func(ctx context.Context, ask app.CorePrincipal, w, p string) error {
			if ask != principal || w != worktree || p != profile {
				return ErrAuthority
			}
			return n.boundary(ctx)
		}}
	err = n.stores.UpdateUIIndex(ctx, func(store *index.UIStore) error {
		if err := store.ImportBuildMap(ctx, auth, artifact); err != nil {
			return err
		}
		out.BuildMapID, out.Status = build.BuildMapID, "imported_unbound"
		scope, err := reader.Resolve(ctx, principal, "")
		if errors.Is(err, index.ErrSnapshotUnavailable) || errors.Is(err, ErrScopeChanged) {
			return n.recheckBinding(ctx)
		}
		if err != nil {
			return err
		}
		if err := generation(ctx, worktree, profile, scope.Binding.PolicyDigest, scope.Binding.PolicyEpoch); err != nil {
			return err
		}
		link, err := store.BindBuildMap(ctx, index.UIBindRequest{Authority: auth, BuildMapID: build.BuildMapID,
			Member: scope.Vector.Members[0], Resolver: n.uiResolver(),
			AuthorizeGeneration: generation, Scanner: scanner, MaxBytes: minInt64(n.binding.Policy.MaxSourceBytes, 512<<20),
			AuthorizePath: func(ctx context.Context, w, name, digest string, epoch int64) error {
				if err := policy.CheckConfiguredPath(name, n.binding.Policy.DeniedGlobs); err != nil {
					return err
				}
				return generation(ctx, w, profile, digest, epoch)
			}})
		if errors.Is(err, index.ErrCaptureMismatch) || errors.Is(err, index.ErrSnapshotUnavailable) {
			return n.recheckBinding(ctx)
		}
		if err != nil {
			return err
		}
		out.BindingID, out.Status = link.BindingID, "bound"
		return n.recheckBinding(ctx)
	})
	return out, err
}

func (n *nativeScope) uiResolver() model.ResolverIdentity {
	return model.ResolverIdentity{ImplementationArtifactHash: n.state.ExecutableDigest(), SemanticVersion: "1.0.0",
		OptionsDigest: uiMapDigest("MCE4D-UI-BIND-OPTIONS1\x00source_site_then_unique_exact_name")}
}

func uiMapDigest(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}
