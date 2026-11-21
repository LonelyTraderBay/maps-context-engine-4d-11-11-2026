package cli

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type RunPreviewPermission func(context.Context, string, model.PreviewPermissionAdminRequest) (model.PreviewPermissionAdminData, *string, error)

func executePreviewPermission(ctx context.Context, action, config string, cpus int, raw []byte, run RunPreviewPermission) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, administrativeTimeout)
	defer cancel()
	const operation = "CLI preview-permission"
	id, err := requestID()
	if err != nil {
		return Result{}, err
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		return Result{}, err
	}
	fail := func(code, message, suggested string, retry bool, exit int) (Result, error) {
		response := baseResponse(id, defaultOutputLimit)
		response.Status, response.Readiness = "error", "blocked"
		response.Error = &model.ErrorInfo{Code: code, Message: message, SuggestedAction: suggested, Retryable: retry, Details: []model.Gap{}}
		return marshalResponse(registry, contracts.CLI, operation, response, exit, cpus)
	}
	decoded, err := registry.DecodeRequest(contracts.CLI, operation, raw, 32768, cpus)
	request, valid := decoded.(model.PreviewPermissionAdminRequest)
	if err != nil || !valid || action != request.Action {
		return fail("INVALID_ARGUMENT", "The preview permission request is invalid.", "fix_request", false, 2)
	}
	if run == nil {
		return fail("UNSUPPORTED_CAPABILITY", "Preview administration is unavailable.", "none", false, 3)
	}
	data, next, err := run(ctx, config, request)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrAdminDenied), errors.Is(err, ErrBindingDenied):
			return fail("ACCESS_DENIED", "The administrator is not authorized for this permission.", "review_policy", false, 3)
		case errors.Is(err, catalog.ErrRevisionConflict):
			return fail("RECORD_CONFLICT", "The permission head changed.", "retry_bounded", false, 3)
		case errors.Is(err, catalog.ErrPermissionUnavailable):
			return fail("PREVIEW_NOT_APPROVED", "The requested permission is unavailable.", "review_policy", false, 3)
		case errors.Is(err, catalog.ErrPermissionInput), errors.Is(err, app.ErrCoreInput):
			return fail("INVALID_ARGUMENT", "The preview permission request is invalid.", "fix_request", false, 2)
		case errors.Is(err, app.ErrCoreStoreRecovery) || action == "put" || action == "revoke":
			return fail("STORE_RECOVERY_REQUIRED", "Inspect the permission head before retrying.", "recover_store", false, 3)
		case ctx.Err() != nil:
			return contextFailure(fail, ctx.Err())
		default:
			return fail("INTERNAL_ERROR", "The permission read is unavailable.", "none", false, 5)
		}
	}
	response := baseResponse(id, defaultOutputLimit)
	response.Data = data
	response.Pagination.ReturnedCount = int64(len(data.Profiles))
	if next != nil {
		reason := "PAGE_LIMIT"
		response.Status = "partial"
		response.Pagination.Truncated, response.Pagination.EnumerationComplete = true, false
		response.Pagination.NextCursor, response.Pagination.LimitReason = next, &reason
		response.Pagination.OptionalPending = nil
	}
	result, err := marshalResponse(registry, contracts.CLI, operation, response, responseExit(response), cpus)
	if errors.Is(err, errResponseBudget) {
		return fail("RESOURCE_LIMIT", "The permission page exceeds its byte limit.", "fix_request", true, 4)
	}
	return result, err
}
