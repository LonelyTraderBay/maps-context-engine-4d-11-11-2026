//go:build linux

package compose

import (
	"context"
	"errors"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// This verifier borrows immutable generation bytes and short read transactions.
// It never takes the foreground watch's writer lease or changes active scope.
func nativeWorkspaceVerifier(n *nativeScope, reader *ScopeReader, principal app.CorePrincipal) WorkspaceVerifier {
	return func(ctx context.Context, actor app.CorePrincipal, member model.ScopeMember, captured *snapshot.Snapshot) (observation app.WorkspaceObservation, resultErr error) {
		if ctx == nil || actor != principal || captured == nil {
			return app.WorkspaceUnknown, ErrAuthority
		}
		if _, ok := ctx.Deadline(); !ok {
			return app.WorkspaceUnknown, ErrScopeInput
		}
		check := func() error {
			current, err := reader.Resolve(ctx, actor, member.GenerationID)
			if err != nil {
				return err
			}
			if len(current.Vector.Members) != 1 || !reflect.DeepEqual(current.Vector.Members[0], member) {
				return ErrScopeChanged
			}
			return n.recheckBinding(ctx)
		}
		if err := check(); err != nil {
			return app.WorkspaceUnknown, err
		}
		manifest := captured.Manifest()
		if manifest.WorktreeID != member.WorktreeID || manifest.ProfileID != member.ProfileID || manifest.PolicyDigest != member.PolicyDigest || manifest.SnapshotDigest != member.SnapshotDigest || model.VerifyCaptureDigest(manifest, 64<<20) != nil || !app.WorkspaceCaptureComparable(manifest) {
			return app.WorkspaceUnknown, app.ErrFreshnessUnknown
		}
		i, err := openNativeInventory(ctx, n, false)
		if err != nil {
			return app.WorkspaceUnknown, err
		}
		defer func() {
			if err := i.Close(); err != nil {
				observation, resultErr = app.WorkspaceUnknown, errors.Join(resultErr, err)
			}
		}()
		blocked := map[string]string{}
		for _, file := range manifest.Files {
			if file.Status == "excluded" && file.ReasonCode != nil {
				switch *file.ReasonCode {
				case "SECRET_BLOCKED", "BINARY_FILE", "FILE_TOO_LARGE":
					blocked[file.Path] = *file.ReasonCode
				}
			}
		}
		previous := i.request.Exclude
		i.request.Exclude = func(name string, directory bool) bool { return blocked[name] != "" || previous(name, directory) }
		i.matchManifestInventory = true

		proof, err := n.state.ObserveCatalogReadFence(ctx)
		if err != nil {
			return app.WorkspaceUnknown, err
		}
		if err := n.recheckBinding(ctx); err != nil {
			return app.WorkspaceUnknown, err
		}
		// Validate the full binding between two observations of the same
		// authenticated head. A later head change needs a new full read.
		if proof(ctx) == nil {
			authorizeRoot := func(ctx context.Context) error {
				if err := proof(ctx); err != nil {
					return n.recheckBinding(ctx)
				}
				return errors.Join(n.stores.Check(ctx), n.binding.VerifyRoots(n.target, n.common, n.linked))
			}
			i.authorizeSourceRoot = authorizeRoot
			i.request.AuthorizeRoot = authorizeRoot
		}
		err = i.observeCaptured(ctx, nativeCapture{snapshot: captured, blocked: blocked}, captured, false)

		if postcheck := check(); postcheck != nil {
			return app.WorkspaceUnknown, errors.Join(err, postcheck)
		}
		if errors.Is(err, indexer.ErrSnapshotChanged) {
			return app.WorkspaceChanged, nil
		}
		if err != nil {
			return app.WorkspaceUnknown, err
		}
		return app.WorkspaceObservedCurrent, nil
	}
}
