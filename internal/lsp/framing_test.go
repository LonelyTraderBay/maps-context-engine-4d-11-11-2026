package lsp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type byteReader struct{ r io.Reader }

func (r byteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}

func TestLSPFramingSplitAndConsecutiveMessages(t *testing.T) {
	first := []byte(`{"jsonrpc":"2.0","id":1,"result":"😀"}`)
	second := []byte(`{"jsonrpc":"2.0","method":"ready"}`)
	a, err := encodeLSPFrame(first, 1024)
	if err != nil {
		t.Fatal(err)
	}
	b, err := encodeLSPFrame(second, 1024)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReaderSize(byteReader{bytes.NewReader(append(a, b...))}, 16)
	for _, want := range [][]byte{first, second} {
		got, err := readLSPFrame(reader, 1024)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("frame %q: %q %v", want, got, err)
		}
	}
}

func TestLSPFramingRejectsAmbiguousOrOversizedInput(t *testing.T) {
	for name, input := range map[string]string{
		"missing length":        "X: 3\r\n\r\n{}",
		"duplicate length":      "Content-Length: 2\r\ncontent-length: 2\r\n\r\n{}",
		"negative length":       "Content-Length: -1\r\n\r\n{}",
		"overflow":              "Content-Length: 99999999999999999\r\n\r\n{}",
		"exceeds cap":           "Content-Length: 1000\r\n\r\n{}",
		"zero":                  "Content-Length: 0\r\n\r\n",
		"short body":            "Content-Length: 3\r\n\r\n{}",
		"newline framing":       "Content-Length: 2\n\n{}",
		"long header":           "Content-Length: " + strings.Repeat("1", 9000) + "\r\n\r\n{}",
		"non UTF8 content type": "Content-Length: 2\r\nContent-Type: application/vscode-jsonrpc; charset=latin1\r\n\r\n{}",
		"duplicate JSON":        "Content-Length: 13\r\n\r\n{\"x\":1,\"x\":2}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readLSPFrame(bufio.NewReaderSize(strings.NewReader(input), 16), 64)
			if !errors.Is(err, ErrLSPFrame) {
				t.Fatalf("accepted malformed frame: %v", err)
			}
		})
	}
	if _, err := encodeLSPFrame([]byte(`{"a":1,"a":2}`), 64); !errors.Is(err, ErrLSPFrame) {
		t.Fatalf("duplicate JSON emitted: %v", err)
	}
}

func TestLSPFramingAcceptsUTF8ContentType(t *testing.T) {
	raw := "Content-Length: 2\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n{}"
	got, err := readLSPFrame(bufio.NewReader(strings.NewReader(raw)), 32)
	if err != nil || string(got) != "{}" {
		t.Fatalf("got %q %v", got, err)
	}
}
