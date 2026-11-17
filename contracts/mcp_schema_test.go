package contracts

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestMCPToolSchemasFollowSealedRegistry(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var sealed struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(schemaJSON, &sealed); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, op := range r.Operations() {
		if op.Transport != MCP {
			continue
		}
		count++
		input, output, err := r.MCPToolSchemas(op.Name)
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range []json.RawMessage{input, output} {
			var schema jsonschema.Schema
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("%s: %v", op.Name, err)
			}
			if _, err := schema.Resolve(nil); err != nil {
				t.Fatalf("%s: unresolved reference: %v", op.Name, err)
			}
		}
		var got, expected map[string]any
		if json.Unmarshal(input, &got) != nil || json.Unmarshal(sealed.Defs[op.Request], &expected) != nil {
			t.Fatal("decode request definition")
		}
		delete(got, "$defs")
		delete(got, "$schema")
		if op.Request == "RenameRequest" {
			expected["type"] = "object"
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("%s request changed from sealed definition", op.Name)
		}
		got, expected = nil, nil
		if json.Unmarshal(output, &got) != nil || json.Unmarshal(sealed.Defs["Response"], &expected) != nil {
			t.Fatal("decode response definition")
		}
		delete(got, "$defs")
		delete(got, "$schema")
		properties := got["properties"].(map[string]any)
		data := properties["data"].(map[string]any)["anyOf"].([]any)
		if data[0].(map[string]any)["$ref"] != "#/$defs/"+op.Response || data[1].(map[string]any)["type"] != "null" {
			t.Fatalf("%s response registry mismatch", op.Name)
		}
		properties["data"] = expected["properties"].(map[string]any)["data"]
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("%s response changed outside data binding", op.Name)
		}
	}
	if count != 6 {
		t.Fatalf("got %d MCP operations", count)
	}
	if _, _, err := r.MCPToolSchemas("CLI config"); err == nil {
		t.Fatal("non-MCP schema exposed")
	}
}
