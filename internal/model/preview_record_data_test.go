package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestPreviewAndRecordResponse(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	preview := fmt.Sprintf(`{"operation":"preview_attach","session_id":"%s","revision":1,"document_epoch":1,"hmr_epoch":0,"authorization_epoch":0,"state":"pairing","mode_state":"inspect","pairing_nonce":"nonce","command_id":null,"expires_at":"2026-11-11T00:00:00Z","binding_id":null,"permission_version":1,"resolver_digest":null}`, id)
	if _, err := DecodePreviewDataJSON([]byte(preview), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(preview, `"permission_version":1`, `"permission_version":0`, 1), strings.Replace(preview, `"state":"pairing"`, `"state":"other"`, 1), strings.Replace(preview, `"command_id":null,`, "", 1)} {
		if _, err := DecodePreviewDataJSON([]byte(bad), 16384); err == nil {
			t.Fatal("accepted bad preview")
		}
	}
	record := fmt.Sprintf(`{"operation":"mapping_review","record_id":"%s","version":1,"record_epoch":0,"declared_status":"approved","effective_status":"needs_review"}`, id)
	if _, err := DecodeRecordDataJSON([]byte(record), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRecordDataJSON([]byte(strings.Replace(record, `"record_epoch":0`, `"record_epoch":-1`, 1)), 16384); err == nil {
		t.Fatal("accepted negative epoch")
	}
}
