package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var errInvalidFrame = errors.New("invalid MCP frame")

// frameReader bounds a newline-delimited frame before handing it to the SDK.
// The SDK's JSON decoder otherwise accepts duplicate JSON-RPC envelope keys.
// Tool arguments remain intact for the core's own closed validation and
// typed INVALID_ARGUMENT response.
type frameReader struct {
	source io.ReadCloser
	reader *bufio.Reader
	frame  []byte
}

func newFrameReader(source io.ReadCloser) *frameReader {
	return &frameReader{source: source, reader: bufio.NewReaderSize(source, 4096)}
}

func (r *frameReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.frame) == 0 {
		var line []byte
		for {
			part, err := r.reader.ReadSlice('\n')
			if len(part) > maxFrameBytes-len(line) {
				return 0, errInvalidFrame
			}
			line = append(line, part...)
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				if err == io.EOF && len(line) == 0 {
					return 0, io.EOF
				}
				return 0, errInvalidFrame
			}
			break
		}
		if validateEnvelope(line) != nil {
			return 0, errInvalidFrame
		}
		r.frame = line
	}
	n := copy(p, r.frame)
	r.frame = r.frame[n:]
	return n, nil
}

func (r *frameReader) Close() error { return r.source.Close() }

func validateEnvelope(line []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	start, end := -1, -1
	var value func(int, bool) error
	value = func(depth int, params bool) error {
		if depth > 32 {
			return errInvalidFrame
		}
		token, err := decoder.Token()
		if err != nil {
			return errInvalidFrame
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] || len(seen) >= 4096 {
					return errInvalidFrame
				}
				seen[name] = true
				if params && name == "arguments" {
					var raw json.RawMessage
					if decoder.Decode(&raw) != nil {
						return errInvalidFrame
					}
					end = int(decoder.InputOffset())
					start = end - len(raw)
				} else if err := value(depth+1, depth == 0 && name == "params"); err != nil {
					return err
				}
			}
			if endToken, err := decoder.Token(); err != nil || endToken != json.Delim('}') {
				return errInvalidFrame
			}
		case '[':
			for decoder.More() {
				if err := value(depth+1, false); err != nil {
					return err
				}
			}
			if endToken, err := decoder.Token(); err != nil || endToken != json.Delim(']') {
				return errInvalidFrame
			}
		default:
			return errInvalidFrame
		}
		return nil
	}
	if value(0, false) != nil {
		return errInvalidFrame
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errInvalidFrame
	}
	checked := line
	if start >= 0 {
		if start > end || end > len(line) {
			return errInvalidFrame
		}
		checked = make([]byte, 0, len(line)-(end-start)+2)
		checked = append(checked, line[:start]...)
		checked = append(checked, '{', '}')
		checked = append(checked, line[end:]...)
	}
	if model.ValidateWireJSON(checked, maxFrameBytes) != nil {
		return errInvalidFrame
	}
	return nil
}
