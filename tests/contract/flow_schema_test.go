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

func TestStaticFlowDoesNotClaimObservedCommit(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/FlowData"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	ref := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	step := fmt.Sprintf(`{"step_id":"step","canonical_refs":[%s],"source_refs":[],"step_kind":"handler","order_semantics":"call_relationship_only","possible_or_observed":"statically_possible","outcome_status":"statically_possible","evidence_refs":[],"limitations":[]}`, ref)
	raw := fmt.Sprintf(`{"operation":"flow_query","projection_id":"%s","root_ref":%s,"mode":"static","steps":[%s],"edges":[]}`, h, ref, step)
	for _, tc := range []struct {
		raw      string
		semantic bool
	}{{raw, true}, {strings.Replace(raw, `"outcome_status":"statically_possible"`, `"outcome_status":"commit_observed"`, 1), false}} {
		var v any
		if err := json.Unmarshal([]byte(tc.raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(v); err != nil {
			t.Fatal(err)
		}
		_, err := model.DecodeFlowDataJSON([]byte(tc.raw), 32768)
		if (err == nil) != tc.semantic {
			t.Fatalf("semantic=%v err=%v", tc.semantic, err)
		}
	}
}
