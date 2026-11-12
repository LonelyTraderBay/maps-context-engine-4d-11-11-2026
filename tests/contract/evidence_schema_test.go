package contract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestEvidenceSchemaAndSemanticMatrix(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/EvidenceRef"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	positive := `{"evidence_id":"e","origin":"static_analyzer","kind":"explicit_document","source_refs":[],"record_ref":null,"artifact_id":"` + strings.Repeat("a", 64) + `"}`
	incompatible := strings.Replace(positive, `"static_analyzer"`, `"ai_proposal"`, 1)
	for _, tc := range []struct {
		name, raw string
		semantic  bool
	}{{"document", positive, true}, {"schema-valid but wrong origin", incompatible, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var wire any
			if err := json.Unmarshal([]byte(tc.raw), &wire); err != nil {
				t.Fatal(err)
			}
			if err := resolved.Validate(wire); err != nil {
				t.Fatal(err)
			}
			_, err := model.DecodeEvidenceRefJSON([]byte(tc.raw), 8192)
			if (err == nil) != tc.semantic {
				t.Fatalf("semantic=%v err=%v", tc.semantic, err)
			}
		})
	}
}
