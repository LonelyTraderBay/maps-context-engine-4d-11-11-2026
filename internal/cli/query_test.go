package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type queryConfigStore struct{}

func (queryConfigStore) Read(context.Context, int) ([]byte, bool, error) { return nil, false, nil }
func (queryConfigStore) Set(context.Context, []byte, int) (ports.RuntimeConfigWriteResult, error) {
	return ports.RuntimeConfigWriteResult{}, errors.New("fixture is read-only")
}

type queryAuthority struct{}

func (queryAuthority) Authenticate(context.Context) (app.CorePrincipal, error) {
	return app.CorePrincipal{ID: "fixture", Digest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64)}, nil
}
func (queryAuthority) AuthorizeRequest(context.Context, app.CorePrincipal, string, model.Request) error {
	return nil
}
func (queryAuthority) AuthorizeResponse(context.Context, app.CorePrincipal, string, model.Request, model.Response) error {
	return nil
}

type queryBackend struct{ calls atomic.Int32 }

func (b *queryBackend) WorkspaceStatus(_ context.Context, _ app.CorePrincipal, _ model.StatusRequest, o app.CoreCallOptions) ([][]byte, error) {
	b.calls.Add(1)
	zero := int64(0)
	response := model.Response{SchemaVersion: 1, RequestID: o.RequestID, Status: "ok", Readiness: "needs_context",
		Freshness:  model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"},
		Coverage:   model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"},
		Data:       model.StatusData{Operation: "status", Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "missing", RegisteredScopes: []model.RegisteredScopeStatus{}},
		Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero}, Budget: model.Budget{MaxOutputBytes: o.MaxOutputBytes, DeliveryMode: o.DeliveryMode}, Warnings: []model.Gap{}}
	_, blocks, err := model.SettleSerializedBudget(response.Budget, func(size int64) ([][]byte, error) {
		response.Budget.SerializedBytes = size
		raw, err := json.Marshal(response)
		if o.DeliveryMode == "mcp_dual" {
			return [][]byte{raw, append([]byte(nil), raw...)}, err
		}
		return [][]byte{raw}, err
	})
	return blocks, err
}
func (*queryBackend) Locate(context.Context, app.CorePrincipal, model.LocateRequest, app.CoreCallOptions) ([][]byte, error) {
	return nil, app.ErrCoreUnsupported
}
func (*queryBackend) ContextPack(context.Context, app.CorePrincipal, model.PackRequest, app.CoreCallOptions) ([][]byte, error) {
	return nil, app.ErrCoreUnsupported
}
func (*queryBackend) ExpandPack(context.Context, app.CorePrincipal, model.ExpandRequest, app.CoreCallOptions) ([][]byte, error) {
	return nil, app.ErrCoreUnsupported
}
func (*queryBackend) ValidatePack(context.Context, app.CorePrincipal, model.ValidateRequest, app.CoreCallOptions) ([][]byte, error) {
	return nil, app.ErrCoreUnsupported
}
func (*queryBackend) RenamePreview(context.Context, app.CorePrincipal, model.Request, app.CoreCallOptions) ([][]byte, error) {
	return nil, app.ErrCoreUnsupported
}
func (*queryBackend) VerifyDelivery(context.Context, app.CorePrincipal, string, model.Request, model.Response) error {
	return nil
}

func queryService(t *testing.T, backend *queryBackend) *app.CoreService {
	t.Helper()
	config, err := app.NewRuntimeConfigService(context.Background(), queryConfigStore{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	service, err := app.NewCoreService(config, queryAuthority{}, backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestQueryRequiresOwnerAndRefusesUnretainedContinuations(t *testing.T) {
	backend := &queryBackend{}
	service := queryService(t, backend)
	opened := 0
	open := func(_ context.Context, config, worktree, profile string) (*app.CoreService, func() error, error) {
		opened++
		if config != "/fixture/runtime.json" || worktree != "" || profile != "" {
			t.Fatal("query did not request the explicitly configured default binding")
		}
		return service, func() error { return nil }, nil
	}
	result, err := ExecuteQuery(context.Background(), "status", []byte(`{}`), 4, "/fixture/runtime.json", open)
	if err != nil || result.ExitCode != 0 || opened != 1 || backend.calls.Load() != 1 {
		t.Fatalf("status: %v", err)
	}
	registry, _ := contracts.LoadRegistry()
	response, err := registry.DecodeResponse(contracts.MCP, "mce4d_workspace_status", result.JSON, 131072, 4)
	if err != nil || response.Budget.DeliveryMode != "cli" || model.ValidateDeliveredBytes(response.Budget, [][]byte{result.JSON}) != nil {
		t.Fatal("invalid query response")
	}
	result, err = ExecuteQuery(context.Background(), "status", []byte(`{"verify":"none","verify":"workspace"}`), 4, "/fixture/runtime.json", open)
	if err != nil || result.ExitCode != 2 || opened != 1 {
		t.Fatal("invalid request opened the owner")
	}
	result, err = ExecuteQuery(context.Background(), "context", []byte(`{"selector":{"kind":"workspace"},"task_kind":"explore"}`), 4, "/fixture/runtime.json", open)
	if err != nil || result.ExitCode != 3 || opened != 1 {
		t.Fatal("unretained pack continuation was exposed")
	}
	result, err = ExecuteQuery(context.Background(), "status", []byte(`{}`), 4, "", func(context.Context, string, string, string) (*app.CoreService, func() error, error) {
		return nil, nil, ErrBindingDenied
	})
	if err != nil || result.ExitCode != 3 {
		t.Fatal("binding denial was not preserved")
	}
}

func TestRetainedQueryRequiresExplicitOwnerAndBackendCapability(t *testing.T) {
	backend := &queryBackend{}
	service := queryService(t, backend)
	opened, released := 0, 0
	retainedOpen := func(context.Context, string, string, string) (*app.CoreService, func() error, error) {
		opened++
		return service, func() error { released++; return nil }, nil
	}
	raw := []byte(`{"selector":{"kind":"workspace"},"task_kind":"explore"}`)
	result, err := executeQuery(context.Background(), "context", raw, 4, "", nil, nil)
	if err != nil || result.ExitCode != 3 || opened != 0 {
		t.Fatalf("missing owner was accepted: %v", err)
	}
	result, err = executeQuery(context.Background(), "context", raw, 4, "", nil, retainedOpen)
	if err != nil || result.ExitCode != 3 || opened != 1 || released != 1 || backend.calls.Load() != 0 {
		t.Fatalf("owner without retention capability was accepted: %v", err)
	}
	result, err = executeQuery(context.Background(), "context", []byte(`{"unknown":"private"}`), 4, "", nil, retainedOpen)
	if err != nil || result.ExitCode != 2 || opened != 1 {
		t.Fatal("invalid request opened the retained owner")
	}
}

func TestMCPCommandUsesExactOwnerBindingAndProtocolStdout(t *testing.T) {
	backend := &queryBackend{}
	service := queryService(t, backend)
	worktree, profile := "01234567-89ab-cdef-0123-456789abcdef", strings.Repeat("a", 64)
	var opened, released atomic.Bool
	deps := Dependencies{CPUs: 4, OpenCore: func(_ context.Context, config, actualWorktree, actualProfile string) (*app.CoreService, func() error, error) {
		if config != "/fixture/runtime.json" || actualWorktree != worktree || actualProfile != profile {
			return nil, nil, ErrBindingDenied
		}
		opened.Store(true)
		return service, func() error { released.Store(true); return nil }, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	defer sw.Close()
	var errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"--runtime-config", "/fixture/runtime.json", "mcp", "--worktree-id", worktree, "--profile-id", profile}, sr, sw, &errOut, deps)
	}()
	client := sdk.NewClient(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.IOTransport{Reader: cr, Writer: cw, MaxLineLength: 1024 * 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "mce4d_workspace_status", Arguments: map[string]any{}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("MCP status: %v", err)
	}
	_ = session.Close()
	select {
	case code := <-done:
		if code != 0 || !opened.Load() || !released.Load() || errOut.Len() != 0 {
			t.Fatalf("MCP session exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("MCP session did not stop")
	}
}
