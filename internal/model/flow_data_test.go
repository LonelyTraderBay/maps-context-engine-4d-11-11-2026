package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestFlowDataKeepsStaticAndObservedSeparate(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	ref := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	step := fmt.Sprintf(`{"step_id":"step","canonical_refs":[%s],"source_refs":[],"step_kind":"handler","order_semantics":"call_relationship_only","possible_or_observed":"statically_possible","outcome_status":"statically_possible","evidence_refs":[],"limitations":[]}`, ref)
	edge := `{"edge_id":"edge","from_step_id":"step","to_step_id":"step","kind":"call","condition_source_ref":null,"evidence_refs":[]}`
	raw := fmt.Sprintf(`{"operation":"flow_query","projection_id":"%s","root_ref":%s,"mode":"static","steps":[%s],"edges":[%s]}`, h, ref, step, edge)
	if _, err := DecodeFlowDataJSON([]byte(raw), 32768); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"to_step_id":"step"`, `"to_step_id":"missing"`, 1), strings.Replace(raw, `"outcome_status":"statically_possible"`, `"outcome_status":"commit_observed"`, 1), strings.Replace(raw, `"possible_or_observed":"statically_possible"`, `"possible_or_observed":"observed"`, 1), strings.Replace(raw, `"order_semantics":"call_relationship_only"`, `"order_semantics":"causal_trace"`, 1)} {
		if _, err := DecodeFlowDataJSON([]byte(bad), 32768); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
