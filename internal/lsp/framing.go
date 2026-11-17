package lsp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrLSPFrame = errors.New("invalid or oversized LSP frame")

const maxLSPHeaderBytes = 8192

// readLSPFrame consumes exactly one Content-Length framed JSON message.
// The buffered reader must be reused so read-ahead belongs to the next frame.
func readLSPFrame(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	if reader == nil || maxBytes < 1 || maxBytes > int(resultHardBytes) {
		return nil, ErrLSPFrame
	}
	remaining := maxLSPHeaderBytes
	length := -1
	contentType := false
	for {
		line := []byte{}
		for {
			part, err := reader.ReadSlice('\n')
			if len(part) > remaining {
				return nil, ErrLSPFrame
			}
			remaining -= len(part)
			line = append(line, part...)
			if err == nil {
				break
			}
			if !errors.Is(err, bufio.ErrBufferFull) {
				return nil, ErrLSPFrame
			}
		}
		if len(line) < 2 || !bytes.HasSuffix(line, []byte("\r\n")) {
			return nil, ErrLSPFrame
		}
		line = line[:len(line)-2]
		if len(line) == 0 {
			break
		}
		name, value, ok := strings.Cut(string(line), ":")
		if !ok || name == "" || strings.TrimSpace(name) != name {
			return nil, ErrLSPFrame
		}
		switch strings.ToLower(name) {
		case "content-length":
			value = strings.TrimSpace(value)
			if length >= 0 || value == "" || len(value) > 10 {
				return nil, ErrLSPFrame
			}
			for _, c := range value {
				if c < '0' || c > '9' {
					return nil, ErrLSPFrame
				}
			}
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil || n == 0 || n > uint64(maxBytes) {
				return nil, ErrLSPFrame
			}
			length = int(n)
		case "content-type":
			// LSP permits this header but only UTF-8 JSON is supported here.
			if contentType || !strings.EqualFold(strings.TrimSpace(value), "application/vscode-jsonrpc; charset=utf-8") {
				return nil, ErrLSPFrame
			}
			contentType = true
		default:
			return nil, ErrLSPFrame
		}
	}
	if length < 0 {
		return nil, ErrLSPFrame
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil || model.ValidateWireJSON(data, maxBytes) != nil {
		return nil, ErrLSPFrame
	}
	return data, nil
}

// encodeLSPFrame validates and bounds the JSON before producing the bytes sent
// over the protocol pipe; it never writes to or logs a target source file.
func encodeLSPFrame(data []byte, maxBytes int) ([]byte, error) {
	if maxBytes < 1 || maxBytes > int(resultHardBytes) || model.ValidateWireJSON(data, maxBytes) != nil {
		return nil, ErrLSPFrame
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	out := make([]byte, 0, len(header)+len(data))
	out = append(out, header...)
	out = append(out, data...)
	return out, nil
}
