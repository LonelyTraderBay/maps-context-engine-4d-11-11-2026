package compose

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// NewLocalWorkspaceVerifier binds a captured inventory and scope to one
// caller-owned writer lease. The owner must supply the original inventory and
// real authorization and dependency proofs; this constructor grants no access.
// Recreate the adapter when the pinned generation changes.
func NewLocalWorkspaceVerifier(reader *ScopeReader, writer ports.IndexWriterLease, jobID string, scope model.ScopeVector, request indexer.RevalidateRequest, linked *runtime.LinkedGitMetadata) (WorkspaceVerifier, error) {
	if reader == nil || writer == nil || len(jobID) != 36 || request.Snapshot == nil || request.Inventory.Root == nil || request.Inventory.Root != reader.WorktreeRoot || request.Inventory.Config == nil || request.Inventory.Scanner == nil || request.Inventory.AuthorizeRoot == nil || request.Inventory.AuthorizePath == nil || request.VerifyAuthority == nil || request.VerifyDependencies == nil || request.MaxManifestBytes < 1024 || request.MaxManifestBytes > 64*1024*1024 || request.MaxFileBytes < 1 || request.MaxFileBytes > 64*1024*1024 || request.MaxSourceBytes < 1 || request.Inventory.MaxFileMapEntries < 1 || request.Inventory.MaxFileMapEntries > 100000 || len(request.Expected.Entries) > request.Inventory.MaxFileMapEntries || reader.LinkedRelative == "" && linked != nil || reader.LinkedRelative != "" && linked == nil || !boundedSingleScope(scope) {
		return nil, ErrScopeInput
	}
	raw, err := json.Marshal(scope)
	if err != nil || len(raw) > 32768 {
		return nil, ErrScopeInput
	}
	pinned, err := model.DecodeScopeVectorJSON(raw, 32768)
	if err != nil || model.VerifyScopeVectorDigest(pinned, 32768) != nil || pinned.Kind != "single" || len(pinned.Members) != 1 || len(pinned.UnavailableMembers) != 0 {
		return nil, ErrScopeInput
	}
	member := pinned.Members[0]
	manifest := request.Snapshot.Manifest()
	if member.WorktreeID != reader.WorktreeID || member.ProfileID != reader.ProfileID || manifest.WorktreeID != member.WorktreeID || manifest.ProfileID != member.ProfileID || manifest.SnapshotDigest != member.SnapshotDigest || manifest.PolicyDigest != member.PolicyDigest || model.VerifyCaptureDigest(manifest, request.MaxManifestBytes) != nil || len(manifest.Files) != len(request.Expected.Entries) {
		return nil, ErrScopeInput
	}
	if !app.WorkspaceCaptureComparable(manifest) {
		return nil, app.ErrFreshnessUnknown
	}
	// The caller may reuse or mutate its own inventory after construction.
	// Bound copying before allocating and retain only this generation's map.
	request.Expected.Entries = append([]inventory.FileMapEntry(nil), request.Expected.Entries...)
	var total int
	for i := range request.Expected.Entries {
		entry := &request.Expected.Entries[i]
		total += len(entry.Path) + len(entry.Origin) + len(entry.State) + len(entry.Stages)
		if total > request.MaxManifestBytes || entry.Path != manifest.Files[i].Path || len(entry.Origin) > 64 || len(entry.State) > 64 || len(entry.Stages) > 4 {
			return nil, ErrScopeInput
		}
		entry.Stages = append([]uint8(nil), entry.Stages...)
	}
	if linked != nil && linked.VerifyRoots(reader.WorktreeRoot, reader.CommonRoot) != nil {
		return nil, ErrScopeInput
	}
	request.Snapshot = nil // retain the identity and inventory, not source bytes
	originalAuthority := request.VerifyAuthority
	return func(ctx context.Context, principal app.CorePrincipal, asked model.ScopeMember, captured *snapshot.Snapshot) (observation app.WorkspaceObservation, resultErr error) {
		if ctx == nil || captured == nil || !reflect.DeepEqual(asked, member) {
			return app.WorkspaceUnknown, ErrScopeChanged
		}
		if _, ok := ctx.Deadline(); !ok {
			return app.WorkspaceUnknown, ErrScopeInput
		}
		currentManifest := captured.Manifest()
		if currentManifest.SnapshotDigest != manifest.SnapshotDigest || currentManifest.WorktreeID != member.WorktreeID || currentManifest.ProfileID != member.ProfileID || currentManifest.PolicyDigest != member.PolicyDigest || model.VerifyCaptureDigest(currentManifest, request.MaxManifestBytes) != nil {
			return app.WorkspaceUnknown, ErrScopeChanged
		}
		if _, err := reader.Recheck(ctx, principal, pinned); err != nil {
			return app.WorkspaceUnknown, err
		}
		guard, err := writer.HoldWriter(ctx, member.WorktreeID, jobID)
		if err != nil || guard == nil {
			return app.WorkspaceUnknown, errors.Join(ErrScopeInput, err)
		}
		defer func() {
			if err := guard.Close(); err != nil {
				observation, resultErr = app.WorkspaceUnknown, errors.Join(resultErr, err)
			}
		}()
		check := func(ctx context.Context, observed model.CaptureManifest) error {
			if observed.SnapshotDigest != member.SnapshotDigest || observed.PolicyDigest != member.PolicyDigest || observed.WorktreeID != member.WorktreeID || observed.ProfileID != member.ProfileID {
				return ErrScopeChanged
			}
			if err := writer.CheckWriter(ctx, member.WorktreeID, jobID); err != nil {
				return err
			}
			if _, err := reader.Recheck(ctx, principal, pinned); err != nil {
				return err
			}
			if err := originalAuthority(ctx, observed); err != nil {
				return err
			}
			return writer.CheckWriter(ctx, member.WorktreeID, jobID)
		}
		bound := request
		bound.Snapshot = captured
		bound.VerifyAuthority = check
		if linked == nil {
			err = indexer.RevalidateLocal(ctx, bound)
		} else {
			err = indexer.RevalidateLinked(ctx, bound, linked, reader.CommonRoot)
		}
		if err != nil && !errors.Is(err, indexer.ErrSnapshotChanged) {
			return app.WorkspaceUnknown, err
		}
		if postcheck := check(ctx, currentManifest); postcheck != nil {
			return app.WorkspaceUnknown, postcheck
		}
		if errors.Is(err, indexer.ErrSnapshotChanged) {
			return app.WorkspaceChanged, nil
		}
		return app.WorkspaceObservedCurrent, nil
	}, nil
}

func boundedSingleScope(scope model.ScopeVector) bool {
	if scope.Kind != "single" || len(scope.Members) != 1 || len(scope.UnavailableMembers) != 0 || scope.GroupID != nil || scope.GroupRevision != nil || len(scope.VectorDigest) != 64 || len(scope.CatalogIncarnation) != 36 {
		return false
	}
	m := scope.Members[0]
	return len(m.WorktreeID) == 36 && len(m.ProfileID) == 64 && len(m.GenerationID) == 36 && len(m.SnapshotDigest) == 64 && len(m.PolicyDigest) == 64 && len(m.IndexIncarnation) == 36 && len(m.RecordsIncarnation) == 36 && (m.ServiceID == nil || len(*m.ServiceID) <= 768)
}
