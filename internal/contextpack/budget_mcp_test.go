package contextpack

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This in-memory SDK fixture measures the tool-visible bodies. It is not a
// production MCP adapter or evidence about a real host's behavior.
func TestBudgetedDualResultThroughPinnedSDK(t *testing.T) {
	ctx, in := budgetFixture(t)
	in.MaxOutputBytes = 8192
	in.MaxItems = 1
	page, err := BudgetTaskSlice(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "budget-fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "budget_fixture", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(page.Blocks[0])}}, StructuredContent: json.RawMessage(page.Blocks[1])}, nil
	})
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	session, err := server.Connect(ctx, &mcp.IOTransport{Reader: sr, Writer: sw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "budget-client", Version: "1"}, nil)
	connection, err := client.Connect(ctx, &mcp.IOTransport{Reader: cr, Writer: cw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	result, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: "budget_fixture", Arguments: map[string]any{}})
	if err != nil || result == nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("SDK fixture: %v", err)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("missing text")
	}
	structured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateDeliveredBytes(page.Response.Budget, [][]byte{[]byte(text.Text), structured}); err != nil {
		t.Fatalf("actual SDK tool-visible bytes differ: %v", err)
	}
	if _, err := model.DecodeResponseJSON([]byte(text.Text), int(in.MaxOutputBytes), 4, "context_pack", false); err != nil {
		t.Fatal(err)
	}
}
