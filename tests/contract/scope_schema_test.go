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

func TestScopeVectorSchemaAndModelAgreeOnShape(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/ScopeVector"
	compiled, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	id := "01234567-89ab-cdef-0123-456789abcdef"
	valid := fmt.Sprintf(`{"kind":"federated","members":[],"vector_digest":"%s","group_id":"%s","group_revision":1,"catalog_incarnation":"%s","unavailable_members":[{"service_id":null,"worktree_id":null,"profile_id":null,"status":"access_denied","reason_code":"REDACTED"}]}`, hash, id, id)
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"redacted unavailable", valid, true},
		{"unknown field", strings.Replace(valid, `"reason_code":`, `"extra":true,"reason_code":`, 1), false},
		{"missing nullable", strings.Replace(valid, `"service_id":null,`, "", 1), false},
		{"bad hash", strings.Replace(valid, hash, "not-a-hash", 1), false},
		{"bad status", strings.Replace(valid, "access_denied", "other", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(tc.input), &v); err != nil {
				t.Fatal(err)
			}
			schemaErr := compiled.Validate(v)
			_, modelErr := model.DecodeScopeVectorJSON([]byte(tc.input), 16384)
			if (schemaErr == nil) != tc.valid || (modelErr == nil) != tc.valid {
				t.Fatalf("valid=%v schema=%v model=%v", tc.valid, schemaErr, modelErr)
			}
		})
	}
}
