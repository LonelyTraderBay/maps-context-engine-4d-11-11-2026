package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPDualFixtureMeasuresDeliveredContentNearCap(t *testing.T) {
	type input struct {
		Dummy string `json:"dummy"`
	}
	type output struct {
		Body            string `json:"body"`
		SerializedBytes int64  `json:"serialized_bytes"`
	}
	var selected output
	var settled model.Budget
	var blocks [][]byte
	for n := 1900; n <= 2100; n++ {
		body := strings.Repeat("x", n) + "😀"
		b, delivered, err := model.SettleSerializedBudget(model.Budget{MaxOutputBytes: 4096, DeliveryMode: "mcp_dual"}, func(size int64) ([][]byte, error) {
			data, e := json.Marshal(output{body, size})
			return [][]byte{data, data}, e
		})
		if err != nil {
			break
		}
		selected = output{body, b.SerializedBytes}
		settled = b
		blocks = delivered
	}
	if settled.SerializedBytes < 4080 || settled.SerializedBytes > 4096 {
		t.Fatalf("not near limit: %d", settled.SerializedBytes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "near_cap"}, func(_ context.Context, _ *mcp.CallToolRequest, _ input) (*mcp.CallToolResult, output, error) {
		return nil, selected, nil
	})
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	session, err := server.Connect(ctx, &mcp.IOTransport{Reader: sr, Writer: sw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client", Version: "1"}, nil)
	connection, err := client.Connect(ctx, &mcp.IOTransport{Reader: cr, Writer: cw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	result, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: "near_cap", Arguments: map[string]any{"dummy": ""}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("call: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content count %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("missing text representation")
	}
	structured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(text.Text), blocks[0]) || !bytes.Equal(structured, blocks[1]) {
		t.Fatalf("SDK content differs from calculated representations")
	}
	if !json.Valid([]byte(text.Text)) || model.ValidateDeliveredBytes(settled, [][]byte{[]byte(text.Text), structured}) != nil {
		t.Fatal("delivered payload budget or JSON invalid")
	}
	_, _, err = model.SettleSerializedBudget(model.Budget{MaxOutputBytes: 4096, DeliveryMode: "mcp_dual"}, func(size int64) ([][]byte, error) {
		data, e := json.Marshal(output{selected.Body + "x", size})
		return [][]byte{data, data}, e
	})
	if err == nil {
		t.Fatal("over-cap dual output accepted")
	}
}
