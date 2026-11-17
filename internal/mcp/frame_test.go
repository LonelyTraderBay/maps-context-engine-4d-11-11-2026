package mcp

import (
	"io"
	"strings"
	"testing"
)

func TestFrameReaderRejectsAmbiguousEnvelopeAndBoundsInput(t *testing.T) {
	for _, value := range []string{
		`{"jsonrpc":"2.0","id":1,"id":2}` + "\n",
		`{"jsonrpc":"2.0","params":{"arguments":{},"arguments":{}}}` + "\n",
		`{"jsonrpc":"2.0","params":{"x":1,"\u0078":2}}` + "\n",
		`{"jsonrpc":"2.0","params":{"x":"\ud800"}}` + "\n",
		`{"jsonrpc":"2.0","params":{}}` + strings.Repeat(" ", maxFrameBytes) + "\n",
		`{"jsonrpc":"2.0"}`,
	} {
		r := newFrameReader(io.NopCloser(strings.NewReader(value)))
		if _, err := io.ReadAll(r); err == nil {
			t.Fatalf("invalid frame accepted: %.80q", value)
		}
	}
	// The raw arguments are left for the core to produce a typed tool error.
	valid := `{"jsonrpc":"2.0","params":{"arguments":{"x":1,"x":2}}}` + "\n"
	r := newFrameReader(io.NopCloser(strings.NewReader(valid)))
	got, err := io.ReadAll(r)
	if err != nil || string(got) != valid {
		t.Fatal("arguments were altered or rejected before the core")
	}
}
