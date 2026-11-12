package model

import (
	"strings"
	"testing"
)

func TestFreshnessUTCAndUnknown(t *testing.T) {
	good := `{"status":"not_checked","scope":"none","checked_at":null,"buffer_scope":"disk_only","consistency":"captured_bytes_with_observed_stability","changed_count":null,"source_view":"indexed_snapshot"}`
	if _, err := DecodeFreshnessJSON([]byte(good), 8192); err != nil {
		t.Fatal(err)
	}
	unsafe := strings.Replace(strings.Replace(good, `"status":"not_checked"`, `"status":"observed_current"`, 1), `"consistency":"captured_bytes_with_observed_stability"`, `"consistency":"captured_buffer_version"`, 1)
	if _, err := DecodeFreshnessJSON([]byte(unsafe), 8192); err == nil {
		t.Fatal("accepted unproven current state")
	}
	for _, bad := range []string{strings.Replace(good, `"status":"not_checked"`, `"status":"invented"`, 1), strings.Replace(good, `"checked_at":null`, `"checked_at":"not-time"`, 1), strings.Replace(good, `"changed_count":null`, `"changed_count":-1`, 1), strings.Replace(good, `"scope":`, `"extra":0,"scope":`, 1)} {
		if _, err := DecodeFreshnessJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
func TestCoverageSchemaFields(t *testing.T) {
	good := `{"status":"partial","capabilities":[{"name":"go","provider_id":"provider","provider_version":"v1","status":"partial"}],"gaps":[{"code":"SKIPPED","reason":"not indexed","count":null,"source_refs":[]}],"scope_description":"fixture"}`
	if _, err := DecodeCoverageJSON([]byte(good), 8192); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(good, `"source_refs":[]`, `"source_refs":null`, 1), strings.Replace(good, `"provider_version":"v1"`, `"provider_version":""`, 1), strings.Replace(good, `"count":null`, `"count":-1`, 1), strings.Replace(good, `"gaps":`, `"extra":0,"gaps":`, 1)} {
		if _, err := DecodeCoverageJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
