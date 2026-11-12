package model

import "testing"

func TestExpandAndValidateRequestContracts(t *testing.T) {
	expand, err := DecodeExpandRequestJSON([]byte(`{"pack_token":"p","next_cursor":"c"}`), 8192)
	if err != nil || expand.MaxOutputBytes != nil {
		t.Fatalf("%+v %v", expand, err)
	}
	for _, bad := range []string{`{"pack_token":"p"}`, `{"pack_token":"","next_cursor":"c"}`, `{"pack_token":"p","next_cursor":"c","max_output_bytes":4095}`, `{"pack_token":"p","next_cursor":"c","unexpected":1}`} {
		if _, err := DecodeExpandRequestJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	for _, good := range []string{`{"pack_token":"p"}`, `{"result_token":"r","verify":"workspace"}`} {
		if _, err := DecodeValidateRequestJSON([]byte(good), 8192); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{`{}`, `{"pack_token":"p","result_token":"r"}`, `{"result_token":"r","verify":"none"}`, `{"result_token":null}`} {
		if _, err := DecodeValidateRequestJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
