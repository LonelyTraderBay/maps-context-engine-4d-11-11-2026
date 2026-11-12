package contract

import (
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"strings"
	"testing"
)

func TestRegistryPackContractFlow(t *testing.T) {
	r, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.DecodeRequest(contracts.MCP, "mce4d_context_pack", []byte(`{"selector":{"kind":"workspace"},"task_kind":"explore"}`), 32768, 4)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := v.(model.PackRequest)
	if !ok || req.FreshnessMode != "analysis_snapshot" {
		t.Fatal("unexpected request defaults")
	}
	body := `{"schema_version":1,"request_id":"01234567-89ab-cdef-0123-456789abcdef","status":"ok","scope":null,"freshness":{"status":"not_checked","scope":"none","checked_at":null,"buffer_scope":"disk_only","consistency":"captured_bytes_with_observed_stability","changed_count":null,"source_view":"indexed_snapshot"},"coverage":{"status":"partial","capabilities":[],"gaps":[],"scope_description":"fixture"},"readiness":"needs_context","data":{"operation":"context_pack","pack_token":"token","target_refs":[],"items":[],"edit_precondition_validated":false},"pagination":{"truncated":false,"enumeration_complete":true,"returned_count":0,"required_pending":0,"optional_pending":0,"next_cursor":null,"limit_reason":null},"budget":{"max_output_bytes":4096,"serialized_bytes":0,"token_count":null,"token_estimate":null,"tokenizer_id":null,"estimate_only":false,"delivery_mode":"mcp_text"},"warnings":[],"error":null}`
	response, err := r.DecodeResponse(contracts.MCP, "mce4d_context_pack", []byte(body), 131072, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err = model.ValidatePackResponseSemantics(req, response); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(body, `"readiness":"needs_context"`, `"readiness":"reviewable"`, 1)
	response, err = r.DecodeResponse(contracts.MCP, "mce4d_context_pack", []byte(bad), 131072, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err = model.ValidatePackResponseSemantics(req, response); err == nil {
		t.Fatal("accepted unsafe snapshot readiness")
	}
	if _, err = r.DecodeResponse(contracts.HTTP, "mce4d_context_pack", []byte(body), 131072, 4); err == nil {
		t.Fatal("accepted wrong transport")
	}
}
