package model

import (
	"strings"
	"testing"
)

func TestResponseStatusAndOperationBinding(t *testing.T) {
	good := `{"schema_version":1,"request_id":"01234567-89ab-cdef-0123-456789abcdef","status":"ok","scope":null,"freshness":{"status":"not_checked","scope":"none","checked_at":null,"buffer_scope":"disk_only","consistency":"captured_bytes_with_observed_stability","changed_count":null,"source_view":"indexed_snapshot"},"coverage":{"status":"unknown","capabilities":[],"gaps":[],"scope_description":"fixture"},"readiness":"needs_context","data":{"operation":"records_list","records":[]},"pagination":{"truncated":false,"enumeration_complete":true,"returned_count":0,"required_pending":0,"optional_pending":0,"next_cursor":null,"limit_reason":null},"budget":{"max_output_bytes":4096,"serialized_bytes":0,"token_count":null,"token_estimate":null,"tokenizer_id":null,"estimate_only":false,"delivery_mode":"http"},"warnings":[],"error":null}`
	v, err := DecodeResponseJSON([]byte(good), 16384, 4, "records_list", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Data.(RecordListData); !ok {
		t.Fatal("unexpected union variant")
	}
	if _, err := DecodeResponseJSON([]byte(good), 16384, 4, "status", false); err == nil {
		t.Fatal("accepted wrong operation")
	}
	errorObject := `{"code":"ACCESS_DENIED","message":"denied","retryable":false,"suggested_action":"none","details":[]}`
	failure := strings.Replace(strings.Replace(strings.Replace(good, `"status":"ok"`, `"status":"blocked"`, 1), `"readiness":"needs_context"`, `"readiness":"blocked"`, 1), `"error":null`, `"error":`+errorObject, 1)
	if _, err := DecodeResponseJSON([]byte(failure), 16384, 4, "records_list", false); err == nil {
		t.Fatal("accepted failure data without contract permission")
	}
	if _, err := DecodeResponseJSON([]byte(failure), 16384, 4, "records_list", true); err != nil {
		t.Fatal(err)
	}
	noData := strings.Replace(failure, `"data":{"operation":"records_list","records":[]}`, `"data":null`, 1)
	if _, err := DecodeResponseJSON([]byte(noData), 16384, 4, "records_list", false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(noData, `"readiness":"blocked"`, `"readiness":"reviewable"`, 1), strings.Replace(noData, `"error":`+errorObject, `"error":null`, 1), strings.Replace(good, `"error":null`, `"error":`+errorObject, 1), strings.Replace(good, `"records":[]`, `"records":[],"extra":1`, 1)} {
		if _, err := DecodeResponseJSON([]byte(bad), 16384, 4, "records_list", false); err == nil {
			t.Fatal("accepted invalid response")
		}
	}
}
