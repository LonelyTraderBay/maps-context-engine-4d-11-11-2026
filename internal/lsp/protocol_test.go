package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestProtocolRejectsServerMutationRequests(t *testing.T) {
	incoming := make(chan inboundFrame, 4)
	incoming <- inboundFrame{data: []byte(`{"jsonrpc":"2.0","id":"server-1","method":"workspace/applyEdit","params":{}}`)}
	incoming <- inboundFrame{data: []byte(`{"jsonrpc":"2.0","id":55,"method":"workspace/executeCommand","params":{}}`)}
	incoming <- inboundFrame{data: []byte(`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{}}`)}
	incoming <- inboundFrame{data: []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)}
	var sent []map[string]any
	notifications := 0
	send := func(_ context.Context, v any) error { sent = append(sent, v.(map[string]any)); return nil }
	result, err := protocolRequest(context.Background(), incoming, send, 1, "initialize", nil, nil, func(string, json.RawMessage) { notifications++ })
	if err != nil || string(result) != `{"ok":true}` || len(sent) != 3 || notifications != 1 {
		t.Fatalf("exchange failed: %s %v %d %d", result, err, len(sent), notifications)
	}
	apply := sent[1]["result"].(map[string]any)
	if apply["applied"] != false || sent[2]["error"].(map[string]any)["code"] != -32601 {
		t.Fatal("server mutation request was not refused")
	}
}

func TestProtocolRejectsInvalidIDsAndResponseShapes(t *testing.T) {
	for name, message := range map[string]string{
		"wrong id":           `{"jsonrpc":"2.0","id":2,"result":null}`,
		"null id":            `{"jsonrpc":"2.0","id":null,"method":"workspace/applyEdit"}`,
		"both results":       `{"jsonrpc":"2.0","id":1,"result":null,"error":{}}`,
		"neither result":     `{"jsonrpc":"2.0","id":1}`,
		"wrong version":      `{"jsonrpc":"1.0","id":1,"result":null}`,
		"request and result": `{"jsonrpc":"2.0","id":2,"method":"workspace/applyEdit","result":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			incoming := make(chan inboundFrame, 1)
			incoming <- inboundFrame{data: []byte(message)}
			_, err := protocolRequest(context.Background(), incoming, func(context.Context, any) error { return nil }, 1, "rename", nil, nil, nil)
			if !errors.Is(err, ErrLSPProtocol) {
				t.Fatalf("invalid response accepted: %v", err)
			}
		})
	}
}

func TestProtocolConfigurationIsBoundedAndCancelReturns(t *testing.T) {
	response, err := serverRequest(json.RawMessage(`3`), "workspace/configuration", json.RawMessage(`{"items":[{"section":"gopls"},{"section":"other"}]}`), map[string]any{"vulncheck": "Off"})
	if err != nil {
		t.Fatal(err)
	}
	items := response["result"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["vulncheck"] != "Off" || items[1] != nil {
		t.Fatalf("unexpected configuration: %+v", items)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ready := make(chan inboundFrame, 1)
	ready <- inboundFrame{data: []byte(`{"jsonrpc":"2.0","id":1,"result":null}`)}
	sends := 0
	_, err = protocolRequest(ctx, ready, func(context.Context, any) error { sends++; return nil }, 1, "rename", nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	if sends != 0 {
		t.Fatal("sent a request after cancellation")
	}
}
