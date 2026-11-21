package cli

import (
	"context"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type OpenRecords func(context.Context, string, string, string) (*app.RecordService, func() error, error)

func bindRecords(open OpenRecords, worktree, profile string) OpenRecords {
	if open == nil {
		return nil
	}
	return func(ctx context.Context, config, askedWorktree, askedProfile string) (*app.RecordService, func() error, error) {
		if askedWorktree != "" && askedWorktree != worktree || askedProfile != "" && askedProfile != profile {
			return nil, nil, ErrBindingDenied
		}
		return open(ctx, config, worktree, profile)
	}
}

func executeRecords(ctx context.Context, action string, raw []byte, config string, cpus int, open OpenRecords) (Result, error) {
	if ctx == nil || cpus < 1 || action != "list" && action != "read" {
		return Result{}, errors.New("invalid records command")
	}
	ctx, cancel := context.WithTimeout(ctx, administrativeTimeout)
	defer cancel()
	id, err := requestID()
	if err != nil {
		return Result{}, err
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		return Result{}, err
	}
	op := "CLI records " + action
	fail := func(code, message, suggested string, retry bool, exit int) (Result, error) {
		response := baseResponse(id, defaultOutputLimit)
		response.Status, response.Readiness = "error", "blocked"
		response.Error = &model.ErrorInfo{Code: code, Message: message, Retryable: retry, SuggestedAction: suggested, Details: []model.Gap{}}
		return marshalResponse(registry, contracts.CLI, op, response, exit, cpus)
	}
	decoded, err := registry.DecodeRequest(contracts.CLI, op, raw, 32768, cpus)
	if err != nil {
		return fail("INVALID_ARGUMENT", "The records request is invalid.", "fix_request", false, 2)
	}
	if open == nil {
		return fail("UNSUPPORTED_CAPABILITY", "A bound records owner is unavailable.", "none", false, 3)
	}
	var scope *model.ScopeRequest
	switch request := decoded.(type) {
	case model.RecordReadRequest:
		scope = request.Scope
	case model.RecordListRequest:
		scope = request.Scope
	default:
		return fail("INVALID_ARGUMENT", "The records request is invalid.", "fix_request", false, 2)
	}
	worktree, profile := "", ""
	if scope != nil {
		if scope.WorktreeID != nil && scope.ProfileID != nil {
			worktree, profile = *scope.WorktreeID, *scope.ProfileID
		} else if scope.PinnedVector != nil && scope.PinnedVector.Kind == "single" && len(scope.PinnedVector.Members) == 1 {
			member := scope.PinnedVector.Members[0]
			worktree, profile = member.WorktreeID, member.ProfileID
		}
	}
	service, release, err := open(ctx, config, worktree, profile)
	if err != nil || service == nil || release == nil {
		if release != nil {
			err = errors.Join(err, release())
		}
		return recordsFailure(fail, ctx, err)
	}
	response := baseResponse(id, defaultOutputLimit)
	response.Coverage.ScopeDescription = "Records in the authorized worktree."
	switch request := decoded.(type) {
	case model.RecordListRequest:
		var result app.RecordListResult
		result, err = service.List(ctx, request)
		response.Data, response.Scope = result.Data, result.Scope
		response.Pagination.ReturnedCount = int64(len(result.Data.Records))
		response.Pagination.NextCursor = result.Cursor
		if result.Cursor != nil {
			reason := "PAGE_LIMIT"
			response.Status = "partial"
			response.Pagination.Truncated, response.Pagination.EnumerationComplete = true, false
			response.Pagination.LimitReason = &reason
			response.Pagination.OptionalPending = nil
		}
	case model.RecordReadRequest:
		var result app.RecordReadResult
		result, err = service.Read(ctx, request)
		response.Data, response.Scope = result.Data, result.Scope
		response.Pagination.ReturnedCount = 1
		if len(result.Data.Limitations) > 0 {
			response.Status, response.Coverage.Status = "partial", "partial"
			response.Coverage.Gaps = result.Data.Limitations
		}
	}
	if err = errors.Join(err, release()); err != nil || ctx.Err() != nil {
		return recordsFailure(fail, ctx, err)
	}
	result, err := marshalResponse(registry, contracts.CLI, op, response, responseExit(response), cpus)
	if errors.Is(err, errResponseBudget) {
		return fail("RESOURCE_LIMIT", "The records response exceeds its byte limit.", "fix_request", true, 4)
	}
	return result, err
}

func recordsFailure(fail configFail, ctx context.Context, err error) (Result, error) {
	if ctx.Err() != nil {
		return contextFailure(fail, ctx.Err())
	}
	switch {
	case errors.Is(err, app.ErrCoreInput):
		return fail("INVALID_ARGUMENT", "The records request is invalid.", "fix_request", false, 2)
	case errors.Is(err, app.ErrCoreUnsupported):
		return fail("UNSUPPORTED_CAPABILITY", "The requested records scope is unavailable.", "none", false, 3)
	case errors.Is(err, app.ErrRecordsMissing), errors.Is(err, app.ErrCoreSnapshotExpired):
		return fail("SNAPSHOT_EXPIRED", "The requested record or selection is unavailable.", "register_scope", false, 3)
	case errors.Is(err, app.ErrRecordsChanged):
		return fail("RECORD_CHANGED", "The current record or scope revision changed.", "retry_bounded", true, 3)
	case errors.Is(err, contextpack.ErrCursorExpired):
		return fail("CURSOR_EXPIRED", "The records continuation expired.", "fix_request", false, 3)
	case errors.Is(err, contextpack.ErrCursorInvalid):
		return fail("CURSOR_INVALID", "The records continuation is invalid.", "fix_request", false, 2)
	case errors.Is(err, app.ErrCoreAuthentication), errors.Is(err, ErrBindingDenied), errors.Is(err, os.ErrPermission):
		return fail("ACCESS_DENIED", "The requested records binding is not authorized.", "review_policy", false, 3)
	case errors.Is(err, app.ErrCoreStoreRecovery):
		return fail("STORE_RECOVERY_REQUIRED", "A scoped store requires explicit recovery.", "recover_store", false, 3)
	default:
		return fail("INTERNAL_ERROR", "The scoped records operation is unavailable.", "none", false, 5)
	}
}
