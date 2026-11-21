//go:build linux

package compose

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

func (r *nativeRecordOwner) ApplyMapping(ctx context.Context, input model.Request, authorize func(context.Context) error) (out model.RecordData, resultErr error) {
	if err := authorize(ctx); err != nil {
		return out, err
	}
	var mutation records.MutationRequest
	var selection *model.ScopeRequest
	var operation, schema string
	switch request := input.(type) {
	case model.MappingProposeRequest:
		operation, schema, selection = "mapping_propose", "MappingProposeRequest", request.Scope
		mutation.Action, mutation.Reason = "propose", request.Rationale
		mutation.Mutation.MutationID = request.ClientMutationID
		mutation.Mapping = &model.UIMappingPayload{SourceRef: request.SourceRef, DestinationRef: request.DestinationRef, Rationale: request.Rationale}
	case model.MappingReviewRequest:
		operation, schema, selection = "mapping_review", "MappingReviewRequest", request.Scope
		mutation.Action, mutation.Reason = request.Action, request.Reason
		mutation.RecordID, mutation.ExpectedVersion = request.RecordID, request.ExpectedVersion
		mutation.Mutation.MutationID = request.ClientMutationID
	default:
		return out, app.ErrCoreInput
	}
	if err := r.scanner.CheckContent([]byte(mutation.Reason)); err != nil {
		return out, app.ErrCoreAuthentication
	}
	var generation string
	var pinned *model.ScopeVector
	if selection != nil {
		mode, err := selection.SelectionMode(1)
		if err != nil {
			return out, app.ErrCoreInput
		}
		switch mode {
		case "group":
			return out, app.ErrCoreUnsupported
		case "pinned":
			pinned = selection.PinnedVector
			if pinned.Kind != "single" || len(pinned.Members) != 1 || len(pinned.UnavailableMembers) != 0 ||
				model.VerifyScopeVectorDigest(*pinned, 32768) != nil {
				return out, app.ErrCoreInput
			}
			member := pinned.Members[0]
			binding := r.scope.binding
			if member.WorktreeID != binding.WorktreeID || member.ProfileID != binding.ProfileID ||
				member.PolicyEpoch != binding.PolicyEpoch || member.PolicyDigest != binding.PolicyDigest ||
				member.BindingRevision != binding.BindingRevision || pinned.CatalogIncarnation != binding.CatalogIncarnation {
				return out, app.ErrCoreAuthentication
			}
			generation = member.GenerationID
			mutation.Expected = &records.State{Identity: records.Identity{WorktreeID: member.WorktreeID, Incarnation: member.RecordsIncarnation},
				RecordEpoch: member.RecordEpoch}
		case "single":
			if *selection.WorktreeID != r.reader.WorktreeID || *selection.ProfileID != r.reader.ProfileID {
				return out, app.ErrCoreAuthentication
			}
			if selection.GenerationID != nil {
				generation = *selection.GenerationID
			}
		}
	}
	hash, err := model.RequestDigest(operation, r.principal.ID, schema, input, 32768, 1)
	if err != nil {
		return out, app.ErrCoreInput
	}
	mutation.Mutation.RequestHash, mutation.Mutation.PrincipalID = hash, r.principal.ID
	job, err := nativeOperationID()
	if err != nil {
		return out, err
	}
	_, lease, _, err := r.scope.stores.CacheResources(ctx)
	if err != nil {
		return out, err
	}
	wait, err := nativeMilliseconds(r.scope.effective.Core.WriterLockWaitMs)
	if err != nil {
		return out, err
	}
	writer, err := runtime.AcquireWriter(ctx, lease, job, wait)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	stamp, err := r.ObserveRecords(ctx, nil)
	if err != nil {
		return out, err
	}
	mutation.Writer, mutation.JobID = writer, job
	mutation.Authorize = func(ctx context.Context, id records.Identity) error {
		if err := authorize(ctx); err != nil {
			return err
		}
		return r.authorize(ctx, id)
	}
	mutation.CheckEvidence = func(ctx context.Context, mapping model.UIMappingPayload) error {
		if generation != "" && mapping.SourceRef.GenerationID != generation {
			return app.ErrRecordsChanged
		}
		if pinned != nil {
			expect := pinned.Members[0]
			if _, err := model.MatchPinnedMember(*pinned, mapping.DestinationRef, nil); err != nil {
				return app.ErrRecordsChanged
			}
			actual, err := r.reader.Index.ReadGeneration(ctx, expect.ProfileID, expect.GenerationID, r.generation, r.reader.MaxManifestBytes)
			if err != nil {
				return recordScopeError(err)
			}
			if actual.Identity.Incarnation != expect.IndexIncarnation || actual.SnapshotDigest != expect.SnapshotDigest {
				return app.ErrRecordsChanged
			}
		}
		label, err := r.RecordProvenance(ctx, stamp, ports.StoredRecord{SourceRefs: []model.SourceRef{mapping.SourceRef}, UIMapping: &mapping})
		if err != nil {
			return err
		}
		if label != "retained_snapshot" {
			return app.ErrCoreSnapshotExpired
		}
		return nil
	}
	result, err := r.scope.stores.MutateRecord(ctx, mutation)
	if errors.Is(err, records.ErrRecordChanged) {
		return out, app.ErrRecordsChanged
	}
	if err != nil {
		return out, err
	}
	out = model.RecordData{Operation: operation, RecordID: result.Record.RecordID, Version: result.Record.Version,
		RecordEpoch: result.State.RecordEpoch, DeclaredStatus: result.Record.DeclaredStatus, EffectiveStatus: result.Record.DeclaredStatus}
	if out.DeclaredStatus == "approved" {
		state := app.RecordSnapshot{Incarnation: result.State.Identity.Incarnation, Epoch: result.State.RecordEpoch}
		label, err := r.RecordProvenance(ctx, state, result.Record)
		if err != nil {
			return model.RecordData{}, errors.Join(records.ErrAfterCommit, err)
		}
		if label != "retained_snapshot" {
			out.EffectiveStatus = "needs_review"
		}
	}
	return out, nil
}
