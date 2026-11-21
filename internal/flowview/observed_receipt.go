package flowview

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// ObserveReceipt is the SDK's local correlation metadata. It contains no
// authority on its own and is kept out of the browser bridge and FlowQuery.
type ObserveReceipt struct {
	SessionID          string `json:"session_id"`
	DocumentEpoch      int64  `json:"document_epoch"`
	HMREpoch           int64  `json:"hmr_epoch"`
	AuthorizationEpoch int64  `json:"authorization_epoch"`
	BuildMapID         string `json:"build_map_id"`
	InteractionID      string `json:"interaction_id"`
	RequestID          string `json:"request_id"`
	TraceParent        string `json:"traceparent"`
	TraceID            string `json:"trace_id"`
	SpanID             string `json:"span_id"`
}

func DecodeObserveReceipt(raw []byte) (ObserveReceipt, error) {
	var r ObserveReceipt
	var fields map[string]json.RawMessage
	if _, err := model.DecodeIdentityJSON(raw, 2048); err != nil {
		return r, ErrObservedCorrelation
	}
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 10 {
		return r, ErrObservedCorrelation
	}
	for _, name := range []string{"session_id", "document_epoch", "hmr_epoch", "authorization_epoch", "build_map_id",
		"interaction_id", "request_id", "traceparent", "trace_id", "span_id"} {
		if value, ok := fields[name]; !ok || string(value) == "null" {
			return r, ErrObservedCorrelation
		}
	}
	if json.Unmarshal(raw, &r) != nil || !r.valid() {
		return ObserveReceipt{}, ErrObservedCorrelation
	}
	return r, nil
}

func (r ObserveReceipt) request(artifactID string) ObservedRequest {
	return ObservedRequest{InteractionID: r.InteractionID, RequestID: r.RequestID, TraceParent: r.TraceParent,
		BuildDigest: r.BuildMapID, RequestSpan: traces.Ref{ArtifactID: artifactID, TraceID: r.TraceID, SpanID: r.SpanID}}
}

func (r ObserveReceipt) valid() bool {
	const maxEpoch = 9007199254740991
	return model.ValidUUID(r.SessionID) && model.ValidUUID(r.InteractionID) && model.ValidUUID(r.RequestID) &&
		model.ValidDigest(r.BuildMapID) && r.DocumentEpoch > 0 && r.DocumentEpoch <= maxEpoch &&
		r.HMREpoch >= 0 && r.HMREpoch <= maxEpoch && r.AuthorizationEpoch > 0 && r.AuthorizationEpoch <= maxEpoch &&
		matchRequestTraceParent(r.request(""))
}

// BindObserveReceipt ties a local export to one imported artifact. The owner
// must verify the admitted receipt and artifact against its session/export
// ledger. The verifier runs at correlation and again before delivery.
func BindObserveReceipt(r ObserveReceipt, artifactID string,
	authorize func(context.Context, ObserveReceipt, string) error) (ObservedRequest, error) {
	if !r.valid() || !model.ValidDigest(artifactID) || authorize == nil {
		return ObservedRequest{}, ErrObservedCorrelation
	}
	request := r.request(artifactID)
	request.VerifyRequest = func(ctx context.Context, candidate ObservedRequest) error {
		if ctx == nil || candidate.RequestSpan != request.RequestSpan || candidate.TraceParent != request.TraceParent ||
			candidate.InteractionID != request.InteractionID || candidate.RequestID != request.RequestID ||
			candidate.BuildDigest != request.BuildDigest {
			return ErrObservedCorrelation
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return authorize(ctx, r, artifactID)
	}
	return request, nil
}
