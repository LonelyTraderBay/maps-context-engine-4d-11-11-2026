package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestPreviewRequestsRejectUnknownAndBadEpochs(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	attach := fmt.Sprintf(`{"origin":"http://localhost:3000","path":"/","build_map_id":"%s","permission_profile_id":"dev"}`, h)
	if _, err := DecodePreviewAttachRequestJSON([]byte(attach), 16384); err != nil {
		t.Fatal(err)
	}
	bind := fmt.Sprintf(`{"session_id":"%s","expected_revision":1,"document_epoch":1,"hmr_epoch":0,"build_map_id":"%s","pairing_nonce":"nonce","capabilities":[]}`, id, h)
	if _, err := DecodePreviewBindRequestJSON([]byte(bind), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewBindRequestJSON([]byte(strings.Replace(bind, `"document_epoch":1`, `"document_epoch":0`, 1)), 16384); err == nil {
		t.Fatal("accepted zero document epoch")
	}
	mode := fmt.Sprintf(`{"session_id":"%s","expected_revision":1,"expected_authorization_epoch":0,"mode":"interact","permission_profile_id":"dev"}`, id)
	if _, err := DecodePreviewModeRequestJSON([]byte(mode), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewModeRequestJSON([]byte(strings.Replace(mode, `"interact"`, `"execute"`, 1)), 16384); err == nil {
		t.Fatal("accepted invalid mode")
	}
	ack := fmt.Sprintf(`{"session_id":"%s","expected_revision":1,"authorization_epoch":0,"command_id":"%s","acknowledged":false}`, id, id)
	if _, err := DecodePreviewAckRequestJSON([]byte(ack), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewAckRequestJSON([]byte(strings.Replace(ack, `"acknowledged":false`, `"acknowledged":null`, 1)), 16384); err == nil {
		t.Fatal("accepted null ack")
	}
	invalidate := fmt.Sprintf(`{"session_id":"%s","expected_revision":1,"reason":"hmr"}`, id)
	if _, err := DecodePreviewInvalidateRequestJSON([]byte(invalidate), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewInvalidateRequestJSON([]byte(strings.Replace(invalidate, `"hmr"`, `"other"`, 1)), 16384); err == nil {
		t.Fatal("accepted invalid reason")
	}
	detach := fmt.Sprintf(`{"session_id":"%s"}`, id)
	if _, err := DecodePreviewDetachRequestJSON([]byte(detach), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePreviewDetachRequestJSON([]byte(strings.Replace(detach, `}`, `,"extra":true}`, 1)), 16384); err == nil {
		t.Fatal("accepted extra field")
	}
}
