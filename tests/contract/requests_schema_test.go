package contract

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestLocateSchemaAndSemanticRequirements(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/LocateRequest"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input                      string
		schemaValid, semanticValid bool
	}{
		{`{"query":"hello"}`, true, true}, {`{}`, true, false}, {`{"query":"hello","limit":101}`, false, false}, {`{"query":"hello","unknown":1}`, false, false},
	} {
		var v any
		if err := json.Unmarshal([]byte(tc.input), &v); err != nil {
			t.Fatal(err)
		}
		se := resolved.Validate(v)
		_, me := model.DecodeLocateRequestJSON([]byte(tc.input), 8192)
		if (se == nil) != tc.schemaValid || (me == nil) != tc.semanticValid {
			t.Errorf("%s schema=%v semantic=%v", tc.input, se, me)
		}
	}
}
