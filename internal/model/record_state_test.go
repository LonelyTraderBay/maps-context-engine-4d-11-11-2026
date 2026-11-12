package model

import (
	"strings"
	"testing"
)

func TestRecordApprovalMustRemainCurrent(t *testing.T) {
	summary := `{"record_id":"01234567-89ab-cdef-0123-456789abcdef","version":1,"record_kind":"ui_mapping","declared_status":"approved","effective_status":"needs_review","provenance_status":"snapshot_expired","source_refs":[]}`
	if _, err := DecodeRecordSummaryJSON([]byte(summary), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRecordSummaryJSON([]byte(strings.Replace(summary, `"effective_status":"needs_review"`, `"effective_status":"approved"`, 1)), 16384); err == nil {
		t.Fatal("accepted approved expired snapshot")
	}
	response := `{"operation":"mapping_review","record_id":"01234567-89ab-cdef-0123-456789abcdef","version":1,"record_epoch":0,"declared_status":"revoked","effective_status":"approved"}`
	if _, err := DecodeRecordDataJSON([]byte(response), 16384); err == nil {
		t.Fatal("accepted revoked record approval")
	}
}
