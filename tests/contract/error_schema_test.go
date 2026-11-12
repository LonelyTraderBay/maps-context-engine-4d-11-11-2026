package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestErrorEnumerationsMatchSchema(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	props := s.Defs["Error"].Properties
	for _, code := range props["code"].Enum {
		raw := fmt.Sprintf(`{"code":%q,"message":"x","retryable":false,"suggested_action":"none","details":[]}`, code)
		if _, err := model.DecodeErrorInfoJSON([]byte(raw), 8192); err != nil {
			t.Errorf("code %s: %v", code, err)
		}
	}
	for _, action := range props["suggested_action"].Enum {
		raw := fmt.Sprintf(`{"code":"INVALID_ARGUMENT","message":"x","retryable":false,"suggested_action":%q,"details":[]}`, action)
		if _, err := model.DecodeErrorInfoJSON([]byte(raw), 8192); err != nil {
			t.Errorf("action %s: %v", action, err)
		}
	}
}
