package model

import "testing"

func TestResponseDataInvalidReturnsNoValue(t *testing.T) {
	v, err := DecodeResponseDataJSON([]byte(`{"operation":"records_list","records":null}`), 4096, 4)
	if err == nil || v != nil {
		t.Fatalf("invalid response leaked value: %v %v", v, err)
	}
}
