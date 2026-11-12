package model

import (
	"strings"
	"testing"
)

func TestProviderFactBoundaries(t *testing.T) {
	symbol := `{"symbol_id":"s","path":null,"package_key":"pkg","kind":"external","name":"X","qualified_name":"pkg.X","scope_key":"pkg","range":null,"signature":"","exported":true}`
	if _, err := DecodeSymbolFactJSON([]byte(symbol), 8192); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(symbol, `"kind":"external"`, `"kind":"function"`, 1), strings.Replace(symbol, `"qualified_name":"pkg.X"`, `"qualified_name":""`, 1), strings.Replace(symbol, `"package_key":"pkg"`, `"package_key":null`, 1), strings.Replace(symbol, `"range":null`, `"range":{"start_byte":0,"end_byte":1}`, 1), strings.Replace(symbol, `"path":null`, `"path":"a.go"`, 1)} {
		if _, err := DecodeSymbolFactJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	rel := `{"relation_id":"r","source_symbol_id":"s","target_symbol_id":null,"kind":"UNRESOLVED_CALL","evidence_kind":"unresolved","site_path":"a.go","range":{"start_byte":0,"end_byte":1},"reason":"dynamic","unresolved_label":"f"}`
	if _, err := DecodeRelationFactJSON([]byte(rel), 8192); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(rel, `"kind":"UNRESOLVED_CALL"`, `"kind":"CALLS_DIRECT"`, 1), strings.Replace(rel, `"reason":"dynamic"`, `"reason":""`, 1), strings.Replace(rel, `"unresolved_label":"f"`, `"unresolved_label":null`, 1), strings.Replace(rel, `"evidence_kind":"unresolved"`, `"evidence_kind":"type_resolved"`, 1), strings.Replace(rel, `"target_symbol_id":null`, `"target_symbol_id":"x"`, 1), strings.Replace(rel, `"site_path":"a.go"`, `"site_path":"../a.go"`, 1)} {
		if _, err := DecodeRelationFactJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
