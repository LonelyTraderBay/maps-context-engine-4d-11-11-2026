package model

import "testing"

func TestPreviewStatusRequest(t *testing.T) {
	for _, tc := range []struct {
		s     string
		valid bool
	}{
		{`{"session_id":"01234567-89ab-cdef-0123-456789abcdef"}`, true},
		{`{"session_id":"bad"}`, false}, {`{"session_id":null}`, false},
		{`{"session_id":"01234567-89ab-cdef-0123-456789abcdef","refresh":true}`, false},
	} {
		_, err := DecodePreviewStatusRequestJSON([]byte(tc.s), 1024)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.s, err)
		}
	}
}
