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

func TestGraphRefClosedUnionAgainstSchema(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/GraphRef"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	common := fmt.Sprintf(`"worktree_id":"%s","profile_id":"%s","generation_id":"%s"`, id, hash, id)
	for _, raw := range []string{
		`{ "kind":"symbol",` + common + `,"symbol_id":"s"}`,
		`{ "kind":"entity",` + common + `,"entity_id":"e"}`,
		`{ "kind":"module",` + common + `,"module_id":"m"}`,
		`{ "kind":"source_anchor",` + common + `,"build_map_id":"` + hash + `","anchor_id":"` + hash + `","binding_id":"` + hash + `"}`,
		`{ "kind":"file",` + common + `,"path":"a.go","content_hash":null}`,
	} {
		var wire any
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(wire); err != nil {
			t.Fatalf("schema rejects %s: %v", raw, err)
		}
		ref, err := model.DecodeGraphRefJSON([]byte(raw), 8192)
		if err != nil {
			t.Fatalf("model rejects %s: %v", raw, err)
		}
		roundtrip, err := json.Marshal(ref)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.DecodeGraphRefJSON(roundtrip, 8192); err != nil {
			t.Fatalf("invalid roundtrip %s: %v", roundtrip, err)
		}
	}
}
