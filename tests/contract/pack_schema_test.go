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

func TestSelectorUnionAndPackAgainstSchema(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/PackRequest"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	selectors := []string{
		`{"kind":"workspace"}`, `{"kind":"symbol","symbol_id":"s"}`, `{"kind":"module","module_id":"m"}`, `{"kind":"entity","entity_id":"e"}`,
		fmt.Sprintf(`{"kind":"source_position","path":"a.go","content_hash":"%s","byte_offset":0}`, hash),
		fmt.Sprintf(`{"kind":"document_anchor","path":"a.md","content_hash":"%s","range":{"start_byte":0,"end_byte":1}}`, hash),
		fmt.Sprintf(`{"kind":"ref","ref":{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"},"service_id":null}`, id, hash, id),
	}
	for _, selector := range selectors {
		raw := `{"selector":` + selector + `,"task_kind":"plan"}`
		var wire any
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(wire); err != nil {
			t.Fatalf("schema %s: %v", raw, err)
		}
		r, err := model.DecodePackRequestJSON([]byte(raw), 8192)
		if err != nil {
			t.Fatalf("model %s: %v", raw, err)
		}
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.DecodePackRequestJSON(encoded, 8192); err != nil {
			t.Fatalf("roundtrip %s: %v", encoded, err)
		}
	}
}
