package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestPreviewStatusSchemaNeedsSemanticChecks(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	schema.Ref = "#/$defs/PreviewStatusData"
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	good := fmt.Sprintf(`{"operation":"preview_status","session_id":"%s","revision":1,"authorization_epoch":0,"document_epoch":1,"hmr_epoch":0,"state":"bound","mode_state":"inspect","binding_id":"%s","permission_profile_id":"profile","permission_version":1,"permission_status":"approved","expires_at":"2026-11-11T06:00:00Z","server_time":"2026-11-11T05:00:00Z","interaction_allowed":true}`, id, hash)
	for _, tc := range []struct {
		raw      string
		semantic bool
	}{{good, true}, {strings.Replace(good, `"permission_status":"approved"`, `"permission_status":"revoked"`, 1), false}, {strings.Replace(good, `"server_time":"2026-11-11T05:00:00Z"`, `"server_time":"bad-time"`, 1), false}} {
		var v any
		if err := json.Unmarshal([]byte(tc.raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(v); err != nil {
			t.Fatal(err)
		}
		_, err := model.DecodePreviewStatusDataJSON([]byte(tc.raw), 16384)
		if (err == nil) != tc.semantic {
			t.Fatalf("semantic=%v err=%v", tc.semantic, err)
		}
	}
}
