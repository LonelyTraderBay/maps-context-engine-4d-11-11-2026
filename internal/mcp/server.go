// Package mcp adapts the sealed core service to the official MCP SDK.
// Construction never opens a workspace or creates an authority.
package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxFrameBytes includes the JSON-RPC wrapper; the core independently enforces
// the 32 KiB request-argument limit before parsing or dispatch.
const maxFrameBytes = 64 * 1024

var errUnavailable = errors.New("MCP operation unavailable")

type Server struct{ server *sdk.Server }

// Options are supplied by the trusted server owner, never by tool arguments.
// Text-only mode is an explicit compatibility setting. Structured-only mode is
// unavailable here until an actual host/version proof has been integrated.
type Options struct{ DeliveryMode string }

// New requires a service whose owner has already configured authentication,
// path authority and backend. It does not infer a principal from MCP input.
func New(service *app.CoreService) (*Server, error) {
	return NewWithOptions(service, Options{})
}

// NewWithOptions selects dual delivery by default or explicit text-only mode.
func NewWithOptions(service *app.CoreService, options Options) (*Server, error) {
	if service == nil {
		return nil, errUnavailable
	}
	mode := options.DeliveryMode
	if mode == "" {
		mode = "mcp_dual"
	}
	if mode != "mcp_dual" && mode != "mcp_text" {
		return nil, errUnavailable
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		return nil, errUnavailable
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "maps-context-engine-4d", Version: "0.0.0"}, &sdk.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Capabilities: &sdk.ServerCapabilities{},
	})
	descriptions := map[string]string{
		"mce4d_workspace_status": "Report the registered workspace and published index status.",
		"mce4d_locate":           "Find indexed symbols. Use mode=evidence with an exact name to get numbered excerpts. Set focus=route when you only need a route and its wrapper; omit focus to trace downstream behavior. Optional path disambiguates. Cite source_ref.path, start_line plus line index, and exact content.",
		"mce4d_context_pack":     "Get source-backed context for one symbol. For code navigation, set view to symbol, task_kind to explore, and selector to {kind: ref, ref: <candidate.ref from locate>, service_id: null}. Use the default output budget, or set max_output_bytes to 24576.",
		"mce4d_expand_pack":      "Continue a previously issued context pack.",
		"mce4d_validate_pack":    "Request validation of a pack or rename result before editing.",
		"mce4d_rename_preview":   "Request a rename preview without applying edits.",
	}
	count := 0
	for _, op := range registry.Operations() {
		if op.Transport != contracts.MCP {
			continue
		}
		input, output, err := registry.MCPToolSchemas(op.Name)
		if err != nil {
			return nil, errUnavailable
		}
		name := op.Name
		description, ok := descriptions[name]
		if !ok {
			return nil, errUnavailable
		}
		server.AddTool(&sdk.Tool{Name: name, Description: description, InputSchema: input, OutputSchema: output}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			if req == nil || req.Params == nil {
				return nil, errUnavailable
			}
			raw := req.Params.Arguments
			if len(raw) == 0 {
				raw = json.RawMessage(`{}`)
			}
			id, err := requestID()
			if err != nil {
				return nil, errUnavailable
			}
			result, err := service.Call(ctx, name, raw, app.CoreCallOptions{RequestID: id, DeliveryMode: mode})
			want := 1
			if mode == "mcp_dual" {
				want = 2
			}
			if err != nil || len(result.Blocks) != want {
				return nil, errUnavailable
			}
			if mode == "mcp_text" {
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(result.Blocks[0])}}, IsError: result.IsError}, nil
			}
			if !bytes.Equal(result.Blocks[0], result.Blocks[1]) {
				return nil, errUnavailable
			}
			// SDK/encoding-json must deliver exactly the bytes whose dual budget
			// was settled by the service, including JSON escaping/compaction.
			structured := json.RawMessage(append([]byte(nil), result.Blocks[1]...))
			wire, err := json.Marshal(structured)
			if err != nil || !bytes.Equal(wire, result.Blocks[1]) {
				return nil, errUnavailable
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(result.Blocks[0])}}, StructuredContent: structured, IsError: result.IsError}, nil
		})
		count++
	}
	if count != 6 {
		return nil, errUnavailable
	}
	return &Server{server: server}, nil
}

// Run serves a single newline-delimited MCP session over owner-supplied pipes.
// Closing the input must unblock Read so cancellation can release resources.
func (s *Server) Run(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	if s == nil || s.server == nil || ctx == nil || input == nil || output == nil {
		return errUnavailable
	}
	err := s.server.Run(ctx, &sdk.IOTransport{Reader: newFrameReader(input), Writer: output, MaxLineLength: maxFrameBytes})
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errUnavailable
	}
	return nil
}

// RunStdio uses the SDK's newline-delimited transport over process stdio. The
// caller owns process lifecycle and keeps stdout free of non-protocol output.
func (s *Server) RunStdio(ctx context.Context) error {
	if s == nil || s.server == nil || ctx == nil {
		return errUnavailable
	}
	return s.Run(ctx, os.Stdin, noCloseWriter{Writer: os.Stdout})
}

type noCloseWriter struct{ io.Writer }

func (noCloseWriter) Close() error { return nil }

func requestID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}
