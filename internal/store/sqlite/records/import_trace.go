package records

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const unboundTrace = `{"status":"unbound","reason":"revision_not_verified"}`

type TraceImportRequest struct {
	Root         *paths.Root
	RelativePath string
	Approval     importtrace.Approval
	Writer       ports.IndexWriterLease
	JobID        string
	Authorize    StateAuthorizer
	AllowUpgrade bool
}

type TraceImportResult struct {
	ArtifactID  string
	RecordEpoch int64
	SpanCount   int
	Replayed    bool
}

// ImportTrace stores validated metadata and its spans in a single commit.
// AllowUpgrade requires an exclusive lifecycle lease, even for a replay.
func (s *Store) ImportTrace(ctx context.Context, req TraceImportRequest) (TraceImportResult, error) {
	result, err := s.importEvidence(ctx, evidenceWrite{
		writer: req.Writer, job: req.JobID, authorize: req.Authorize, allowUpgrade: req.AllowUpgrade,
		recheck: func(ctx context.Context, id string) error {
			if err := req.Approval.Recheck(ctx, req.Root, req.RelativePath, id); err != nil {
				return importtrace.ErrApproval
			}
			return nil
		},
	}, func(ctx context.Context) (localEvidence, error) {
		artifact, err := importtrace.Read(ctx, req.Root, req.RelativePath, req.Approval)
		if err != nil {
			return localEvidence{}, err
		}
		m, err := json.Marshal(artifact.Metadata())
		if err != nil {
			return localEvidence{}, err
		}
		spans := artifact.Spans()
		store := localEvidence{id: artifact.InputHash(), kind: "runtime_trace", version: importtrace.SanitizerVersion,
			metadata: string(m), items: make([]localEvidenceItem, 0, len(spans))}
		for _, span := range spans {
			raw, err := json.Marshal(span)
			if err != nil {
				return localEvidence{}, err
			}
			store.items = append(store.items, localEvidenceItem{id: span.TraceID + ":" + span.SpanID,
				kind: "span", body: string(raw), binding: unboundTrace})
		}
		return store, nil
	})
	return TraceImportResult{ArtifactID: result.ArtifactID, RecordEpoch: result.RecordEpoch,
		Replayed: result.Replayed, SpanCount: result.ItemCount}, err
}
