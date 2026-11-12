package contextpack

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// BudgetErrorEnvelope returns a bounded, source-free error or a verified safe
// descriptor for an oversized selected item. The transport owner still maps
// this result to the protocol's error indicator.
func BudgetErrorEnvelope(ctx context.Context, in BudgetInput, cause error) (model.Response, [][]byte, error) {
	if ctx == nil {
		return model.Response{}, nil, ErrInput
	}
	var err error
	in, err = normalizeBudget(in)
	if err != nil {
		return model.Response{}, nil, err
	}
	if err := verifyBudgetHost(ctx, in); err != nil {
		return model.Response{}, nil, err
	}
	info := model.ErrorInfo{Retryable: true, SuggestedAction: "retry_bounded", Details: []model.Gap{}}
	var sourceScope *model.ScopeVector
	switch {
	case errors.Is(cause, ErrBudgetTooSmall):
		info.Code = "BUDGET_TOO_SMALL"
		info.Message = "The available budget cannot hold the required complete response or item."
	case errors.Is(cause, ErrContinuationUnavailable):
		info.Code = "CONTEXT_INCOMPLETE"
		info.Message = "A verified advancing continuation is not available."
		info.SuggestedAction = "recreate_pack"
	case errors.Is(cause, ErrItemTooLarge):
		var item *OversizedItemError
		if !errors.As(cause, &item) || item == nil || len(item.Sources) > 16 {
			return model.Response{}, nil, ErrInput
		}
		if err := in.Slice.authorize(ctx); err != nil {
			return model.Response{}, nil, err
		}
		found := false
		for _, candidate := range in.Slice.report.Items {
			if candidate.ID != item.ItemID || len(candidate.SourceRefs) != len(item.Sources) {
				continue
			}
			match := true
			for i := range candidate.SourceRefs {
				if candidate.SourceRefs[i] != item.Sources[i] {
					match = false
					break
				}
			}
			if match {
				found = true
				break
			}
		}
		if !found {
			return model.Response{}, nil, ErrInput
		}
		scope, _, err := authorityCopy(in.Slice.report.Scope, in.Slice.selection.member)
		if err != nil {
			return model.Response{}, nil, err
		}
		raw, err := json.Marshal(scope)
		if err != nil {
			return model.Response{}, nil, ErrInput
		}
		if err = in.Slice.selection.scanner.CheckContent(raw); err != nil {
			return model.Response{}, nil, err
		}
		sourceScope = &scope
		info.Code = "RESULT_TOO_LARGE"
		info.Message = "The complete evidence item exceeds the effective output limit."
		info.Details = []model.Gap{{Code: "EVIDENCE_ITEM_TOO_LARGE", Reason: "Review the selected source range or use a supported fragment continuation.", SourceRefs: append([]model.SourceRef{}, item.Sources...)}}
	default:
		return model.Response{}, nil, ErrInput
	}
	zero := int64(0)
	response := model.Response{SchemaVersion: 1, RequestID: in.RequestID, Status: "error", Scope: sourceScope, Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}, Coverage: model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "No source context was returned."}, Readiness: "blocked", Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero}, Budget: model.Budget{MaxOutputBytes: in.MaxOutputBytes, DeliveryMode: in.DeliveryMode}, Warnings: []model.Gap{}, Error: &info}
	blocks, err := settleResponse(&response)
	if err != nil {
		if errors.Is(err, errOverBudget) {
			return model.Response{}, nil, ErrBudgetTooSmall
		}
		return model.Response{}, nil, err
	}
	if errors.Is(cause, ErrItemTooLarge) {
		if err := in.Slice.authorize(ctx); err != nil {
			return model.Response{}, nil, err
		}
	}
	if err := verifyBudgetHost(ctx, in); err != nil {
		return model.Response{}, nil, err
	}
	return response, blocks, nil
}
