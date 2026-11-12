package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenameRequestDiscriminatesInitialAndContinuation(t *testing.T) {
	hash := strings.Repeat("a", 64)
	selector := fmt.Sprintf(`{"kind":"source_position","path":"a.go","content_hash":"%s","byte_offset":4}`, hash)
	req, err := DecodeRenameRequestJSON([]byte(`{"selector":`+selector+`,"new_name":"newName"}`), 8192)
	if err != nil {
		t.Fatal(err)
	}
	initial, ok := req.(*RenameInitialRequest)
	if !ok || !initial.PreserveContracts {
		t.Fatal("default preserve_contracts not true")
	}
	req, err = DecodeRenameRequestJSON([]byte(`{"result_token":"r","cursor":"c"}`), 8192)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := req.(*RenameContinueRequest); !ok {
		t.Fatal("wrong continuation type")
	}
	for _, bad := range []string{`{}`, `{"selector":` + selector + `,"new_name":""}`, `{"result_token":"r","cursor":"c","new_name":"x"}`, `{"selector":` + selector + `,"new_name":"x","result_token":"r"}`, `{"result_token":"r"}`} {
		if _, err := DecodeRenameRequestJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
