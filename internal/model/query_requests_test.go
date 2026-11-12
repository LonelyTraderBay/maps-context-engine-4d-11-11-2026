package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestGraphAndFlowQueryContracts(t *testing.T) {
	graph := `{"root":{"kind":"workspace"},"node_limit":200,"edge_limit":500}`
	if _, err := DecodeGraphQueryRequestJSON([]byte(graph), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, strings.Replace(graph, `"node_limit":200`, `"node_limit":501`, 1), strings.Replace(graph, `"edge_limit":500`, `"edge_limit":0`, 1), strings.Replace(graph, `"root":`, `"extra":1,"root":`, 1)} {
		if _, err := DecodeGraphQueryRequestJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	ref := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	observed := `{"root_ref":` + ref + `,"mode":"observed","artifact_id":"` + h + `"}`
	if _, err := DecodeFlowQueryRequestJSON([]byte(observed), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(observed, `,"artifact_id":"`+h+`"`, "", 1), strings.Replace(observed, `"mode":"observed"`, `"mode":"invented"`, 1), strings.Replace(observed, `"artifact_id":"`+h+`"`, `"artifact_id":"bad"`, 1)} {
		if _, err := DecodeFlowQueryRequestJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
