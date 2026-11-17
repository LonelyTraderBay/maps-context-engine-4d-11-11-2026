package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func decodeResult(t *testing.T, result Result) model.Response {
	t.Helper()
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	response, err := registry.DecodeResponse(contracts.CLI, "CLI config", result.JSON, 131072, 4)
	if err != nil || model.ValidateDeliveredBytes(response.Budget, [][]byte{result.JSON}) != nil {
		t.Fatalf("invalid configuration response: %v", err)
	}
	return response
}

func TestConfigValidateDoesNotOpenStateAndRejectsInvalidRequests(t *testing.T) {
	opened := false
	open := func(context.Context) (ConfigOwner, func() error, error) {
		opened = true
		return nil, nil, errors.New("must not open")
	}
	good := []byte(`{"action":"validate","config":{"schema_version":1,"core":{"max_output_bytes":4096,"max_parse_workers":0}}}`)
	result, err := ExecuteConfig(context.Background(), "validate", good, 8, open)
	if err != nil || result.ExitCode != 0 || opened {
		t.Fatalf("validate opened state or failed: %v", err)
	}
	response := decodeResult(t, result)
	data, ok := response.Data.(model.RuntimeConfigData)
	if !ok || data.Config.Core.MaxParseWorkers != 4 || data.Config.Core.MaxOutputBytes != 4096 || data.RestartRequired || response.Budget.MaxOutputBytes != 4096 {
		t.Fatal("effective config or budget incorrect")
	}
	for _, item := range []struct {
		action string
		raw    string
	}{
		{"show", `{"action":"validate","config":{"schema_version":1}}`},
		{"validate", `{"action":"validate","config":{"schema_version":1,"unknown":"private-input"}}`},
		{"validate", `{"action":"validate","action":"set","config":{"schema_version":1}}`},
		{"validate", `{"action":"validate","config":{"schema_version":1,"core":{"max_output_bytes":null}}}`},
		{"set", `{"action":"set","config":{"schema_version":1}}`},
	} {
		result, err := ExecuteConfig(context.Background(), item.action, []byte(item.raw), 4, open)
		if err != nil || result.ExitCode != 2 || decodeResult(t, result).Error.Code != "INVALID_ARGUMENT" || strings.Contains(string(result.JSON), "private-input") || opened {
			t.Fatalf("invalid request accepted or opened state: %v", err)
		}
	}
	result, err = ExecuteConfig(context.Background(), "validate", []byte(strings.Repeat("x", configRequestLimit+1)), 4, open)
	if err != nil || result.ExitCode != 2 || decodeResult(t, result).Error.Code != "INVALID_ARGUMENT" || opened {
		t.Fatal("oversized input was accepted")
	}
}

type configOwnerFixture struct {
	state string
	err   error
}

func TestConfigBoundsOwnerLifetimeWithoutExtendingCallerDeadline(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		ctx := context.Background()
		parentDeadline := time.Now().Add(time.Second)
		if earlier {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, parentDeadline)
			defer cancel()
		}
		var acquired context.Context
		result, err := ExecuteConfig(ctx, "show", []byte(`{"action":"show"}`), 4, func(operation context.Context) (ConfigOwner, func() error, error) {
			deadline, ok := operation.Deadline()
			if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > administrativeTimeout || earlier && !deadline.Equal(parentDeadline) {
				t.Fatal("owner did not receive the bounded caller deadline")
			}
			acquired = operation
			return configOwnerFixture{err: app.ErrAdminDenied}, func() error {
				if operation.Err() != nil {
					t.Fatal("owner was cancelled before release")
				}
				return nil
			}, nil
		})
		if err != nil || result.ExitCode != 3 || acquired == nil || !errors.Is(acquired.Err(), context.Canceled) {
			t.Fatal("configuration owner lifetime was not finalized")
		}
	}
}

func (f configOwnerFixture) Show(context.Context) (model.RuntimeConfigData, error) {
	return model.RuntimeConfigData{}, f.err
}
func (f configOwnerFixture) Set(context.Context, []byte) (app.RuntimeConfigSetResult, error) {
	return app.RuntimeConfigSetResult{State: f.state}, f.err
}

func TestConfigReportsUnknownOrAppliedWriteWithoutLeakingError(t *testing.T) {
	raw := []byte(`{"action":"set","config":{"schema_version":1},"expected_digest":null}`)
	for _, state := range []string{"applied", "unknown"} {
		owner := configOwnerFixture{state: state, err: errors.New("private transport detail")}
		result, err := ExecuteConfig(context.Background(), "set", raw, 4, func(context.Context) (ConfigOwner, func() error, error) {
			return owner, func() error { return nil }, nil
		})
		if err != nil || result.ExitCode != 3 {
			t.Fatalf("write outcome: %v", err)
		}
		response := decodeResult(t, result)
		if response.Error.Code != "STORE_RECOVERY_REQUIRED" || strings.Contains(string(result.JSON), "private transport detail") || !strings.Contains(response.Error.Message, "before retrying") {
			t.Fatal("ambiguous write was not disclosed safely")
		}
	}
}

func TestConfigWithoutOwnerAndCancellationFailClosed(t *testing.T) {
	result, err := ExecuteConfig(context.Background(), "show", []byte(`{"action":"show"}`), 4, nil)
	if err != nil || result.ExitCode != 3 || decodeResult(t, result).Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatal("missing owner was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = ExecuteConfig(ctx, "validate", []byte(`{"action":"validate","config":{"schema_version":1}}`), 4, nil)
	if err != nil || result.ExitCode != 4 || decodeResult(t, result).Error.Code != "CANCELLED" {
		t.Fatal("cancelled request was accepted")
	}
}
