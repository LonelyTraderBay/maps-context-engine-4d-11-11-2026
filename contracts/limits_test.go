package contracts

import (
	"bytes"
	"testing"
)

func TestRequestHardCapsCannotBeRaised(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		transport  Transport
		name, body string
		cap        int
	}{{HTTP, "POST /session/bootstrap", `{"code":"x"}`, 16384}, {MCP, "mce4d_workspace_status", `{}`, 32768}, {HTTP, "POST /graph/query", `{"root":{"kind":"workspace"}}`, 65536}}
	for _, tc := range cases {
		op, err := r.Lookup(tc.transport, tc.name)
		if err != nil || op.RequestHardLimit() != tc.cap {
			t.Fatalf("limit %s: %d %v", tc.name, op.RequestHardLimit(), err)
		}
		if _, err := r.DecodeRequest(tc.transport, tc.name, []byte(tc.body), tc.cap, 4); err != nil {
			t.Fatalf("valid fixture %s: %v", tc.name, err)
		}
		raw := append([]byte(tc.body), bytes.Repeat([]byte(" "), tc.cap)...)
		if _, err := r.DecodeRequest(tc.transport, tc.name, raw, len(raw), 4); err == nil {
			t.Fatalf("accepted oversized %s", tc.name)
		}
	}
}
