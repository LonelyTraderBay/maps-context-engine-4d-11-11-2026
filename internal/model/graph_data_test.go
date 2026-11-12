package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestGraphEdgesStayWithinVisibleNodes(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	ref := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	node := fmt.Sprintf(`{"node_id":"n","ref":%s,"kind":"function","label":"f","plane":"observed_static","evidence_refs":[],"source_refs":[],"expandable_count":null}`, ref)
	edge := `{"edge_id":"e","from_node_id":"n","to_node_id":"n","relation_kind":"CALLS_DIRECT","resolution_status":"exact_in_supported_scope","evidence_refs":[],"expand_ref":null}`
	raw := fmt.Sprintf(`{"operation":"graph_query","nodes":[%s],"edges":[%s],"subgraph_digest":"%s"}`, node, edge, h)
	if _, err := DecodeGraphDataJSON([]byte(raw), 32768); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"to_node_id":"n"`, `"to_node_id":"missing"`, 1), strings.Replace(raw, `"resolution_status":"exact_in_supported_scope"`, `"resolution_status":"invented"`, 1), strings.Replace(raw, `"nodes":[`+node+`]`, `"nodes":[`+node+`,`+node+`]`, 1)} {
		if _, err := DecodeGraphDataJSON([]byte(bad), 32768); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
