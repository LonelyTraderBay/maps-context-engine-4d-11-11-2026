package model

import (
	"strings"
	"testing"
)

func TestPaginationWireAndPendingSemantics(t *testing.T) {
	complete := `{"truncated":false,"enumeration_complete":true,"returned_count":3,"required_pending":0,"optional_pending":0,"next_cursor":null,"limit_reason":null}`
	pending := `{"truncated":true,"enumeration_complete":false,"returned_count":3,"required_pending":null,"optional_pending":0,"next_cursor":"cursor","limit_reason":"BUDGET"}`
	for _, input := range []string{complete, pending} {
		if _, err := DecodePaginationJSON([]byte(input), 8192); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{
		strings.Replace(complete, `"truncated":false`, `"truncated":true`, 1),
		strings.Replace(complete, `"enumeration_complete":true`, `"enumeration_complete":false`, 1),
		strings.Replace(pending, `"truncated":true`, `"truncated":false`, 1),
		strings.Replace(complete, `"returned_count":3`, `"returned_count":-1`, 1),
		strings.Replace(complete, `"returned_count":3`, `"returned_count":3.0`, 1),
		strings.Replace(complete, `,"limit_reason":null`, "", 1),
		strings.Replace(complete, `"next_cursor":null`, `"next_cursor":""`, 1),
	} {
		if _, err := DecodePaginationJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
