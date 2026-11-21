package app

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

func (s *CoreService) failure(ctx context.Context, name string, options CoreCallOptions, err error) (CoreCallResult, error) {
	if options.DeliveryMode == "mcp_structured_verified" {
		if check := s.checkHost(ctx, options); check != nil {
			return CoreCallResult{}, check
		}
	}
	code, message, action, retry := coreError(name, err)
	zero := int64(0)
	response := model.Response{SchemaVersion: 1, RequestID: options.RequestID, Status: "error", Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}, Coverage: model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "No source context was returned."}, Readiness: "blocked", Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero}, Budget: model.Budget{MaxOutputBytes: options.MaxOutputBytes, DeliveryMode: options.DeliveryMode}, Warnings: []model.Gap{}, Error: &model.ErrorInfo{Code: code, Message: message, Retryable: retry, SuggestedAction: action, Details: []model.Gap{}}}
	if code == "CONTEXT_INCOMPLETE" && errors.Is(err, contextpack.ErrContinuationUnavailable) {
		reason := "CONTINUATION_UNAVAILABLE"
		response.Pagination = model.Pagination{Truncated: true, LimitReason: &reason}
	}
	return marshalCoreResponse(response, name, s.registry)
}

func coreError(name string, err error) (code, message, action string, retry bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return "CANCELLED", "The operation was cancelled.", "cancel", false
	case errors.Is(err, context.DeadlineExceeded):
		if name == "mce4d_validate_pack" {
			return "FRESHNESS_UNKNOWN", "Workspace freshness could not be established within the deadline.", "retry_bounded", true
		}
		return "TIMEOUT", "The operation exceeded its deadline.", "retry_bounded", true
	case errors.Is(err, ErrCoreAuthentication):
		return "AUTH_REQUIRED", "Authentication is required.", "reauthenticate", false
	case errors.Is(err, ErrValidationAuthority), errors.Is(err, contextpack.ErrAuthority), errors.Is(err, lsp.ErrLSPAuthority), errors.Is(err, policy.ErrDeniedPath):
		return "ACCESS_DENIED", "The requested scope or source is not authorized.", "review_policy", false
	case errors.Is(err, policy.ErrSecretBlocked):
		return "SECRET_BLOCKED", "Sensitive source content was blocked.", "review_policy", false
	case errors.Is(err, ErrValidationScopeChanged):
		return "CURSOR_INVALID", "The pinned scope has changed.", "recreate_pack", false
	case errors.Is(err, ErrCoreIndexMissing):
		return "INDEX_MISSING", "No published index is available for this scope.", "explicit_index", false
	case errors.Is(err, ErrCoreSnapshotExpired):
		return "SNAPSHOT_EXPIRED", "The pinned snapshot is no longer retained.", "recreate_pack", false
	case errors.Is(err, ErrCoreStoreChanged):
		return "STORE_REPLACED", "A required store was replaced.", "recover_store", false
	case errors.Is(err, ErrCoreStoreRecovery):
		return "STORE_RECOVERY_REQUIRED", "A scoped store requires explicit recovery.", "recover_store", false
	case errors.Is(err, ErrCoreAmbiguous):
		return "AMBIGUOUS_SYMBOL", "The selector matches multiple symbols.", "review_candidates", false
	case errors.Is(err, ErrFreshnessChanged), errors.Is(err, ErrValidationPrecondition):
		return "SOURCE_PRECONDITION_FAILED", "The workspace or source precondition changed.", "recreate_pack", false
	case errors.Is(err, ErrFreshnessUnknown):
		return "FRESHNESS_UNKNOWN", "Workspace freshness could not be established.", "retry_bounded", true
	case errors.Is(err, contextpack.ErrCursorExpired), errors.Is(err, contextpack.ErrPackExpired):
		return "CURSOR_EXPIRED", "The continuation has expired.", "recreate_pack", false
	case errors.Is(err, contextpack.ErrCursorInvalid):
		return "CURSOR_INVALID", "The continuation is invalid.", "recreate_pack", false
	case errors.Is(err, contextpack.ErrBudgetTooSmall):
		return "BUDGET_TOO_SMALL", "The output budget cannot carry the required response.", "fix_request", false
	case errors.Is(err, contextpack.ErrItemTooLarge), errors.Is(err, lsp.ErrResultTooLarge):
		return "RESULT_TOO_LARGE", "The result exceeds the supported output limit.", "fix_request", false
	case errors.Is(err, contextpack.ErrContinuationUnavailable), errors.Is(err, contextpack.ErrTarget):
		return "CONTEXT_INCOMPLETE", "The context cannot be continued safely.", "recreate_pack", false
	case errors.Is(err, ErrCoreLimit), errors.Is(err, contextpack.ErrLimit), errors.Is(err, policy.ErrContentLimit), errors.Is(err, lsp.ErrResultQuota), errors.Is(err, lsp.ErrLSPSessionLimit):
		return "RESOURCE_LIMIT", "The operation exceeded a resource limit.", "retry_bounded", true
	case errors.Is(err, ErrCoreResultExpired), errors.Is(err, lsp.ErrResultExpired):
		return "RESULT_EXPIRED", "The stored result has expired.", "recreate_pack", false
	case errors.Is(err, ErrCoreUnsupported), errors.Is(err, lsp.ErrLSPUnsupported), errors.Is(err, lsp.ErrResultUnsupported), errors.Is(err, search.ErrLocateUnsupported):
		return "UNSUPPORTED_CAPABILITY", "The capability is unavailable.", "none", false
	case errors.Is(err, ErrCoreInput), errors.Is(err, ErrValidationInput), errors.Is(err, contextpack.ErrInput), errors.Is(err, lsp.ErrResultInvalid), errors.Is(err, search.ErrLocatePageInvalid):
		return "INVALID_ARGUMENT", "The request is invalid.", "fix_request", false
	default:
		return "INTERNAL_ERROR", "The operation failed.", "none", false
	}
}
