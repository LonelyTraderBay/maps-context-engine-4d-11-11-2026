package model

import (
	"fmt"
	"testing"
)

func TestRecordListAndReadRequestShapes(t *testing.T) {
	list, err := DecodeRecordListRequestJSON([]byte(`{}`), 8192)
	if err != nil || list.Limit == nil || *list.Limit != 20 {
		t.Fatalf("%+v %v", list, err)
	}
	for _, bad := range []string{`{"status":"invented"}`, `{"limit":101}`, `{"cursor":""}`, `{"extra":1}`} {
		if _, err := DecodeRecordListRequestJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	raw := fmt.Sprintf(`{"record_id":"%s","version":2}`, id)
	if _, err := DecodeRecordReadRequestJSON([]byte(raw), 8192); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, `{"record_id":"bad"}`, fmt.Sprintf(`{"record_id":"%s","version":0}`, id)} {
		if _, err := DecodeRecordReadRequestJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
