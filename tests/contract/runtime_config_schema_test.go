package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestRuntimeDefaultsAndSchemaBounds(t *testing.T) {
	spec, err := os.ReadFile("../../Maps-Context-Engine-4D_SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.SplitN(string(spec), "<!-- BEGIN MCE4D_RUNTIME_DEFAULTS_V1 -->", 2)
	if len(blocks) != 2 {
		t.Fatal("missing defaults")
	}
	fence := strings.SplitN(blocks[1], "```json", 2)
	if len(fence) != 2 {
		t.Fatal("missing JSON")
	}
	text := strings.SplitN(fence[1], "```", 2)[0]
	var expected, actual any
	if err := json.Unmarshal([]byte(text), &expected); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(model.DefaultRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("runtime defaults differ from spec")
	}
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/RuntimeConfig"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(actual); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Minimum int64 `json:"minimum"`
				Maximum int64 `json:"maximum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"core", "CoreRuntimeLimits"}, {"explorer", "ExplorerRuntimeLimits"}} {
		for key, bounds := range raw.Defs[pair[1]].Properties {
			for _, value := range []int64{bounds.Minimum - 1, bounds.Maximum + 1} {
				input := fmt.Sprintf(`{"schema_version":1,"%s":{"%s":%d}}`, pair[0], key, value)
				if _, err := model.DecodeRuntimeConfigJSON([]byte(input), 8192, 4); err == nil {
					t.Errorf("accepted out-of-schema %s", input)
				}
			}
		}
	}
}
