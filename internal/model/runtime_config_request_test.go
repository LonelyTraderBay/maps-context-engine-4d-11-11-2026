package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeConfigAdminRequestPreservesCASAndSentinel(t *testing.T) {
	raw := `{"action":"set","expected_digest":null,"config":{"schema_version":1,"core":{"max_parse_workers":0}}}`
	r, err := DecodeRuntimeConfigRequestJSON([]byte(raw), 8192, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ExpectedDigestSet || r.ExpectedDigest != nil || r.Config == nil || r.Config.Core == nil || r.Config.Core.MaxParseWorkers == nil || *r.Config.Core.MaxParseWorkers != 0 || r.Effective == nil || r.Effective.Core.MaxParseWorkers != 4 {
		t.Fatalf("lost sentinel or CAS: %+v", r)
	}
	encoded, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(encoded), `"expected_digest":null`) || !strings.Contains(string(encoded), `"max_parse_workers":0`) {
		t.Fatalf("lost wire semantics: %s %v", encoded, err)
	}
	for _, bad := range []string{`{"action":"set","config":{"schema_version":1}}`, `{"action":"validate"}`, `{"action":"show","config":{"schema_version":1}}`, `{"action":"set","expected_digest":"bad","config":{"schema_version":1}}`} {
		if _, err := DecodeRuntimeConfigRequestJSON([]byte(bad), 8192, 8); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if _, err := DecodeRuntimeConfigRequestJSON([]byte(`{"action":"show"}`), 8192, 0); err != nil {
		t.Fatal(err)
	}
}
