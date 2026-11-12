package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestPreviewPermissionAdmin(t *testing.T) {
	p := `{"schema_version":1,"profile_id":"dev","approved_origin":"http://localhost:3000","approved_path_prefix":"/","target_environment":"local_mock","source_worktree_ids":["11234567-89ab-cdef-0123-456789abcdef","01234567-89ab-cdef-0123-456789abcdef"],"allow_attach":true,"allow_interact":false,"allow_scroll":false,"allowed_dev_destinations":["http://b","http://a"],"attestation_id":"a","attested_by":"owner","attested_at":"2026-11-11T00:00:00Z","expires_at":"2026-11-12T00:00:00Z","version":1}`
	put := fmt.Sprintf(`{"action":"put","expected_version":0,"profile":%s}`, p)
	if _, err := DecodePreviewPermissionAdminRequestJSON([]byte(put), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewPermissionAdminRequestJSON([]byte(strings.Replace(put, `"expected_version":0`, `"expected_version":1`, 1)), 16384); err == nil {
		t.Fatal("accepted bad CAS version")
	}
	revoke := `{"action":"revoke","profile_id":"dev","expected_version":1,"reason":"removed"}`
	if _, err := DecodePreviewPermissionAdminRequestJSON([]byte(revoke), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewPermissionAdminRequestJSON([]byte(strings.Replace(revoke, `,"reason":"removed"`, "", 1)), 16384); err == nil {
		t.Fatal("accepted missing reason")
	}
	summary := fmt.Sprintf(`{"operation":"preview_permission_admin","profiles":[{"profile_id":"dev","version":1,"declared_status":"approved","effective_status":"approved","digest":"b873da0467ce3939ccaf4cb176567a20eece3ade2568fb62d597b89409eafc31","profile":%s}]}`, p)
	if _, err := DecodePreviewPermissionAdminDataJSON([]byte(summary), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewPermissionAdminDataJSON([]byte(strings.Replace(summary, `"declared_status":"approved"`, `"declared_status":"revoked"`, 1)), 16384); err == nil {
		t.Fatal("accepted revoked permission as approved")
	}
	if _, err := DecodePreviewPermissionAdminDataJSON([]byte(strings.Replace(summary, `"version":1`, `"version":2`, 1)), 16384); err == nil {
		t.Fatal("accepted mismatched version")
	}
}
