package app

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func (s *CoreService) deliverOversized(ctx context.Context, name string, raw []byte, requestLimit int, principal CorePrincipal, request model.Request, options CoreCallOptions, proof *oversizedEvidenceError) (CoreCallResult, error) {
	fail := func(err error) (CoreCallResult, error) {
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	if proof == nil || proof.recheck == nil || proof.maxOutput < 4096 || proof.maxOutput > 131072 || proof.principal != principal.Digest || proof.binding != principal.BindingDigest {
		return fail(ErrValidationAuthority)
	}
	switch value := request.(type) {
	case model.PackRequest:
		digest, err := model.RequestDigest("context_pack", principal.ID, "PackRequest", value, 65536, 4)
		if name != "mce4d_context_pack" || !proof.first || err != nil || digest != proof.request {
			return fail(ErrValidationAuthority)
		}
	case model.ExpandRequest:
		if name != "mce4d_expand_pack" || proof.first || value.PackToken != proof.token || value.NextCursor != proof.cursor {
			return fail(ErrValidationAuthority)
		}
	default:
		return fail(ErrValidationAuthority)
	}
	if err := proof.recheck(ctx, proof); err != nil {
		return fail(err)
	}
	if proof.maxOutput < options.MaxOutputBytes {
		options.MaxOutputBytes = proof.maxOutput
	}
	if options.MaxOutputBytes < 4096 {
		return fail(contextpack.ErrBudgetTooSmall)
	}
	limitReason := "result_too_large"
	scope := proof.scope
	response := model.Response{SchemaVersion: 1, RequestID: options.RequestID, Status: "error", Scope: &scope,
		Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"},
		Coverage:  model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "The complete evidence item could not be delivered."},
		Readiness: "blocked", Pagination: model.Pagination{Truncated: true, EnumerationComplete: false, LimitReason: &limitReason},
		Budget: model.Budget{MaxOutputBytes: options.MaxOutputBytes, DeliveryMode: options.DeliveryMode}, Warnings: []model.Gap{},
		Error: &model.ErrorInfo{Code: "RESULT_TOO_LARGE", Message: "The complete evidence item exceeds the effective output limit.", SuggestedAction: "fix_request", Details: []model.Gap{{Code: "EVIDENCE_ITEM_TOO_LARGE", Reason: "Evidence item " + proof.itemID + " cannot fit in the output budget.", SourceRefs: append([]model.SourceRef{}, proof.sources...)}}}}
	result, err := marshalCoreResponse(response, name, s.registry)
	if err != nil {
		return fail(contextpack.ErrBudgetTooSmall)
	}
	for i := 0; i < 2; i++ {
		responseCopy, responseErr := s.registry.DecodeResponse(contracts.MCP, name, result.Blocks[0], int(options.MaxOutputBytes), 4)
		requestCopy, requestErr := s.registry.DecodeRequest(contracts.MCP, name, raw, requestLimit, 4)
		if responseErr != nil || requestErr != nil || s.authority.AuthorizeResponse(ctx, principal, name, requestCopy, responseCopy) != nil {
			return fail(ErrValidationAuthority)
		}
		if err := s.authorizeRequest(ctx, principal, name, raw, requestLimit); err != nil {
			return fail(err)
		}
	}
	if err := proof.recheck(ctx, proof); err != nil {
		return fail(err)
	}
	if err := s.checkHost(ctx, options); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return result, nil
}
