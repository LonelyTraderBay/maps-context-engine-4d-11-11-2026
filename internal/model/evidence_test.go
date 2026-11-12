package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestEvidenceOriginKindMatrix(t *testing.T) {
	cases := map[string][]string{
		"static_analyzer":       {"syntax_exact", "type_resolved", "type_possible", "explicit_document", "text_match", "unresolved"},
		"build_instrumentation": {"build_anchored", "unresolved"},
		"runtime_observation":   {"runtime_observed", "unresolved"},
		"manual_approved":       {"manual_declared"},
		"ai_proposal":           {"proposed"},
	}
	kinds := []string{"syntax_exact", "type_resolved", "type_possible", "explicit_document", "text_match", "unresolved", "build_anchored", "runtime_observed", "manual_declared", "proposed"}
	for origin, allowed := range cases {
		for _, kind := range kinds {
			input := fmt.Sprintf(`{"evidence_id":"e1","origin":"%s","kind":"%s","source_refs":[],"record_ref":null,"artifact_id":"%s"}`, origin, kind, strings.Repeat("a", 64))
			_, err := DecodeEvidenceRefJSON([]byte(input), 8192)
			want := false
			for _, a := range allowed {
				if a == kind {
					want = true
				}
			}
			if (err == nil) != want {
				t.Errorf("%s/%s want %v, error %v", origin, kind, want, err)
			}
		}
	}
}
func TestEvidenceRefClosedShape(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	input := fmt.Sprintf(`{"evidence_id":"e","origin":"manual_approved","kind":"manual_declared","source_refs":[],"record_ref":{"record_id":"%s","version":1},"artifact_id":"%s"}`, id, hash)
	if _, err := DecodeEvidenceRefJSON([]byte(input), 8192); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(input, `"version":1`, `"version":0`, 1), strings.Replace(input, `"record_id":`, `"extra":1,"record_id":`, 1), strings.Replace(input, `"source_refs":[]`, `"source_refs":null`, 1), strings.Replace(input, `,"artifact_id":"`+hash+`"`, "", 1)} {
		if _, err := DecodeEvidenceRefJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestExplicitDocumentRequiresReference(t *testing.T) {
	input := `{"evidence_id":"e","origin":"static_analyzer","kind":"explicit_document","source_refs":[],"record_ref":null,"artifact_id":null}`
	if _, err := DecodeEvidenceRefJSON([]byte(input), 8192); err == nil {
		t.Fatal("accepted document evidence without an anchor")
	}
}
