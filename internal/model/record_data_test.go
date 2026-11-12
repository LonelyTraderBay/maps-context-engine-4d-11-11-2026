package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestRecordDataClosedShapes(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	summary := fmt.Sprintf(`{"record_id":"%s","version":1,"record_kind":"ui_mapping","declared_status":"approved","effective_status":"needs_review","provenance_status":"snapshot_expired","source_refs":[]}`, id)
	list := fmt.Sprintf(`{"operation":"records_list","records":[%s]}`, summary)
	if _, err := DecodeRecordListDataJSON([]byte(list), 16384); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s","path":"a.go","content_hash":"%s","range":{"start_byte":0,"end_byte":1}}`, id, h, id, h)
	target := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"item"}`, id, h, id)
	read := fmt.Sprintf(`{"operation":"records_read","record":%s,"ui_mapping":{"source_ref":%s,"destination_ref":%s,"rationale":"reason"},"limitations":[]}`, summary, source, target)
	if _, err := DecodeRecordReadDataJSON([]byte(read), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(read, `"ui_mapping"`, `"ui_mapping_extra"`, 1), strings.Replace(read, `"record_kind":"ui_mapping"`, `"record_kind":"other"`, 1), strings.Replace(read, `"version":1`, `"version":0`, 1), strings.Replace(read, `"rationale":"reason"`, `"rationale":""`, 1)} {
		if _, err := DecodeRecordReadDataJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
