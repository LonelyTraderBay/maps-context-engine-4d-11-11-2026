//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

type nativeRecordOwner struct {
	scope     *nativeScope
	reader    ScopeReader
	principal app.CorePrincipal
	scanner   *policy.Scanner
	maxFile   int
}

func OpenNativeRecords(ctx context.Context, installation, config, worktree, profile string, cpus int) (_ *app.RecordService, _ func() error, resultErr error) {
	scope, err := openNativeScope(ctx, installation, config, worktree, profile, cpus)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, scope.Close())
		}
	}()
	key, err := scope.state.CursorKey(ctx)
	if err != nil {
		return nil, nil, err
	}
	cursors, err := contextpack.NewCursorCodec(key)
	clear(key[:])
	if err != nil {
		return nil, nil, err
	}
	service, err := bindNativeRecords(scope, cursors)
	if err != nil {
		return nil, nil, err
	}
	return service, scope.Close, nil
}

func bindNativeRecords(scope *nativeScope, cursors *contextpack.CursorCodec) (*app.RecordService, error) {
	indexStore, err := scope.stores.Index.Store()
	if err != nil {
		return nil, err
	}
	recordStore, err := scope.stores.Records.Store()
	if err != nil {
		return nil, err
	}
	worktree, profile := scope.binding.WorktreeID, scope.binding.ProfileID
	maxFile := int(minInt64(scope.effective.Core.MaxFileBytes, scope.binding.Policy.MaxFileBytes, 2<<20))
	scanner, err := policy.NewScanner(maxFile)
	if err != nil {
		return nil, err
	}
	id := "installation:" + scope.state.InstallationID()
	digest := func(value string) string { hash := sha256.Sum256([]byte(value)); return hex.EncodeToString(hash[:]) }
	principal := app.CorePrincipal{ID: id, Digest: digest("MCE4D-OS-PRINCIPAL1\x00" + id),
		BindingDigest: digest("MCE4D-BINDING1\x00" + id + "\x00" + worktree + "\x00" + profile)}
	owner := &nativeRecordOwner{scope: scope, principal: principal, scanner: scanner, maxFile: maxFile}
	owner.reader = ScopeReader{Catalog: scope.catalog, Index: indexStore, Records: recordStore, WorktreeRoot: scope.target, CommonRoot: scope.common,
		LinkedRelative: scope.linked, WorktreeID: worktree, ProfileID: profile, MaxHeads: 100000, MaxManifestBytes: 64 << 20,
		Access: func(ctx context.Context, ask app.CorePrincipal, w, p string) error {
			if ask != principal || w != worktree || p != profile {
				return ErrAuthority
			}
			return scope.boundary(ctx)
		}}
	return &app.RecordService{Owner: owner, Cursors: cursors}, nil
}

func (r *nativeRecordOwner) authorize(ctx context.Context, id records.Identity) error {
	if id.WorktreeID != r.reader.WorktreeID {
		return app.ErrCoreAuthentication
	}
	return recordScopeError(r.scope.recheckBinding(ctx))
}

func recordScopeError(err error) error {
	switch {
	case errors.Is(err, ErrScopeInput):
		return errors.Join(app.ErrCoreInput, err)
	case errors.Is(err, ErrScopeSnapshotExpired), errors.Is(err, index.ErrSnapshotUnavailable):
		return errors.Join(app.ErrCoreSnapshotExpired, err)
	case errors.Is(err, ErrScopeChanged):
		return errors.Join(app.ErrRecordsChanged, err)
	case errors.Is(err, ErrAuthority):
		return errors.Join(app.ErrCoreAuthentication, err)
	default:
		return err
	}
}

func (r *nativeRecordOwner) ObserveRecords(ctx context.Context, requested *model.ScopeRequest) (app.RecordSnapshot, error) {
	selection := model.ScopeRequest{}
	if requested != nil {
		selection = *requested
	}
	mode, err := selection.SelectionMode(1)
	if err != nil {
		return app.RecordSnapshot{}, app.ErrCoreInput
	}
	var observed ScopeObservation
	var generation string
	switch mode {
	case "group":
		return app.RecordSnapshot{}, app.ErrCoreUnsupported
	case "pinned":
		observed, err = r.reader.Recheck(ctx, r.principal, *selection.PinnedVector)
	case "bound", "single":
		if mode == "single" && (*selection.WorktreeID != r.reader.WorktreeID || *selection.ProfileID != r.reader.ProfileID) {
			return app.RecordSnapshot{}, app.ErrCoreAuthentication
		}
		if selection.GenerationID != nil {
			generation = *selection.GenerationID
		}
		observed, err = r.reader.Resolve(ctx, r.principal, generation)
	}
	scoped := err == nil
	if !scoped {
		if mode == "pinned" || generation != "" || !errors.Is(err, index.ErrSnapshotUnavailable) {
			return app.RecordSnapshot{}, recordScopeError(err)
		}
		observed.Binding, err = r.reader.ObserveMissingIndex(ctx, r.principal)
		if err != nil {
			return app.RecordSnapshot{}, recordScopeError(err)
		}
		observed.Records, err = r.reader.Records.ReadState(ctx, r.reader.MaxHeads, r.authorize)
		if err != nil {
			return app.RecordSnapshot{}, err
		}
		indexIdentity, err := r.reader.Index.Identity(ctx)
		if err != nil {
			return app.RecordSnapshot{}, err
		}
		observed.Generation.Identity = indexIdentity
	}
	binding := observed.Binding
	stamp := app.RecordSnapshot{Incarnation: observed.Records.Identity.Incarnation, Epoch: observed.Records.RecordEpoch}
	if scoped {
		stamp.Scope = &observed.Vector
	}
	raw, err := json.Marshal(struct {
		Principal, Binding, Vector, Catalog, Policy, Index, Records string
		BindingRevision, PolicyEpoch, RecordEpoch                   int64
	}{r.principal.Digest, r.principal.BindingDigest, observed.Vector.VectorDigest, binding.CatalogIncarnation, binding.PolicyDigest,
		observed.Generation.Identity.Incarnation, stamp.Incarnation, binding.BindingRevision, binding.PolicyEpoch, stamp.Epoch})
	if err != nil {
		return app.RecordSnapshot{}, err
	}
	sum := sha256.Sum256(raw)
	stamp.Digest = hex.EncodeToString(sum[:])
	return stamp, ctx.Err()
}

func (r *nativeRecordOwner) ReadRecord(ctx context.Context, stamp app.RecordSnapshot, name string, version int64) (ports.StoredRecord, error) {
	row, err := r.reader.Records.ReadVersion(ctx, name, version, r.authorize)
	if errors.Is(err, records.ErrRecordMissing) {
		return ports.StoredRecord{}, app.ErrRecordsMissing
	}
	if err != nil {
		return ports.StoredRecord{}, err
	}
	if row.State.Identity.Incarnation != stamp.Incarnation || row.State.RecordEpoch != stamp.Epoch {
		return ports.StoredRecord{}, app.ErrRecordsChanged
	}
	return row.Record, nil
}

func (r *nativeRecordOwner) ReadRecordHeads(ctx context.Context, stamp app.RecordSnapshot, after, kind string, limit int) ([]ports.StoredRecord, bool, error) {
	expected := records.State{Identity: records.Identity{WorktreeID: r.reader.WorktreeID, Incarnation: stamp.Incarnation}, RecordEpoch: stamp.Epoch}
	page, err := r.reader.Records.ReadHeadPage(ctx, records.HeadPageRequest{AfterID: after, Kind: kind, Limit: limit, Expected: &expected, Authorize: r.authorize})
	if errors.Is(err, records.ErrRecordChanged) {
		return nil, false, app.ErrRecordsChanged
	}
	if errors.Is(err, records.ErrInvalidStore) {
		return nil, false, app.ErrCoreInput
	}
	if err != nil {
		return nil, false, err
	}
	return page.Records, page.HasMore, nil
}

func (r *nativeRecordOwner) generation(ctx context.Context, worktree, profile, digest string, epoch int64) error {
	binding := r.scope.binding
	if worktree != binding.WorktreeID || profile != binding.ProfileID || digest != binding.PolicyDigest || epoch != binding.PolicyEpoch {
		return ErrAuthority
	}
	return r.scope.recheckBinding(ctx)
}

func (r *nativeRecordOwner) path(ctx context.Context, worktree, path, digest string, epoch int64) error {
	if err := policy.CheckConfiguredPath(path, r.scope.binding.Policy.DeniedGlobs); err != nil {
		return ErrAuthority
	}
	return r.generation(ctx, worktree, r.reader.ProfileID, digest, epoch)
}

func (r *nativeRecordOwner) file(ctx context.Context, generation, path string) (index.PublishedFile, error) {
	return r.reader.Index.ReadPublishedFile(ctx, index.PublishedFileRequest{ProfileID: r.reader.ProfileID, GenerationID: generation,
		Path: path, AuthorizeGeneration: r.generation, AuthorizePath: r.path, Scanner: r.scanner,
		MaxManifestBytes: r.reader.MaxManifestBytes, MaxFileBytes: r.maxFile})
}

func (r *nativeRecordOwner) verifySource(ctx context.Context, source model.SourceRef) (string, error) {
	if source.WorktreeID != r.reader.WorktreeID || source.ProfileID != r.reader.ProfileID {
		return "access_denied", nil
	}
	captured, err := r.file(ctx, source.GenerationID, source.Path)
	if err != nil {
		return recordSourceState(ctx, err)
	}
	file, err := snapshot.NewFile(source.Path, captured.Bytes, r.maxFile)
	if err != nil {
		return "unverified", nil
	}
	if file.ContentHash() != source.ContentHash || source.Range.StartByte < 0 || source.Range.EndByte > int64(file.Len()) ||
		file.CheckRange(int(source.Range.StartByte), int(source.Range.EndByte)) != nil {
		return "source_changed", nil
	}
	live, err := inventory.CaptureFile(ctx, r.scope.target, r.scanner, source.Path, r.maxFile, func(ctx context.Context, path string) error {
		return r.path(ctx, source.WorktreeID, path, captured.PolicyDigest, captured.PolicyEpoch)
	})
	if err != nil {
		return recordSourceState(ctx, err)
	}
	if live.ContentHash() != source.ContentHash {
		return "source_changed", nil
	}
	return "retained_snapshot", nil
}

func recordSourceState(ctx context.Context, err error) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	switch {
	case errors.Is(err, index.ErrSnapshotUnavailable), errors.Is(err, ErrScopeSnapshotExpired):
		return "snapshot_expired", nil
	case errors.Is(err, ErrAuthority), errors.Is(err, policy.ErrSecretBlocked), errors.Is(err, fs.ErrPermission):
		return "access_denied", nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, index.ErrPublishedFileUnavailable):
		return "source_changed", nil
	default:
		return "unverified", nil
	}
}

func (r *nativeRecordOwner) RecordProvenance(ctx context.Context, stamp app.RecordSnapshot, record ports.StoredRecord) (string, error) {
	if err := r.scope.recheckBinding(ctx); err != nil {
		return "", recordScopeError(err)
	}
	if len(record.SourceRefs) == 0 {
		return "unverified", nil
	}
	for _, source := range record.SourceRefs {
		label, err := r.verifySource(ctx, source)
		if err != nil || label != "retained_snapshot" {
			return label, err
		}
	}
	if record.UIMapping == nil {
		return "retained_snapshot", nil
	}
	if err := r.scanner.CheckContent([]byte(record.UIMapping.Rationale)); err != nil {
		return "access_denied", nil
	}
	switch destination := record.UIMapping.DestinationRef.(type) {
	case *model.FileRef:
		if destination.WorktreeID != r.reader.WorktreeID || destination.ProfileID != r.reader.ProfileID || destination.ContentHash == nil {
			return "unverified", nil
		}
		return r.verifySource(ctx, model.SourceRef{WorktreeID: destination.WorktreeID, ProfileID: destination.ProfileID,
			GenerationID: destination.GenerationID, Path: destination.Path, ContentHash: *destination.ContentHash})
	case *model.SymbolRef:
		if destination.WorktreeID != r.reader.WorktreeID || destination.ProfileID != r.reader.ProfileID {
			return "access_denied", nil
		}
		generation, err := r.reader.Index.ReadGeneration(ctx, r.reader.ProfileID, destination.GenerationID, r.generation, r.reader.MaxManifestBytes)
		if err != nil {
			return recordSourceState(ctx, err)
		}
		member := model.ScopeMember{WorktreeID: r.reader.WorktreeID, ProfileID: r.reader.ProfileID, GenerationID: generation.GenerationID,
			SnapshotDigest: generation.SnapshotDigest, PolicyDigest: generation.PolicyDigest, PolicyEpoch: generation.PolicyEpoch,
			BindingRevision: r.scope.binding.BindingRevision, RecordEpoch: stamp.Epoch, RecordsIncarnation: stamp.Incarnation, IndexIncarnation: generation.Identity.Incarnation}
		vector := model.ScopeVector{Kind: "single", CatalogIncarnation: r.scope.binding.CatalogIncarnation,
			Members: []model.ScopeMember{member}, UnavailableMembers: []model.MemberUnavailable{}}
		vector.VectorDigest, err = model.ComputeScopeVectorDigest(vector, 32768)
		if err != nil {
			return "unverified", nil
		}
		facts, err := r.reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: vector.VectorDigest,
			AuthorizeGeneration: r.generation, AuthorizePath: r.path, Scanner: r.scanner, MaxManifestBytes: r.reader.MaxManifestBytes, MaxFileBytes: r.maxFile,
			MaxSourceBytes: 128 << 20, MaxFactBytes: 16 << 20, MaxFacts: 50000})
		if err != nil {
			return recordSourceState(ctx, err)
		}
		for _, symbol := range facts.Symbols {
			if symbol.SymbolID != destination.SymbolID || symbol.Path == nil || symbol.Range == nil {
				continue
			}
			source, ok := facts.Snapshot.File(*symbol.Path)
			if !ok {
				return "unverified", nil
			}
			return r.verifySource(ctx, model.SourceRef{WorktreeID: destination.WorktreeID, ProfileID: destination.ProfileID,
				GenerationID: destination.GenerationID, Path: *symbol.Path, Range: *symbol.Range, ContentHash: source.ContentHash()})
		}
		return "source_changed", nil
	default:
		return "unverified", nil
	}
}
