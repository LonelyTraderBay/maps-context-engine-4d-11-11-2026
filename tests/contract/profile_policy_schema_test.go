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

func TestProfileAndPolicyAgainstSchema(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	h := strings.Repeat("a", 64)
	profile := fmt.Sprintf(`{"schema_version":1,"language":"go","goos":"linux","goarch":"amd64","build_tags":[],"cgo_enabled":false,"tests_enabled":true,"module_roots":["."],"toolchain_digest":"%s","provider_digest":"%s","module_mode":"readonly","analysis_mode":"syntax","dependency_roots":[]}`, h, h)
	policy := fmt.Sprintf(`{"schema_version":1,"allowed_roots":["/a"],"denied_globs":[],"trusted_rule_paths":[],"dependency_roots":[],"allowed_executables":[{"path":"/tool","sha256":"%s"}],"allow_target_execution":false,"allow_runtime_download":false,"max_file_bytes":100,"max_source_bytes":1000,"max_output_bytes":4096,"secret_rules_version":"v1"}`, h)
	for _, tc := range []struct{ definition, raw string }{{"EffectiveProfile", profile}, {"PolicyConfig", policy}} {
		var schema jsonschema.Schema
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		schema.Ref = "#/$defs/" + tc.definition
		resolved, err := schema.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal([]byte(tc.raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(v); err != nil {
			t.Fatal(err)
		}
		if tc.definition == "EffectiveProfile" {
			if _, err := model.DecodeEffectiveProfileJSON([]byte(tc.raw), 16384); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := model.DecodePolicyConfigJSON([]byte(tc.raw), 16384); err != nil {
				t.Fatal(err)
			}
		}
	}
}
