package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestAdminRequestsRequireCASAndTypedBodies(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	profile := fmt.Sprintf(`{"schema_version":1,"language":"go","goos":"linux","goarch":"amd64","build_tags":[],"cgo_enabled":false,"tests_enabled":false,"module_roots":["."],"toolchain_digest":"%s","provider_digest":"%s","module_mode":"readonly","analysis_mode":"syntax","dependency_roots":[]}`, h, h)
	raw := fmt.Sprintf(`{"worktree_id":"%s","profile_name":"default","expected_revision":0,"profile":%s}`, id, profile)
	if _, err := DecodeProfileAdminRequestJSON([]byte(raw), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `,"expected_revision":0`, "", 1), strings.Replace(raw, `"expected_revision":0`, `"expected_revision":-1`, 1), strings.Replace(raw, `"profile_name":"default"`, `"profile_name":""`, 1)} {
		if _, err := DecodeProfileAdminRequestJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	policy := `{"schema_version":1,"allowed_roots":["/a"],"denied_globs":[],"trusted_rule_paths":[],"dependency_roots":[],"allowed_executables":[],"allow_target_execution":false,"allow_runtime_download":false,"max_file_bytes":100,"max_source_bytes":1000,"max_output_bytes":4096,"secret_rules_version":"v1"}`
	raw = fmt.Sprintf(`{"worktree_id":"%s","expected_epoch":0,"policy":%s}`, id, policy)
	if _, err := DecodePolicyAdminRequestJSON([]byte(raw), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `,"expected_epoch":0`, "", 1), strings.Replace(raw, `"allow_runtime_download":false`, `"allow_runtime_download":true`, 1), strings.Replace(raw, `"worktree_id":`, `"extra":1,"worktree_id":`, 1)} {
		if _, err := DecodePolicyAdminRequestJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
