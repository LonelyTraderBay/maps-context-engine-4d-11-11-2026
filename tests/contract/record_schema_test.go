package contract

import (
	"encoding/json"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
	"os"
	"strings"
	"testing"
)

func TestRecordListShapeAgainstSchema(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err = json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/RecordListData"
	compiled, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"operation":"records_list","records":[{"record_id":"01234567-89ab-cdef-0123-456789abcdef","version":1,"record_kind":"ui_mapping","declared_status":"approved","effective_status":"needs_review","provenance_status":"snapshot_expired","source_refs":[]}]}`
	for _, tc := range []struct {
		s     string
		valid bool
	}{{valid, true}, {strings.Replace(valid, `"version":1`, `"version":0`, 1), false}, {strings.Replace(valid, `"source_refs":[]`, `"source_refs":[],"extra":true`, 1), false}, {strings.Replace(valid, `"provenance_status":"snapshot_expired"`, `"provenance_status":"current"`, 1), false}} {
		var v any
		if err = json.Unmarshal([]byte(tc.s), &v); err != nil {
			t.Fatal(err)
		}
		schemaErr := compiled.Validate(v)
		_, modelErr := model.DecodeRecordListDataJSON([]byte(tc.s), 16384)
		if (schemaErr == nil) != tc.valid || (modelErr == nil) != tc.valid {
			t.Fatalf("%s schema=%v model=%v", tc.s, schemaErr, modelErr)
		}
	}
}
