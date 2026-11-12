package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestPreviewStatusFailClosed(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	good := fmt.Sprintf(`{"operation":"preview_status","session_id":"%s","revision":1,"authorization_epoch":0,"document_epoch":1,"hmr_epoch":0,"state":"bound","mode_state":"interact","binding_id":"%s","permission_profile_id":"profile","permission_version":1,"permission_status":"approved","expires_at":"2026-11-11T06:00:00Z","server_time":"2026-11-11T05:00:00Z","interaction_allowed":true}`, id, hash)
	if _, err := DecodePreviewStatusDataJSON([]byte(good), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(good, `"permission_status":"approved"`, `"permission_status":"revoked"`, 1),
		strings.Replace(good, `"state":"bound"`, `"state":"stale"`, 1),
		strings.Replace(good, `"expires_at":"2026-11-11T06:00:00Z"`, `"expires_at":"2026-11-11T05:00:00Z"`, 1),
		strings.Replace(good, `"expires_at":"2026-11-11T06:00:00Z"`, `"expires_at":"bad"`, 1),
		strings.Replace(good, `"binding_id":"`+hash+`"`, `"binding_id":null`, 1),
		strings.Replace(good, `"revision":1`, `"revision":0`, 1),
		strings.Replace(good, `"state":`, `"extra":true,"state":`, 1),
	} {
		if _, err := DecodePreviewStatusDataJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	safe := strings.Replace(strings.Replace(good, `"permission_status":"approved"`, `"permission_status":"revoked"`, 1), `"interaction_allowed":true`, `"interaction_allowed":false`, 1)
	if _, err := DecodePreviewStatusDataJSON([]byte(safe), 16384); err != nil {
		t.Fatal(err)
	}
}
