package cli

import (
	"context"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var ErrBindingDenied = errors.New("bound service access denied")

// OpenCore is implemented by the trusted process owner. Nonempty IDs require
// that exact binding; empty IDs require one explicit binding, never a guessed
// active repository. Returned services must already enforce authentication.
type OpenCore func(context.Context, string, string, string) (*app.CoreService, func() error, error)

// ExecuteQuery exposes status and locate through the ordinary core owner.
// It never assumes that owner's process-local cache survives another call.
func ExecuteQuery(ctx context.Context, action string, raw []byte, cpus int, configPath string, open OpenCore) (Result, error) {
	return executeQuery(ctx, action, raw, cpus, configPath, open, nil, nil)
}

// executeQuery allows retained packs and rename results only through a
// separate owner explicitly supplied by the trusted process.
func executeQuery(ctx context.Context, action string, raw []byte, cpus int, configPath string, open, retainedOpen OpenCore, group RunGroupScope) (Result, error) {
	if ctx == nil || cpus < 1 {
		return Result{}, errors.New("invalid CLI configuration")
	}
	names := map[string]string{"status": "mce4d_workspace_status", "locate": "mce4d_locate", "context": "mce4d_context_pack", "expand": "mce4d_expand_pack", "validate": "mce4d_validate_pack", "rename-preview": "mce4d_rename_preview"}
	name, ok := names[action]
	if !ok {
		return Result{}, errors.New("unknown query action")
	}
	id, err := requestID()
	if err != nil {
		return Result{}, errors.New("request identity unavailable")
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		return Result{}, errors.New("contract unavailable")
	}
	fail := func(code, message, suggested string, retry bool, exit int) (Result, error) {
		response := baseResponse(id, defaultOutputLimit)
		response.Status, response.Readiness = "error", "blocked"
		response.Error = &model.ErrorInfo{Code: code, Message: message, Retryable: retry, SuggestedAction: suggested, Details: []model.Gap{}}
		return marshalResponse(registry, contracts.MCP, name, response, exit, cpus)
	}
	if ctx.Err() != nil {
		return contextFailure(fail, ctx.Err())
	}
	if len(raw) == 0 || len(raw) > 32768 {
		return fail("INVALID_ARGUMENT", "The query request is invalid.", "fix_request", false, 2)
	}
	decoded, err := registry.DecodeRequest(contracts.MCP, name, raw, 32768, cpus)
	if err != nil {
		return fail("INVALID_ARGUMENT", "The query request is invalid.", "fix_request", false, 2)
	}
	if status, ok := decoded.(model.StatusRequest); ok && status.Scope != nil &&
		(status.Scope.GroupID != nil || status.Scope.PinnedVector != nil && status.Scope.PinnedVector.Kind == "federated") {
		if group == nil {
			return fail("UNSUPPORTED_CAPABILITY", "Group status is unavailable in this binding.", "none", false, 3)
		}
		groupCtx, cancel := context.WithTimeout(ctx, administrativeTimeout)
		defer cancel()
		scope, err := group(groupCtx, configPath, status)
		if err != nil {
			info := app.CoreErrorInfo(name, err)
			response := baseResponse(id, defaultOutputLimit)
			response.Error = &info
			response.Status, response.Readiness = "error", "blocked"
			return marshalResponse(registry, contracts.MCP, name, response, responseExit(response), cpus)
		}
		if model.VerifyScopeVectorDigest(scope, 32768) != nil || scope.Kind != "federated" {
			return fail("INTERNAL_ERROR", "The group observation is invalid.", "none", false, 5)
		}
		response := groupStatusResponse(id, scope)
		result, err := marshalResponse(registry, contracts.MCP, name, response, responseExit(response), cpus)
		if errors.Is(err, errResponseBudget) {
			return fail("RESOURCE_LIMIT", "The group observation exceeds the output budget.", "fix_request", false, 4)
		}
		return result, err
	}
	rename := action == "rename-preview"
	if request, ok := decoded.(model.ValidateRequest); ok && request.ResultToken != nil {
		rename = true
	}
	retained := rename || action == "context" || action == "expand" || action == "validate"
	if retained && retainedOpen == nil {
		return fail("UNSUPPORTED_CAPABILITY", "This CLI query is unavailable until its continuation or preview owner is ready.", "none", false, 3)
	}
	if retained {
		open = retainedOpen
	}
	if open == nil {
		return fail("UNSUPPORTED_CAPABILITY", "An authorized bound service is unavailable.", "none", false, 3)
	}
	service, release, err := open(ctx, configPath, "", "")
	if err != nil || service == nil || release == nil {
		if release != nil {
			_ = release()
		}
		if ctx.Err() != nil {
			return contextFailure(fail, ctx.Err())
		}
		if errors.Is(err, app.ErrCoreStoreRecovery) {
			return fail("STORE_RECOVERY_REQUIRED", "A scoped store requires explicit recovery.", "recover_store", false, 3)
		}
		if errors.Is(err, runtime.ErrStoreMaintenance) {
			return fail("STORE_MAINTENANCE", "The scoped store is busy with maintenance.", "retry_bounded", true, 4)
		}
		if errors.Is(err, ErrBindingDenied) || errors.Is(err, os.ErrPermission) || errors.Is(err, app.ErrCoreAuthentication) {
			return fail("ACCESS_DENIED", "The requested binding is not authorized.", "review_policy", false, 3)
		}
		if errors.Is(err, app.ErrCoreLimit) {
			return fail("RESOURCE_LIMIT", "The bound service exceeds its configured capacity.", "retry_bounded", true, 4)
		}
		return fail("INTERNAL_ERROR", "The bound service is unavailable.", "none", false, 5)
	}
	if retained && (rename && !service.SupportsCLIRenameRetention() || !rename && !service.SupportsCLIPackRetention()) {
		if release() != nil {
			return fail("INTERNAL_ERROR", "The bound service is unavailable.", "none", false, 5)
		}
		return fail("UNSUPPORTED_CAPABILITY", "A retained-result owner is unavailable.", "none", false, 3)
	}
	result, callErr := service.Call(ctx, name, append([]byte(nil), raw...), app.CoreCallOptions{RequestID: id, DeliveryMode: "cli"})
	closeErr := release()
	if callErr != nil || closeErr != nil || len(result.Blocks) != 1 {
		if ctx.Err() != nil {
			return contextFailure(fail, ctx.Err())
		}
		return fail("INTERNAL_ERROR", "The query response is unavailable.", "none", false, 5)
	}
	if ctx.Err() != nil {
		return contextFailure(fail, ctx.Err())
	}
	response, err := registry.DecodeResponse(contracts.MCP, name, result.Blocks[0], 131072, cpus)
	if err != nil || response.RequestID != id || response.Budget.DeliveryMode != "cli" || model.ValidateDeliveredBytes(response.Budget, result.Blocks) != nil || result.IsError != (response.Status == "error" || response.Status == "blocked") {
		return fail("INTERNAL_ERROR", "The query response is unavailable.", "none", false, 5)
	}
	return Result{JSON: append([]byte(nil), result.Blocks[0]...), ExitCode: responseExit(response)}, nil
}

func responseExit(response model.Response) int {
	if response.Status == "partial" {
		if response.Data != nil {
			return 6
		}
		return 3
	}
	if response.Status == "ok" {
		return 0
	}
	if response.Error == nil {
		return 5
	}
	switch response.Error.Code {
	case "INVALID_ARGUMENT", "UNEXPECTED_FIELD", "DUPLICATE_FIELD":
		return 2
	case "TIMEOUT", "CANCELLED", "WORKTREE_BUSY", "RESOURCE_LIMIT", "STORE_MAINTENANCE":
		return 4
	case "INTERNAL_ERROR", "INDEX_CORRUPT", "DISK_FULL":
		return 5
	default:
		return 3
	}
}
