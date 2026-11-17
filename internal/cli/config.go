// Package cli adapts explicitly owned application capabilities to local CLI
// operations. It does not grant access to a workspace or configuration root.
package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

const configRequestLimit = 32768
const defaultOutputLimit int64 = 24576

// Bound the complete administrative operation, including owner acquisition,
// mutation and final verification. An earlier caller deadline still wins.
const administrativeTimeout = 15 * time.Second

var errResponseBudget = errors.New("configuration response exceeded its budget")
var errResponseContract = errors.New("configuration response violates contract")

// ConfigOwner is supplied only after the process owner has authenticated the
// administrator and authorized the state root. The CLI never derives that
// authority from request JSON.
type ConfigOwner interface {
	Show(context.Context) (model.RuntimeConfigData, error)
	Set(context.Context, []byte) (app.RuntimeConfigSetResult, error)
}

// OpenConfig returns a borrowed service and a release function. Validation
// never calls this hook and therefore never opens or creates a state root.
type OpenConfig func(context.Context) (ConfigOwner, func() error, error)

type Result struct {
	JSON     []byte
	ExitCode int
}

// ExecuteConfig accepts the sealed RuntimeConfigRequest. The subcommand action
// must match the body; errors are sanitized and use the same response schema.
// Input allocation/reading must be bounded by the caller before this function.
func ExecuteConfig(ctx context.Context, action string, raw []byte, cpus int, open OpenConfig) (Result, error) {
	if ctx == nil || cpus < 1 {
		return Result{}, errors.New("invalid CLI configuration")
	}
	ctx, cancel := context.WithTimeout(ctx, administrativeTimeout)
	defer cancel()
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
		return marshalResponse(registry, contracts.CLI, "CLI config", response, exit, cpus)
	}
	if err := ctx.Err(); err != nil {
		return contextFailure(fail, err)
	}
	if len(raw) == 0 || len(raw) > configRequestLimit {
		return fail("INVALID_ARGUMENT", "The configuration request is invalid.", "fix_request", false, 2)
	}
	decoded, err := registry.DecodeRequest(contracts.CLI, "CLI config", raw, configRequestLimit, cpus)
	request, ok := decoded.(model.RuntimeConfigRequest)
	if err != nil || !ok || request.Action != action {
		return fail("INVALID_ARGUMENT", "The configuration request is invalid.", "fix_request", false, 2)
	}
	var data model.RuntimeConfigData
	if action == "validate" {
		config, digest, err := app.ValidateRuntimeConfig(ctx, raw, cpus)
		if err != nil {
			if ctx.Err() != nil {
				return contextFailure(fail, ctx.Err())
			}
			return fail("INVALID_ARGUMENT", "The configuration request is invalid.", "fix_request", false, 2)
		}
		// Validation performs no write and has not changed any running process.
		data = model.RuntimeConfigData{Operation: "runtime_config", Config: config, ConfigDigest: digest, RestartRequired: false}
	} else {
		if action != "show" && action != "set" {
			return fail("INVALID_ARGUMENT", "The configuration action is invalid.", "fix_request", false, 2)
		}
		if open == nil {
			return fail("UNSUPPORTED_CAPABILITY", "An authorized configuration owner is unavailable.", "none", false, 3)
		}
		owner, release, err := open(ctx)
		if err != nil || owner == nil || release == nil {
			if err == nil {
				err = errors.New("configuration owner unavailable")
			}
			if release != nil {
				err = errors.Join(err, release())
			}
			return configFailure(fail, err, "not_applied")
		}
		state := "not_applied"
		if action == "show" {
			data, err = owner.Show(ctx)
		} else {
			var result app.RuntimeConfigSetResult
			result, err = owner.Set(ctx, raw)
			data, state = result.Data, result.State
		}
		err = errors.Join(err, release())
		if err != nil {
			return configFailure(fail, err, state)
		}
		if action == "set" && state != "applied" {
			return configFailure(fail, errors.New("configuration outcome unknown"), "unknown")
		}
	}
	if err := ctx.Err(); err != nil {
		if action == "set" {
			return configFailure(fail, err, "applied")
		}
		return contextFailure(fail, err)
	}
	limit := defaultOutputLimit
	if data.Config.Core.MaxOutputBytes >= 4096 && data.Config.Core.MaxOutputBytes < limit {
		limit = data.Config.Core.MaxOutputBytes
	}
	response := baseResponse(id, limit)
	response.Data = data
	result, err := marshalResponse(registry, contracts.CLI, "CLI config", response, 0, cpus)
	if err != nil {
		if action == "set" {
			return configFailure(fail, err, "applied")
		}
		if errors.Is(err, errResponseBudget) {
			return fail("BUDGET_TOO_SMALL", "The output budget cannot carry the configuration response.", "fix_request", false, 2)
		}
		return fail("INTERNAL_ERROR", "The configuration response is unavailable.", "none", false, 5)
	}
	return result, nil
}

type configFail func(string, string, string, bool, int) (Result, error)

func contextFailure(fail configFail, err error) (Result, error) {
	if errors.Is(err, context.DeadlineExceeded) {
		return fail("TIMEOUT", "The operation exceeded its deadline.", "retry_bounded", true, 4)
	}
	return fail("CANCELLED", "The operation was cancelled.", "cancel", false, 4)
}

func configFailure(fail configFail, err error, state string) (Result, error) {
	if state == "applied" {
		return fail("STORE_RECOVERY_REQUIRED", "The configuration was applied, but final verification failed; inspect it before retrying.", "recover_store", false, 3)
	}
	if state != "not_applied" {
		return fail("STORE_RECOVERY_REQUIRED", "The configuration may have been applied; inspect it before retrying.", "recover_store", false, 3)
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return contextFailure(fail, err)
	case errors.Is(err, model.ErrConfigConflict):
		return fail("CONFIG_CONFLICT", "The configuration changed; inspect it before retrying.", "recover_store", false, 3)
	case errors.Is(err, fsruntime.ErrConfigPending):
		return fail("STORE_RECOVERY_REQUIRED", "Configuration recovery is required.", "recover_store", false, 3)
	case errors.Is(err, os.ErrPermission), errors.Is(err, app.ErrAdminDenied):
		return fail("ACCESS_DENIED", "The configuration is not authorized.", "review_policy", false, 3)
	case errors.Is(err, fsruntime.ErrConfigUnsupported):
		return fail("UNSUPPORTED_CAPABILITY", "The configuration operation is unavailable.", "none", false, 3)
	default:
		return fail("INTERNAL_ERROR", "The configuration operation failed.", "none", false, 5)
	}
}

func baseResponse(id string, limit int64) model.Response {
	zero := int64(0)
	return model.Response{SchemaVersion: 1, RequestID: id, Status: "ok", Readiness: "needs_context",
		Freshness:  model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"},
		Coverage:   model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "No source context was requested."},
		Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero},
		Budget:     model.Budget{MaxOutputBytes: limit, DeliveryMode: "cli"}, Warnings: []model.Gap{}}
}

func marshalResponse(registry *contracts.Registry, transport contracts.Transport, name string, response model.Response, exit, cpus int) (Result, error) {
	settled, blocks, err := model.SettleSerializedBudget(response.Budget, func(size int64) ([][]byte, error) {
		response.Budget.SerializedBytes = size
		raw, err := json.Marshal(response)
		return [][]byte{raw}, err
	})
	if err != nil || model.ValidateDeliveredBytes(settled, blocks) != nil {
		return Result{}, errResponseBudget
	}
	if _, err := registry.DecodeResponse(transport, name, blocks[0], int(response.Budget.MaxOutputBytes), cpus); err != nil {
		return Result{}, errResponseContract
	}
	return Result{JSON: blocks[0], ExitCode: exit}, nil
}

func requestID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}
