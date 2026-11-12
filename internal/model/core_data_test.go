package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestStatusLocateAndValidateData(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	status := fmt.Sprintf(`{"operation":"status","scopes":[],"capabilities":[],"project_stage":"spec_only","index_state":"missing","registered_scopes":[{"worktree_id":"%s","profile_id":null,"root_status":"registered","index_state":"missing"}]}`, id)
	if _, err := DecodeStatusDataJSON([]byte(status), 16384); err != nil {
		t.Fatal(err)
	}
	ref := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	locate := fmt.Sprintf(`{"operation":"locate","candidates":[{"ref":%s,"name":"s","qualified_name":"pkg.s","source_refs":[],"evidence_refs":[]}]}`, ref)
	if _, err := DecodeLocateDataJSON([]byte(locate), 16384); err != nil {
		t.Fatal(err)
	}
	good := `{"operation":"validate_pack","edit_precondition_validated":true,"changed_sources":[],"membership_changed":false,"configuration_changed":false,"record_set_changed":false}`
	if _, err := DecodeValidateDataJSON([]byte(good), 16384); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(good, `"membership_changed":false`, `"membership_changed":true`, 1)
	if _, err := DecodeValidateDataJSON([]byte(bad), 16384); err == nil {
		t.Fatal("accepted validated state after membership change")
	}
}
