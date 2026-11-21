package cli

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type RunGroupAdmin func(context.Context, string, model.GroupAdminRequest) (model.GroupAdminData, *string, error)
type RunGroupScope func(context.Context, string, model.StatusRequest) (model.ScopeVector, error)

func groupStatusResponse(id string, scope model.ScopeVector) model.Response {
	response := baseResponse(id, defaultOutputLimit)
	response.Scope = &scope
	indexState := "published"
	response.Readiness = "needs_context"
	if len(scope.Members) == 0 {
		indexState = "unavailable"
		response.Readiness = "blocked"
	}
	if len(scope.UnavailableMembers) != 0 {
		response.Status = "partial"
		response.Coverage.Status = "partial"
	}
	response.Coverage.ScopeDescription = "Independently observed published group members and unavailable members."
	response.Data = model.StatusData{Operation: "status", Scopes: scope.Members, Capabilities: []model.Capability{},
		ProjectStage: "unknown", IndexState: indexState, RegisteredScopes: []model.RegisteredScopeStatus{}}
	response.Pagination.ReturnedCount = int64(len(scope.Members))
	return response
}

func executeGroupAdmin(ctx context.Context, action, config string, cpus int, raw []byte, run RunGroupAdmin) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, administrativeTimeout)
	defer cancel()
	const operation = "CLI group"
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
	request, valid := decoded.(model.GroupAdminRequest)
	if err != nil || !valid || request.Action != action || action != "list" && (request.Limit != nil || request.Cursor != nil) {
		return fail("INVALID_ARGUMENT", "The group request is invalid.", "fix_request", false, 2)
	}
	if run == nil {
		return fail("UNSUPPORTED_CAPABILITY", "Group administration is unavailable.", "none", false, 3)
	}
	data, next, err := run(ctx, config, request)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrAdminDenied), errors.Is(err, ErrBindingDenied), errors.Is(err, catalog.ErrGroupUnavailable):
			return fail("ACCESS_DENIED", "The group administrative scope is unavailable.", "review_policy", false, 3)
		case errors.Is(err, catalog.ErrRevisionConflict):
			return fail("RECORD_CONFLICT", "The group revision or name changed.", "retry_bounded", false, 3)
		case errors.Is(err, catalog.ErrGroupInput):
			return fail("INVALID_ARGUMENT", "The group request is invalid.", "fix_request", false, 2)
		case errors.Is(err, contextpack.ErrCursorInvalid):
			return fail("CURSOR_INVALID", "The group continuation is invalid.", "fix_request", false, 3)
		case errors.Is(err, contextpack.ErrCursorExpired):
			return fail("CURSOR_EXPIRED", "The group list changed or its continuation expired.", "retry_bounded", false, 3)
		case errors.Is(err, app.ErrCoreStoreRecovery), action == "put", action == "remove":
			return fail("STORE_RECOVERY_REQUIRED", "Inspect the group revision before retrying.", "recover_store", false, 3)
		case ctx.Err() != nil:
			return contextFailure(fail, ctx.Err())
		default:
			return fail("INTERNAL_ERROR", "The group read is unavailable.", "none", false, 5)
		}
	}
	response := baseResponse(id, defaultOutputLimit)
	response.Data = data
	response.Pagination.ReturnedCount = int64(len(data.Groups))
	if next != nil {
		reason := "PAGE_LIMIT"
		response.Status = "partial"
		response.Pagination.Truncated, response.Pagination.EnumerationComplete = true, false
		response.Pagination.NextCursor, response.Pagination.LimitReason = next, &reason
		response.Pagination.OptionalPending = nil
	}
	result, err := marshalResponse(registry, contracts.CLI, operation, response, responseExit(response), cpus)
	if err != nil && (action == "put" || action == "remove") {
		return fail("STORE_RECOVERY_REQUIRED", "The change response is unavailable. Inspect the group revision before retrying.", "recover_store", false, 3)
	}
	if errors.Is(err, errResponseBudget) {
		return fail("RESOURCE_LIMIT", "The group page exceeds its byte limit.", "fix_request", true, 4)
	}
	return result, err
}
