package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestGraphDataSchemaDoesNotProveEndpoints(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/GraphData"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	ref := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	node := fmt.Sprintf(`{"node_id":"n","ref":%s,"kind":"function","label":"f","plane":"observed_static","evidence_refs":[],"source_refs":[],"expandable_count":null}`, ref)
	edge := `{"edge_id":"e","from_node_id":"n","to_node_id":"n","relation_kind":"CALLS_DIRECT","resolution_status":"exact_in_supported_scope","evidence_refs":[],"expand_ref":null}`
	raw := fmt.Sprintf(`{"operation":"graph_query","nodes":[%s],"edges":[%s],"subgraph_digest":"%s"}`, node, edge, h)
	for _, tc := range []struct {
		raw      string
		semantic bool
	}{{raw, true}, {strings.Replace(raw, `"to_node_id":"n"`, `"to_node_id":"missing"`, 1), false}} {
		var v any
		if err := json.Unmarshal([]byte(tc.raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(v); err != nil {
			t.Fatal(err)
		}
		_, err := model.DecodeGraphDataJSON([]byte(tc.raw), 32768)
		if (err == nil) != tc.semantic {
			t.Fatalf("semantic=%v err=%v", tc.semantic, err)
		}
	}
}
