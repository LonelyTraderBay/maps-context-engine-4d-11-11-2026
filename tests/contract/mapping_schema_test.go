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

func TestMappingDataBindingIsSemantic(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/MappingData"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	ref := fmt.Sprintf(`{"kind":"source_anchor","worktree_id":"%s","profile_id":"%s","generation_id":"%s","build_map_id":"%s","anchor_id":"%s","binding_id":"%s"}`, id, h, id, h, h, b)
	match := fmt.Sprintf(`{"match_id":"%s","anchor_ref":%s,"target_ref":null,"source_refs":[],"mapping_status":"unmapped","evidence_refs":[]}`, h, ref)
	raw := fmt.Sprintf(`{"operation":"mapping_resolve","requested_ref":%s,"matches":[%s],"build_map_id":"%s","binding_id":"%s","resolver_digest":"%s","mapping_status":"unmapped","why_matched":"none"}`, ref, match, h, b, h)
	for _, tc := range []struct {
		raw      string
		semantic bool
	}{{raw, true}, {strings.Replace(raw, `"binding_id":"`+b+`"`, `"binding_id":"`+h+`"`, 1), false}} {
		var v any
		if err := json.Unmarshal([]byte(tc.raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(v); err != nil {
			t.Fatal(err)
		}
		_, err := model.DecodeMappingDataJSON([]byte(tc.raw), 16384)
		if (err == nil) != tc.semantic {
			t.Fatalf("semantic %v: %v", tc.semantic, err)
		}
	}
}
