package contracts

import (
	"bytes"
	"strings"
	"testing"
)

func TestResponseHardCapsCannotBeRaised(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	h := strings.Repeat("a", 64)
	body := `{"schema_version":1,"request_id":"01234567-89ab-cdef-0123-456789abcdef","status":"ok","scope":null,"freshness":{"status":"not_checked","scope":"none","checked_at":null,"buffer_scope":"disk_only","consistency":"captured_bytes_with_observed_stability","changed_count":null,"source_view":"indexed_snapshot"},"coverage":{"status":"unknown","capabilities":[],"gaps":[],"scope_description":"fixture"},"readiness":"needs_context","data":{"operation":"graph_query","nodes":[],"edges":[],"subgraph_digest":"` + h + `"},"pagination":{"truncated":false,"enumeration_complete":true,"returned_count":0,"required_pending":0,"optional_pending":0,"next_cursor":null,"limit_reason":null},"budget":{"max_output_bytes":4096,"serialized_bytes":0,"token_count":null,"token_estimate":null,"tokenizer_id":null,"estimate_only":false,"delivery_mode":"http"},"warnings":[],"error":null}`
	if _, err := r.DecodeResponse(HTTP, "POST /graph/query", []byte(body), 1048576, 4); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(body), bytes.Repeat([]byte(" "), 1048576)...)
	if _, err := r.DecodeResponse(HTTP, "POST /graph/query", raw, len(raw), 4); err == nil {
		t.Fatal("accepted oversized graph response")
	}
	if _, err := r.DecodeResponse(HTTP, "POST /graph/query", []byte(body), 16, 4); err == nil {
		t.Fatal("ignored caller lower cap")
	}
}
