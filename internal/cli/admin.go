package cli

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrAdminDenied      = app.ErrAdminDenied
	ErrAdminUnavailable = app.ErrAdminUnavailable
	ErrAdminConflict    = app.ErrAdminConflict
	ErrAdminUnsupported = app.ErrAdminUnsupported
)

// AdminOwner is an authenticated, scope-bound administrative use case. The
// four sentinel errors may be returned only before a mutation was committed;
// any other error is conservatively treated as an unknown commit outcome.
type AdminOwner interface {
	SetProfile(context.Context, []byte) (model.ProfileAdminData, error)
	SetPolicy(context.Context, []byte) (model.PolicyAdminData, error)
}

// OpenAdmin must authorize this exact operation and worktree before returning
// an owner. A release error after a write is reported as requiring recovery.
type OpenAdmin func(context.Context, string, string, string) (AdminOwner, func() error, error)

func ExecuteAdmin(ctx context.Context, kind, flagWorktree string, raw []byte, cpus int, configPath string, open OpenAdmin) (Result, error) {
	if ctx == nil || cpus < 1 || kind != "profile" && kind != "policy" {
		return Result{}, errors.New("invalid CLI administration request")
	}
	ctx, cancel := context.WithTimeout(ctx, administrativeTimeout)
	defer cancel()
	name := "CLI " + kind + " set"
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
		return marshalResponse(registry, contracts.CLI, name, response, exit, cpus)
	}
	if ctx.Err() != nil {
		return contextFailure(fail, ctx.Err())
	}
	if len(raw) == 0 || len(raw) > 32768 {
		return fail("INVALID_ARGUMENT", "The administrative request is invalid.", "fix_request", false, 2)
	}
	request, err := registry.DecodeRequest(contracts.CLI, name, raw, 32768, cpus)
	if err != nil {
		return fail("INVALID_ARGUMENT", "The administrative request is invalid.", "fix_request", false, 2)
	}
	var worktree, digest, profileName string
	var expected int64
	switch value := request.(type) {
	case model.ProfileAdminRequest:
		if kind != "profile" || flagWorktree == "" || value.WorktreeID != flagWorktree {
			return fail("INVALID_ARGUMENT", "The worktree flag and request must match.", "fix_request", false, 2)
		}
		worktree, expected, profileName = value.WorktreeID, value.ExpectedRevision, value.ProfileName
		digest, err = model.ProfileDigest(value.Profile, 32768)
	case model.PolicyAdminRequest:
		if kind != "policy" || flagWorktree != "" {
			return fail("INVALID_ARGUMENT", "The administrative request is invalid.", "fix_request", false, 2)
		}
		worktree, expected = value.WorktreeID, value.ExpectedEpoch
		digest, err = model.PolicyDigest(value.Policy, 32768)
	default:
		return fail("INVALID_ARGUMENT", "The administrative request is invalid.", "fix_request", false, 2)
	}
	if err != nil {
		return fail("INVALID_ARGUMENT", "The administrative request is invalid.", "fix_request", false, 2)
	}
	if open == nil {
		return fail("UNSUPPORTED_CAPABILITY", "An authorized administrative owner is unavailable.", "none", false, 3)
	}
	owner, release, err := open(ctx, configPath, kind, worktree)
	if err != nil || owner == nil || release == nil {
		if release != nil {
			_ = release()
		}
		if ctx.Err() != nil {
			return contextFailure(fail, ctx.Err())
		}
		if errors.Is(err, ErrAdminDenied) {
			return fail("ACCESS_DENIED", "The administrative scope is not authorized.", "review_policy", false, 3)
		}
		if errors.Is(err, ErrAdminUnavailable) {
			return fail("UNKNOWN_WORKTREE", "The requested worktree is unavailable.", "register_scope", false, 3)
		}
		if errors.Is(err, ErrAdminUnsupported) {
			return fail("UNSUPPORTED_CAPABILITY", "The administrative capability is unavailable.", "none", false, 3)
		}
		return fail("INTERNAL_ERROR", "The administrative owner is unavailable.", "none", false, 5)
	}
	if ctx.Err() != nil {
		_ = release()
		return contextFailure(fail, ctx.Err())
	}
	var data model.ResponseData
	if kind == "profile" {
		value, setErr := owner.SetProfile(ctx, append([]byte(nil), raw...))
		data, err = value, setErr
		if err == nil && (value.WorktreeID != worktree || value.ProfileName != profileName || value.ProfileID != digest || value.Revision != expected+1) {
			err = errors.New("profile outcome requires reconciliation")
		}
	} else {
		value, setErr := owner.SetPolicy(ctx, append([]byte(nil), raw...))
		data, err = value, setErr
		if err == nil && (value.WorktreeID != worktree || value.PolicyDigest != digest || value.PolicyEpoch != expected+1) {
			err = errors.New("policy outcome requires reconciliation")
		}
	}
	closeErr := release()
	if closeErr != nil || err == nil && ctx.Err() != nil {
		return fail("STORE_RECOVERY_REQUIRED", "The change may have been applied; inspect the state before retrying.", "recover_store", false, 3)
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrAdminConflict):
			code := "PROFILE_MISMATCH"
			if kind == "policy" {
				code = "POLICY_CHANGED"
			}
			return fail(code, "The administrative revision changed; inspect it before retrying.", "review_policy", false, 3)
		case errors.Is(err, ErrAdminDenied):
			return fail("ACCESS_DENIED", "The administrative scope is not authorized.", "review_policy", false, 3)
		case errors.Is(err, ErrAdminUnavailable):
			return fail("UNKNOWN_WORKTREE", "The requested worktree is unavailable.", "register_scope", false, 3)
		case errors.Is(err, ErrAdminUnsupported):
			return fail("UNSUPPORTED_CAPABILITY", "The administrative capability is unavailable.", "none", false, 3)
		default:
			return fail("STORE_RECOVERY_REQUIRED", "The change may have been applied; inspect the state before retrying.", "recover_store", false, 3)
		}
	}
	response := baseResponse(id, defaultOutputLimit)
	response.Data = data
	result, err := marshalResponse(registry, contracts.CLI, name, response, 0, cpus)
	if err != nil {
		return fail("STORE_RECOVERY_REQUIRED", "The change was applied but its response is unavailable; inspect before retrying.", "recover_store", false, 3)
	}
	return result, nil
}
