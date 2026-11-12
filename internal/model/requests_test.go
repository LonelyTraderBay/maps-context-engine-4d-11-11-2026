package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestLocateRequestDefaultsAndXOR(t *testing.T) {
	id := strings.Repeat("a", 64)
	input := `{"query":"account"}`
	req, err := DecodeLocateRequestJSON([]byte(input), 16384)
	if err != nil || req.Mode != "symbol" || req.Limit != 20 {
		t.Fatalf("%+v %v", req, err)
	}
	pos := fmt.Sprintf(`{"kind":"source_position","path":"a.go","content_hash":"%s","byte_offset":1}`, id)
	if _, err := DecodeLocateRequestJSON([]byte(`{"position":`+pos+`}`), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{}`, `{"query":"x","position":` + pos + `}`, `{"query":null}`, `{"query":"x","mode":"other"}`,
		`{"query":"x","limit":0}`, `{"query":"x","limit":101}`, `{"query":"x","extra":1}`,
		`{"query":"x","kinds":[null]}`, `{"position":` + strings.Replace(pos, "a.go", "../a.go", 1) + `}`,
		`{"position":` + strings.Replace(pos, `"byte_offset":1`, `"byte_offset":-1`, 1) + `}`,
	} {
		if _, err := DecodeLocateRequestJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
func TestStatusRequestVerifyClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"verify":"none"}`, `{"verify":"workspace"}`} {
		if _, err := DecodeStatusRequestJSON([]byte(raw), 8192); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{"verify":"returned_files"}`, `{"verify":null}`, `{"extra":1}`} {
		if _, err := DecodeStatusRequestJSON([]byte(raw), 8192); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
