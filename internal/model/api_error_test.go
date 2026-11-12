package model

import "testing"

func TestErrorInfoClosedShape(t *testing.T) {
	good := `{"code":"INVALID_ARGUMENT","message":"invalid","retryable":false,"suggested_action":"fix_request","details":[]}`
	if _, err := DecodeErrorInfoJSON([]byte(good), 8192); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"code":"BAD","message":"invalid","retryable":false,"suggested_action":"none","details":[]}`, `{"code":"INVALID_ARGUMENT","message":"","retryable":false,"suggested_action":"none","details":[]}`, `{"code":"INVALID_ARGUMENT","message":"x","retryable":false,"suggested_action":"unknown","details":[]}`, `{"code":"INVALID_ARGUMENT","message":"x","retryable":false,"suggested_action":"none","details":null}`} {
		if _, err := DecodeErrorInfoJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
