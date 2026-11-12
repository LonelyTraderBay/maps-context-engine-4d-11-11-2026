package model

import "testing"

func TestPolicyCapsCannotBeWidenedByRuntime(t *testing.T) {
	cfg, err := DecodeRuntimeConfigJSON([]byte(`{"schema_version":1}`), 16384, 4)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := DecodePolicyConfigJSON([]byte(`{"schema_version":1,"allowed_roots":["/a"],"denied_globs":[],"trusted_rule_paths":[],"dependency_roots":[],"allowed_executables":[],"allow_target_execution":false,"allow_runtime_download":false,"max_file_bytes":1000,"max_source_bytes":500,"max_output_bytes":4096,"secret_rules_version":"v1"}`), 16384)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := RuntimePolicyCaps(cfg, policy)
	if err != nil {
		t.Fatal(err)
	}
	if caps.MaxFileBytes != 500 || caps.MaxSourceBytesPerSnapshot != 500 || caps.MaxOutputBytes != 4096 {
		t.Fatalf("policy widened: %+v", caps)
	}
	if cfg.Core.MaxFileBytes == caps.MaxFileBytes {
		t.Fatal("mutated config")
	}
}
