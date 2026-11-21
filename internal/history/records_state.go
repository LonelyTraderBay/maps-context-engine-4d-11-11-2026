// Package history resolves approved declarations against current authority.
package history

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/domain/state"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrStateModelApproval = errors.New("state model has no current approved source in the pinned scope")

// StateRecordReader is implemented by the existing authority-bound record
// service. Historical versions are unavailable for this projection.
type StateRecordReader interface {
	Read(context.Context, model.RecordReadRequest) (app.RecordReadResult, error)
}

// StateSourceReader obtains bytes from the same authorized capture. The bound
// limits materialization and the caller checks the entire captured file hash.
type StateSourceReader func(context.Context, model.SourceRef, int) ([]byte, error)

type ApprovedStateModel struct {
	RecordID      string
	RecordVersion int64
	ScopeDigest   string
	Source        model.SourceRef
	Definition    state.Definition
	Reachability  string
}

// ResolveStateModel returns a declarative view of one approved current head.
// Re-observation after decoding detects changed permission, epoch, or source.
func ResolveStateModel(ctx context.Context, records StateRecordReader, source StateSourceReader, request model.RecordReadRequest, scope model.ScopeVector) (ApprovedStateModel, error) {
	const vectorBudget = 256 << 10
	if ctx == nil || records == nil || source == nil || request.Version != nil ||
		model.VerifyScopeVectorDigest(scope, vectorBudget) != nil {
		return ApprovedStateModel{}, ErrStateModelApproval
	}
	before, ref, err := approvedStateHead(ctx, records, request, scope)
	if err != nil {
		return ApprovedStateModel{}, err
	}
	raw, err := source(ctx, ref, contracts.StateModelMaxBytes)
	if err != nil {
		return ApprovedStateModel{}, err
	}
	definition, err := state.DecodeCaptured(ref, raw)
	if err != nil {
		return ApprovedStateModel{}, err
	}
	after, proof, err := approvedStateHead(ctx, records, request, scope)
	if err != nil {
		return ApprovedStateModel{}, err
	}
	if before.Version != after.Version || ref != proof {
		return ApprovedStateModel{}, ErrStateModelApproval
	}
	if err := ctx.Err(); err != nil {
		return ApprovedStateModel{}, err
	}
	return ApprovedStateModel{RecordID: before.RecordID, RecordVersion: before.Version,
		ScopeDigest: scope.VectorDigest, Source: ref, Definition: definition, Reachability: "unknown"}, nil
}

func approvedStateHead(ctx context.Context, reader StateRecordReader, request model.RecordReadRequest, pinned model.ScopeVector) (model.RecordSummary, model.SourceRef, error) {
	read, err := reader.Read(ctx, request)
	if err != nil {
		return model.RecordSummary{}, model.SourceRef{}, err
	}
	record := read.Data.Record
	if read.Scope == nil || model.ComparePinnedScope(pinned, *read.Scope, 256<<10) != nil ||
		record.RecordID != request.RecordID || record.Version < 1 || record.RecordKind != "state_model" ||
		record.DeclaredStatus != "approved" || record.EffectiveStatus != "approved" ||
		record.ProvenanceStatus != "retained_snapshot" || len(record.SourceRefs) != 1 || len(read.Data.Limitations) != 0 {
		return model.RecordSummary{}, model.SourceRef{}, ErrStateModelApproval
	}
	ref := record.SourceRefs[0]
	if _, err := model.MatchPinnedSource(pinned, ref, nil, 256<<10); err != nil || !state.EligibleSource(ref) {
		return model.RecordSummary{}, model.SourceRef{}, ErrStateModelApproval
	}
	return record, ref, nil
}
