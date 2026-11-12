package contract

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestRuntimeConfigRequestSemanticCAS(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/RuntimeConfigRequest"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw      string
		semantic bool
	}{{`{"action":"set","expected_digest":null,"config":{"schema_version":1}}`, true}, {`{"action":"set","config":{"schema_version":1}}`, false}, {`{"action":"show","config":{"schema_version":1}}`, false}} {
		var v any
		if err := json.Unmarshal([]byte(tc.raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(v); err != nil {
			t.Fatal(err)
		}
		_, err := model.DecodeRuntimeConfigRequestJSON([]byte(tc.raw), 8192, 4)
		if (err == nil) != tc.semantic {
			t.Fatalf("%s: %v", tc.raw, err)
		}
	}
}
