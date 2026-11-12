package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestPolicyDigestRespectsOrderingAndRules(t *testing.T) {
	hash := strings.Repeat("a", 64)
	format := `{"schema_version":1,"allowed_roots":[%s],"denied_globs":["*.secret"],"trusted_rule_paths":[%s],"dependency_roots":[],"allowed_executables":[{"path":"/tool","sha256":"%s"}],"allow_target_execution":false,"allow_runtime_download":false,"max_file_bytes":100,"max_source_bytes":1000,"max_output_bytes":4096,"secret_rules_version":"v1"}`
	raw := fmt.Sprintf(format, `"/z","/a"`, `"docs/a","docs/b"`, hash)
	p, err := DecodePolicyConfigJSON([]byte(raw), 16384)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := PolicyDigest(p, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "817e73083ef410745ecf975d9f200edcba7714b45ca8c4815373bfd607afb227" {
		t.Fatalf("does not match Python reference: %s", digest)
	}
	equivalent := fmt.Sprintf(format, `"/a","/z"`, `"docs/a","docs/b"`, hash)
	q, err := DecodePolicyConfigJSON([]byte(equivalent), 16384)
	if err != nil {
		t.Fatal(err)
	}
	same, err := PolicyDigest(q, 16384)
	if err != nil || same != digest {
		t.Fatal("root sorting changed digest")
	}
	changed := fmt.Sprintf(format, `"/a","/z"`, `"docs/b","docs/a"`, hash)
	q, err = DecodePolicyConfigJSON([]byte(changed), 16384)
	if err != nil {
		t.Fatal(err)
	}
	other, err := PolicyDigest(q, 16384)
	if err != nil || other == digest {
		t.Fatal("trusted rule order was lost")
	}
	for _, bad := range []string{strings.Replace(raw, `"allow_target_execution":false`, `"allow_target_execution":true`, 1), strings.Replace(raw, `"/z","/a"`, `"/a","/a"`, 1), strings.Replace(raw, `"docs/a"`, `"../a"`, 1), strings.Replace(raw, `"sha256":`, `"extra":1,"sha256":`, 1)} {
		if _, err := DecodePolicyConfigJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
