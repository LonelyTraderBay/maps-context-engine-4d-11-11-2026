package contract

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// This verifies the schema artifact with the already pinned SDK dependency.
// Format annotation and CT-02 semantic checks require independent validators.
func TestSchemaResolvesAndSourceRefFixtures(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Defs) != 153 {
		t.Fatalf("got %d definitions", len(schema.Defs))
	}
	schema.Ref = "#/$defs/SourceRef"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve schema: %v", err)
	}
	good := `{"worktree_id":"01234567-89ab-cdef-0123-456789abcdef","profile_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation_id":"01234567-89ab-cdef-0123-456789abcdef","path":"src/main.go","content_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","range":{"start_byte":0,"end_byte":4}}`
	cases := []struct {
		name, input string
		valid       bool
	}{
		{"valid", good, true},
		{"unknown", `{"worktree_id":"01234567-89ab-cdef-0123-456789abcdef","profile_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation_id":"01234567-89ab-cdef-0123-456789abcdef","path":"x","content_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","range":{"start_byte":0,"end_byte":4},"extra":true}`, false},
		{"missing", `{"path":"x"}`, false},
		{"bad type", `{"worktree_id":1}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(tc.input), &v); err != nil {
				t.Fatal(err)
			}
			err := resolved.Validate(v)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
