package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestPackRequestDefaultsAndSelector(t *testing.T) {
	hash := strings.Repeat("a", 64)
	good := `{"selector":{"kind":"workspace"},"task_kind":"plan"}`
	r, err := DecodePackRequestJSON([]byte(good), 8192)
	if err != nil {
		t.Fatal(err)
	}
	if r.FreshnessMode != "analysis_snapshot" || r.View == nil || *r.View != "overview" {
		t.Fatalf("wrong defaults: %+v", r)
	}
	pos := fmt.Sprintf(`{"kind":"source_position","path":"a.go","content_hash":"%s","byte_offset":1}`, hash)
	r, err = DecodePackRequestJSON([]byte(`{"selector":`+pos+`,"task_kind":"rename"}`), 8192)
	if err != nil || r.FreshnessMode != "checked_workspace" || r.View == nil || *r.View != "symbol" {
		t.Fatalf("rename: %+v %v", r, err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ref := fmt.Sprintf(`{"selector":{"kind":"ref","ref":{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"},"service_id":null},"task_kind":"plan"}`, id, hash, id)
	selected, err := DecodePackRequestJSON([]byte(ref), 8192)
	if err != nil || selected.View == nil || *selected.View != "symbol" {
		t.Fatalf("symbol reference default: %+v %v", selected, err)
	}
	for _, bad := range []string{`{"selector":{"kind":"workspace"},"task_kind":"rename"}`, `{"selector":{"kind":"workspace","extra":1},"task_kind":"plan"}`, `{"selector":{"kind":"symbol","symbol_id":""},"task_kind":"plan"}`, `{"selector":{"kind":"workspace"},"task_kind":"plan","constraints":["invented"]}`, `{"selector":{"kind":"workspace"},"task_kind":"plan","view":"invented"}`} {
		if _, err := DecodePackRequestJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
