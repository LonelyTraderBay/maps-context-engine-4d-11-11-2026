package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type configFixture struct{}

func (configFixture) Read(context.Context, int) ([]byte, bool, error) {
	return []byte(`{"schema_version":1,"core":{"max_output_bytes":4096}}`), true, nil
}
func (configFixture) Set(context.Context, []byte, int) (ports.RuntimeConfigWriteResult, error) {
	return ports.RuntimeConfigWriteResult{}, errors.New("fixture is read-only")
}

type authorityFixture struct{}

func (authorityFixture) Authenticate(context.Context) (app.CorePrincipal, error) {
	return app.CorePrincipal{ID: "fixture-actor", Digest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64)}, nil
}
func (authorityFixture) AuthorizeRequest(context.Context, app.CorePrincipal, string, model.Request) error {
	return nil
}
func (authorityFixture) AuthorizeResponse(context.Context, app.CorePrincipal, string, model.Request, model.Response) error {
	return nil
}

type backendFixture struct {
	calls   atomic.Int32
	started chan struct{}
	stopped chan struct{}
	wait    bool
}

func (b *backendFixture) WorkspaceStatus(ctx context.Context, _ app.CorePrincipal, _ model.StatusRequest, o app.CoreCallOptions) ([][]byte, error) {
	b.calls.Add(1)
	if b.wait {
		close(b.started)
		<-ctx.Done()
		close(b.stopped)
		return nil, ctx.Err()
	}
	zero := int64(0)
	caps := make([]model.Capability, 4)
	for i := range caps {
		caps[i] = model.Capability{Name: "fixture", ProviderID: "fixture", ProviderVersion: strings.Repeat("v", 128), Status: "unknown"}
	}
	response := model.Response{SchemaVersion: 1, RequestID: o.RequestID, Status: "ok",
		Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"},
		Coverage:  model.Coverage{Status: "unknown", Capabilities: caps, Gaps: []model.Gap{}}, Readiness: "needs_context",
		Data:       model.StatusData{Operation: "status", Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "missing", RegisteredScopes: []model.RegisteredScopeStatus{}},
		Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero},
		Budget:     model.Budget{MaxOutputBytes: o.MaxOutputBytes, DeliveryMode: o.DeliveryMode}, Warnings: []model.Gap{}}
	var best [][]byte
	for n := 1; n <= 980; n++ {
		response.Coverage.ScopeDescription = strings.Repeat("x", n) + " ລາວ😀<&"
		_, blocks, err := model.SettleSerializedBudget(response.Budget, func(size int64) ([][]byte, error) {
			response.Budget.SerializedBytes = size
			raw, err := json.Marshal(response)
			if o.DeliveryMode == "mcp_text" {
				return [][]byte{raw}, err
			}
			return [][]byte{raw, append([]byte(nil), raw...)}, err
		})
		if err != nil {
			break
		}
		best = blocks
	}
	if len(best) == 0 {
		return nil, errors.New("fixture cannot fit")
	}
	return best, nil
}
func (b *backendFixture) Locate(context.Context, app.CorePrincipal, model.LocateRequest, app.CoreCallOptions) ([][]byte, error) {
	b.calls.Add(1)
	return nil, app.ErrCoreUnsupported
}
func (b *backendFixture) ContextPack(context.Context, app.CorePrincipal, model.PackRequest, app.CoreCallOptions) ([][]byte, error) {
	b.calls.Add(1)
	return nil, app.ErrCoreUnsupported
}
func (b *backendFixture) ExpandPack(context.Context, app.CorePrincipal, model.ExpandRequest, app.CoreCallOptions) ([][]byte, error) {
	b.calls.Add(1)
	return nil, app.ErrCoreUnsupported
}
func (b *backendFixture) ValidatePack(context.Context, app.CorePrincipal, model.ValidateRequest, app.CoreCallOptions) ([][]byte, error) {
	b.calls.Add(1)
	return nil, app.ErrCoreUnsupported
}
func (b *backendFixture) RenamePreview(context.Context, app.CorePrincipal, model.Request, app.CoreCallOptions) ([][]byte, error) {
	b.calls.Add(1)
	return nil, app.ErrCoreUnsupported
}
func (*backendFixture) VerifyDelivery(context.Context, app.CorePrincipal, string, model.Request, model.Response) error {
	return nil
}

func fixtureService(t *testing.T, backend *backendFixture) *app.CoreService {
	t.Helper()
	config, err := app.NewRuntimeConfigService(context.Background(), configFixture{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	service, err := app.NewCoreService(config, authorityFixture{}, backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func fixtureServer(t *testing.T, backend *backendFixture) *Server {
	t.Helper()
	server, err := New(fixtureService(t, backend))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestSDKTextModeRequiresExplicitOwnerOption(t *testing.T) {
	service := fixtureService(t, &backendFixture{})
	if _, err := NewWithOptions(service, Options{DeliveryMode: "mcp_structured_verified"}); err == nil {
		t.Fatal("structured-only delivery accepted without host proof")
	}
	server, err := NewWithOptions(service, Options{DeliveryMode: "mcp_text"})
	if err != nil {
		t.Fatal(err)
	}
	client := fixtureClient(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "mce4d_workspace_status", Arguments: map[string]any{}})
	if err != nil || result == nil || result.IsError || len(result.Content) != 1 || result.StructuredContent != nil {
		t.Fatalf("invalid text-only result: %v", err)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatal("missing text content")
	}
	registry, _ := contracts.LoadRegistry()
	response, err := registry.DecodeResponse(contracts.MCP, "mce4d_workspace_status", []byte(text.Text), 4096, 4)
	if err != nil || response.Budget.DeliveryMode != "mcp_text" || model.ValidateDeliveredBytes(response.Budget, [][]byte{[]byte(text.Text)}) != nil {
		t.Fatalf("text-only budget invalid: %v", err)
	}
}

func fixtureClient(t *testing.T, server *Server) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, sr, sw) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.IOTransport{Reader: cr, Writer: cw, MaxLineLength: 1024 * 1024}, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("server did not terminate")
		}
	})
	return session
}

func delivered(t *testing.T, name string, result *sdk.CallToolResult) model.Response {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatal("missing text content")
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatal("not text content")
	}
	structured, err := json.Marshal(result.StructuredContent)
	var textValue, structuredValue any
	if err != nil || json.Unmarshal([]byte(text.Text), &textValue) != nil || json.Unmarshal(structured, &structuredValue) != nil || !reflect.DeepEqual(textValue, structuredValue) {
		t.Fatal("text and structured payload semantics differ")
	}
	registry, _ := contracts.LoadRegistry()
	response, err := registry.DecodeResponse(contracts.MCP, name, structured, 131072, 4)
	if err != nil || model.ValidateDeliveredBytes(response.Budget, [][]byte{[]byte(text.Text), structured}) != nil {
		t.Fatalf("invalid delivered envelope: %v", err)
	}
	return response
}

func TestSDKListsOnlySealedToolsAndMeasuresDualPayload(t *testing.T) {
	backend := &backendFixture{}
	client := fixtureClient(t, fixtureServer(t, backend))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var statusOutput any
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("missing schema for %s", tool.Name)
		}
		if tool.Name == "mce4d_workspace_status" {
			statusOutput = tool.OutputSchema
		}
	}
	sort.Strings(names)
	want := []string{"mce4d_context_pack", "mce4d_expand_pack", "mce4d_locate", "mce4d_rename_preview", "mce4d_validate_pack", "mce4d_workspace_status"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("tools: %v", names)
	}
	result, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "mce4d_workspace_status", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("status call failed: %v", err)
	}
	response := delivered(t, "mce4d_workspace_status", result)
	rawSchema, err := json.Marshal(statusOutput)
	var schema jsonschema.Schema
	if err != nil || json.Unmarshal(rawSchema, &schema) != nil {
		t.Fatal("status output schema invalid")
	}
	resolved, err := schema.Resolve(nil)
	if err != nil || resolved.Validate(result.StructuredContent) != nil {
		t.Fatalf("delivered response violates advertised schema: %v", err)
	}
	if response.Budget.SerializedBytes < 4070 || response.Budget.SerializedBytes > 4096 {
		t.Fatalf("not near cap: %d", response.Budget.SerializedBytes)
	}
}

type failingInput struct{}

func (failingInput) Read([]byte) (int, error) { return 0, errors.New("private transport detail") }
func (failingInput) Close() error             { return nil }

func TestRunSanitizesTransportErrors(t *testing.T) {
	server := fixtureServer(t, &backendFixture{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := server.Run(ctx, failingInput{}, noCloseWriter{Writer: io.Discard})
	if err == nil || strings.Contains(err.Error(), "private transport detail") {
		t.Fatalf("transport error leaked or disappeared: %v", err)
	}
}

func TestSDKRejectsBadArgumentsAndPreservesCanary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	const canary = "do not change this file\n"
	if err := os.WriteFile(path, []byte(canary), 0600); err != nil {
		t.Fatal(err)
	}
	backend := &backendFixture{}
	client := fixtureClient(t, fixtureServer(t, backend))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range []json.RawMessage{[]byte(`{"verify":"none","verify":"workspace"}`), []byte(`{"unknown":"private-argument"}`), []byte(`{"verify":null}`), []byte(`{"unknown":"` + strings.Repeat("z", 33000) + `"}`)} {
		result, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "mce4d_workspace_status", Arguments: args})
		if err != nil || result == nil || !result.IsError {
			t.Fatalf("bad arguments were not a tool error: %v", err)
		}
		response := delivered(t, "mce4d_workspace_status", result)
		if response.Error == nil || response.Error.Code != "INVALID_ARGUMENT" || strings.Contains(fmt.Sprint(result.Content), "private-argument") {
			t.Fatal("invalid input not sanitized")
		}
	}
	if backend.calls.Load() != 0 {
		t.Fatal("bad input reached backend")
	}
	for _, name := range []string{"mce4d_workspace_status", "mce4d_locate", "mce4d_context_pack", "mce4d_expand_pack", "mce4d_validate_pack", "mce4d_rename_preview"} {
		if _, err := client.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{}}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != canary {
		t.Fatal("fixture canary changed")
	}
}

func TestSDKCancelReachesService(t *testing.T) {
	backend := &backendFixture{wait: true, started: make(chan struct{}), stopped: make(chan struct{})}
	client := fixtureClient(t, fixtureServer(t, backend))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "mce4d_workspace_status", Arguments: map[string]any{}})
		done <- err
	}()
	select {
	case <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("service did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled client call succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled client call did not terminate")
	}
	select {
	case <-backend.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("service did not observe cancellation")
	}
}

func TestRawJSONRPCRejectsDuplicateEnvelopeArguments(t *testing.T) {
	backend := &backendFixture{}
	server := fixtureServer(t, backend)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, sr, sw) }()
	defer func() { _ = cw.Close(); _ = cr.Close(); <-done }()
	reader := bufio.NewReader(cr)
	write := func(line string) {
		t.Helper()
		if _, err := io.WriteString(cw, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`)
	if _, err := reader.ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	write(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	write(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mce4d_workspace_status","arguments":{"verify":"workspace"},"arguments":{"verify":"none"}}}`)
	_, _ = reader.ReadBytes('\n')
	if backend.calls.Load() != 0 {
		t.Fatal("duplicate arguments in protocol envelope reached backend")
	}
}

func TestRawJSONRPCEmitsExactDualBytes(t *testing.T) {
	server := fixtureServer(t, &backendFixture{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, sr, sw) }()
	defer func() { _ = cw.Close(); _ = cr.Close(); <-done }()
	reader := bufio.NewReader(cr)
	write := func(line string) {
		t.Helper()
		if _, err := io.WriteString(cw, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`)
	if _, err := reader.ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	write(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	write(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mce4d_workspace_status","arguments":{}}}`)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Structured json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(line, &frame) != nil || len(frame.Result.Content) != 1 || frame.Result.Content[0].Type != "text" || !bytes.Equal([]byte(frame.Result.Content[0].Text), frame.Result.Structured) {
		t.Fatal("raw protocol frame differs from budgeted representations")
	}
	registry, _ := contracts.LoadRegistry()
	response, err := registry.DecodeResponse(contracts.MCP, "mce4d_workspace_status", frame.Result.Structured, 4096, 4)
	if err != nil || model.ValidateDeliveredBytes(response.Budget, [][]byte{[]byte(frame.Result.Content[0].Text), frame.Result.Structured}) != nil {
		t.Fatalf("raw frame accounting failed: %v", err)
	}
}

func TestStdioProcessUsesOnlyProtocolFrames(t *testing.T) {
	if os.Getenv("MCE4D_TEST_STDIO_HELPER") == "1" {
		server := fixtureServer(t, &backendFixture{})
		if err := server.RunStdio(context.Background()); err != nil {
			os.Exit(2)
		}
		os.Exit(0) // The test harness must not print PASS on protocol stdout.
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStdioProcessUsesOnlyProtocolFrames$")
	cmd.Env = append(os.Environ(), "MCE4D_TEST_STDIO_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	started := true
	defer func() {
		if started {
			cancel()
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReader(stdout)
	write := func(line string) {
		t.Helper()
		if _, err := io.WriteString(stdin, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]json.RawMessage {
		t.Helper()
		line, err := reader.ReadBytes('\n')
		var frame map[string]json.RawMessage
		if err != nil || json.Unmarshal(line, &frame) != nil || string(frame["jsonrpc"]) != `"2.0"` {
			t.Fatalf("invalid protocol frame: %v", err)
		}
		return frame
	}
	write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`)
	if frame := read(); string(frame["id"]) != "1" || frame["result"] == nil {
		t.Fatal("initialization failed")
	}
	write(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	write(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mce4d_workspace_status","arguments":{}}}`)
	frame := read()
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
	}
	if string(frame["id"]) != "2" || json.Unmarshal(frame["result"], &result) != nil || len(result.Content) != 1 || !bytes.Equal([]byte(result.Content[0].Text), result.Structured) {
		t.Fatal("stdio dual content differs")
	}
	write(`{"jsonrpc":"2.0","id":3,"method":"fixture/unknown","params":{}}`)
	frame = read()
	var protocolError struct {
		Code int `json:"code"`
	}
	if string(frame["id"]) != "3" || json.Unmarshal(frame["error"], &protocolError) != nil || protocolError.Code != -32601 {
		t.Fatal("SDK did not return a method-not-found protocol error")
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	started = false
	if err != nil || len(rest) != 0 || stderr.Len() != 0 {
		t.Fatalf("stdio process leaked output or failed: %v", err)
	}
}
