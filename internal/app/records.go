package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var (
	ErrRecordsChanged = errors.New("record scope changed during request")
	ErrRecordsMissing = errors.New("requested record version is unavailable")
)

// RecordSnapshot is produced by a bound owner, including its authenticated
// principal, approved scope, policy revision and durable record epoch.
type RecordSnapshot struct {
	Digest      string
	Scope       *model.ScopeVector
	Incarnation string
	Epoch       int64
}

type RecordOwner interface {
	ObserveRecords(context.Context, *model.ScopeRequest) (RecordSnapshot, error)
	ReadRecord(context.Context, RecordSnapshot, string, int64) (ports.StoredRecord, error)
	ReadRecordHeads(context.Context, RecordSnapshot, string, string, int) ([]ports.StoredRecord, bool, error)
	RecordProvenance(context.Context, RecordSnapshot, ports.StoredRecord) (string, error)
}

type RecordService struct {
	Owner   RecordOwner
	Cursors *contextpack.CursorCodec
}

type RecordListResult struct {
	Data   model.RecordListData
	Scope  *model.ScopeVector
	Cursor *string
}

type RecordReadResult struct {
	Data  model.RecordReadData
	Scope *model.ScopeVector
}

type RecordMutationOwner interface {
	ApplyMapping(context.Context, model.Request, func(context.Context) error) (model.RecordData, error)
}

// ApplyMapping uses the authenticated transport guard through the owner's
// mutation transaction and derives the durable actor from the bound owner.
func (s *RecordService) ApplyMapping(ctx context.Context, request model.Request, authorize func(context.Context) error) (model.RecordData, error) {
	if s == nil || ctx == nil || authorize == nil {
		return model.RecordData{}, ErrCoreInput
	}
	owner, ok := s.Owner.(RecordMutationOwner)
	if !ok {
		return model.RecordData{}, ErrCoreUnsupported
	}
	return owner.ApplyMapping(ctx, request, authorize)
}

func (s *RecordService) List(ctx context.Context, request model.RecordListRequest) (RecordListResult, error) {
	if s == nil || s.Owner == nil || s.Cursors == nil || ctx == nil || request.Limit == nil ||
		*request.Limit < 1 || *request.Limit > 100 {
		return RecordListResult{}, ErrCoreInput
	}
	stamp, err := s.Owner.ObserveRecords(ctx, request.Scope)
	if err != nil {
		return RecordListResult{}, err
	}
	kind := ""
	if request.RecordKind != nil {
		kind = *request.RecordKind
	}
	cursor := request.Cursor
	request.Cursor = nil
	raw, err := json.Marshal(struct {
		Authority string                  `json:"authority"`
		Filter    model.RecordListRequest `json:"filter"`
	}{stamp.Digest, request})
	if err != nil {
		return RecordListResult{}, err
	}
	preimage, err := model.DecodeIdentityJSON(raw, 65536)
	if err != nil {
		return RecordListResult{}, ErrCoreInput
	}
	_, digest, err := model.EncodeCanonical("records-list-v1", preimage, 65536)
	if err != nil {
		return RecordListResult{}, err
	}
	after := ""
	if cursor != nil {
		after, err = s.Cursors.VerifyRecordList(*cursor, digest)
		if err != nil {
			return RecordListResult{}, err
		}
	}
	records, more, err := s.Owner.ReadRecordHeads(ctx, stamp, after, kind, int(*request.Limit))
	if err != nil {
		return RecordListResult{}, err
	}
	if len(records) > int(*request.Limit) || more && len(records) == 0 {
		return RecordListResult{}, ErrCoreBackend
	}
	data := model.RecordListData{Operation: "records_list", Records: make([]model.RecordSummary, 0, len(records))}
	proofs := make([]string, 0, len(records))
	for _, record := range records {
		if record.RecordID <= after {
			return RecordListResult{}, ErrCoreBackend
		}
		item, err := s.project(ctx, stamp, record)
		if err != nil {
			return RecordListResult{}, err
		}
		proofs = append(proofs, item.Record.ProvenanceStatus)
		if request.Status == nil || item.Record.EffectiveStatus == *request.Status {
			data.Records = append(data.Records, item.Record)
		}
		after = record.RecordID
	}
	if err := s.recheck(ctx, stamp, request.Scope); err != nil {
		return RecordListResult{}, err
	}
	for i, record := range records {
		current, err := s.Owner.RecordProvenance(ctx, stamp, record)
		if err != nil {
			return RecordListResult{}, err
		}
		if current != proofs[i] {
			return RecordListResult{}, ErrRecordsChanged
		}
	}
	if err := s.recheck(ctx, stamp, request.Scope); err != nil {
		return RecordListResult{}, err
	}
	out := RecordListResult{Data: data, Scope: stamp.Scope}
	if more {
		token, err := s.Cursors.IssueRecordList(digest, after)
		if err != nil {
			return RecordListResult{}, err
		}
		out.Cursor = &token
	}
	return out, nil
}

func (s *RecordService) Read(ctx context.Context, request model.RecordReadRequest) (RecordReadResult, error) {
	if s == nil || ctx == nil || s.Owner == nil || !model.ValidUUID(request.RecordID) {
		return RecordReadResult{}, ErrCoreInput
	}
	stamp, err := s.Owner.ObserveRecords(ctx, request.Scope)
	if err != nil {
		return RecordReadResult{}, err
	}
	var version int64
	if request.Version != nil {
		version = *request.Version
	}
	record, err := s.Owner.ReadRecord(ctx, stamp, request.RecordID, version)
	if err != nil {
		return RecordReadResult{}, err
	}
	if record.RecordID != request.RecordID || version > 0 && record.Version != version {
		return RecordReadResult{}, ErrCoreBackend
	}
	data, err := s.project(ctx, stamp, record)
	if err != nil {
		return RecordReadResult{}, err
	}
	if err := s.recheck(ctx, stamp, request.Scope); err != nil {
		return RecordReadResult{}, err
	}
	// Recheck provenance after the metadata traversal. A declared approval
	// cannot authorize freshly changed or removed source.
	verify, err := s.Owner.RecordProvenance(ctx, stamp, record)
	if err != nil {
		return RecordReadResult{}, err
	}
	if verify != data.Record.ProvenanceStatus {
		return RecordReadResult{}, ErrRecordsChanged
	}
	if err := s.recheck(ctx, stamp, request.Scope); err != nil {
		return RecordReadResult{}, err
	}
	return RecordReadResult{Data: data, Scope: stamp.Scope}, nil
}

func (s *RecordService) recheck(ctx context.Context, expected RecordSnapshot, scope *model.ScopeRequest) error {
	current, err := s.Owner.ObserveRecords(ctx, scope)
	if err != nil {
		return err
	}
	if !model.ValidDigest(expected.Digest) || expected.Digest != current.Digest {
		return ErrRecordsChanged
	}
	return ctx.Err()
}

func (s *RecordService) project(ctx context.Context, state RecordSnapshot, stored ports.StoredRecord) (model.RecordReadData, error) {
	provenance, err := s.Owner.RecordProvenance(ctx, state, stored)
	if err != nil {
		return model.RecordReadData{}, err
	}
	summary := model.RecordSummary{RecordID: stored.RecordID, Version: stored.Version, RecordKind: stored.Kind,
		DeclaredStatus: stored.DeclaredStatus, EffectiveStatus: stored.DeclaredStatus, ProvenanceStatus: provenance, SourceRefs: []model.SourceRef{}}
	data := model.RecordReadData{Operation: "records_read", Limitations: []model.Gap{}}
	if provenance == "retained_snapshot" {
		summary.SourceRefs = append(summary.SourceRefs, stored.SourceRefs...)
		data.UIMapping = stored.UIMapping
	} else {
		if summary.DeclaredStatus == "approved" {
			summary.EffectiveStatus = "needs_review"
		}
		data.Limitations = append(data.Limitations, model.Gap{Code: "RECORD_PROVENANCE_UNAVAILABLE",
			Reason: "The current authority could not verify the recorded source.", SourceRefs: []model.SourceRef{}})
	}
	if stored.Kind != "ui_mapping" && (stored.Kind != "state_model" || !stored.StateModelSource) {
		data.Limitations = append(data.Limitations, model.Gap{Code: "RECORD_PAYLOAD_UNSUPPORTED",
			Reason: "This record kind exposes metadata only.", SourceRefs: []model.SourceRef{}})
	}
	data.Record = summary
	raw, err := json.Marshal(data)
	if err != nil {
		return model.RecordReadData{}, ErrCoreBackend
	}
	if _, err := model.DecodeRecordReadDataJSON(raw, 256<<10); err != nil {
		return model.RecordReadData{}, ErrCoreBackend
	}
	return data, nil
}
