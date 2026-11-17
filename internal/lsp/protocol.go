package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

var (
	ErrLSPProtocol = errors.New("invalid LSP protocol message")
	ErrLSPRemote   = errors.New("LSP request failed")
)

type inboundFrame struct {
	data []byte
	err  error
}

type sendProtocol func(context.Context, any) error

// protocolRequest is intentionally sequential: a timeout invalidates the
// session rather than assigning a late reply to a later request. Server-side
// requests are handled on the same stream and never execute commands or edits.
func protocolRequest(ctx context.Context, incoming <-chan inboundFrame, send sendProtocol, id int64, method string, params any, settings map[string]any, notify func(string, json.RawMessage)) (json.RawMessage, error) {
	if ctx == nil || incoming == nil || send == nil || id < 1 || method == "" {
		return nil, ErrLSPProtocol
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := send(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for count := 0; count < 1024; count++ {
		var frame inboundFrame
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case value, ok := <-incoming:
			if !ok {
				return nil, ErrLSPProtocol
			}
			frame = value
		}
		if frame.err != nil {
			return nil, errors.Join(ErrLSPProtocol, frame.err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var message map[string]json.RawMessage
		if json.Unmarshal(frame.data, &message) != nil || message == nil {
			return nil, ErrLSPProtocol
		}
		var version string
		if json.Unmarshal(message["jsonrpc"], &version) != nil || version != "2.0" {
			return nil, ErrLSPProtocol
		}
		if rawMethod, present := message["method"]; present {
			if _, hasResult := message["result"]; hasResult {
				return nil, ErrLSPProtocol
			}
			if _, hasError := message["error"]; hasError {
				return nil, ErrLSPProtocol
			}
			var serverMethod string
			if json.Unmarshal(rawMethod, &serverMethod) != nil || serverMethod == "" || len(serverMethod) > 256 {
				return nil, ErrLSPProtocol
			}
			if serverID, isRequest := message["id"]; isRequest {
				if !validProtocolID(serverID) {
					return nil, ErrLSPProtocol
				}
				response, err := serverRequest(serverID, serverMethod, message["params"], settings)
				if err != nil {
					return nil, err
				}
				if err := send(ctx, response); err != nil {
					return nil, err
				}
			} else if notify != nil {
				notify(serverMethod, message["params"])
			}
			continue
		}
		if !bytes.Equal(bytes.TrimSpace(message["id"]), []byte(strconv.FormatInt(id, 10))) {
			return nil, ErrLSPProtocol
		}
		result, hasResult := message["result"]
		_, hasError := message["error"]
		if hasResult == hasError {
			return nil, ErrLSPProtocol
		}
		if hasError {
			// Provider text can contain source or paths, so do not expose it.
			return nil, ErrLSPRemote
		}
		return append(json.RawMessage(nil), result...), nil
	}
	return nil, ErrLSPProtocol
}

func validProtocolID(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return len(text) <= 256
	}
	var n int64
	return json.Unmarshal(raw, &n) == nil && n >= -9007199254740991 && n <= 9007199254740991
}

func serverRequest(id json.RawMessage, method string, params json.RawMessage, settings map[string]any) (map[string]any, error) {
	response := map[string]any{"jsonrpc": "2.0", "id": id}
	switch method {
	case "workspace/applyEdit":
		response["result"] = map[string]any{"applied": false, "failureReason": "read-only preview"}
	case "workspace/configuration":
		var request struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		if json.Unmarshal(params, &request) != nil || request.Items == nil || len(request.Items) > 256 {
			return nil, ErrLSPProtocol
		}
		items := make([]any, len(request.Items))
		for i, item := range request.Items {
			if item.Section == "gopls" {
				items[i] = settings
			}
		}
		response["result"] = items
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create", "window/showMessageRequest":
		response["result"] = nil
	default:
		// Includes workspace/executeCommand and every unrecognized method.
		response["error"] = map[string]any{"code": -32601, "message": "unsupported"}
	}
	return response, nil
}
