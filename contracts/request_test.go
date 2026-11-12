package contracts

import (
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"testing"
)

func TestRegisteredRequestDecoderAndTransport(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.DecodeRequest(MCP, "mce4d_workspace_status", []byte(`{}`), 4096, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(model.StatusRequest); !ok {
		t.Fatal("wrong request type")
	}
	if v, err := r.DecodeRequest(HTTP, "mce4d_workspace_status", []byte(`{}`), 4096, 4); err == nil || v != nil {
		t.Fatal("wrong transport accepted")
	}
	if v, err := r.DecodeRequest(MCP, "mce4d_locate", []byte(`{"unknown":true}`), 4096, 4); err == nil || v != nil {
		t.Fatal("invalid request accepted")
	}
}
func TestEveryRegisteredRequestHasDecoder(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range r.Operations() {
		_, err := model.DecodeRequestJSON(op.Request, []byte(`null`), 4096, 4)
		if err == nil || err.Error() == "unknown request schema" {
			t.Fatalf("%s: %v", op.Name, err)
		}
	}
}
