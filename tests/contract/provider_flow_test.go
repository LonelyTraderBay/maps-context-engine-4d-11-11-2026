package contract

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestProviderFlowContractVersionSequenceAndInput(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	digest := strings.Repeat("a", 64)
	batch := fmt.Sprintf(`{"provider_protocol":1,"type":"analyze_batch","job_id":"%s","sequence":1,"packages":[],"symbols":[],"relations":[],"diagnostics":[]}`, id)
	for _, bad := range []string{strings.Replace(batch, `"provider_protocol":1,`, "", 1), strings.Replace(batch, `"provider_protocol":1`, `"provider_protocol":2`, 1)} {
		if _, err := model.DecodeProviderMessageJSON([]byte(bad), 16384); err == nil {
			t.Fatal("missing/unsupported protocol accepted")
		}
	}
	state, err := model.BeginProviderStream(id, digest)
	if err != nil {
		t.Fatal(err)
	}
	message, err := model.DecodeProviderMessageJSON([]byte(batch), 16384)
	if err != nil {
		t.Fatal(err)
	}
	state, err = model.AcceptProviderOutput(state, message, 16384)
	if err != nil {
		t.Fatal(err)
	}
	wrong := fmt.Sprintf(`{"provider_protocol":1,"type":"analyze_done","job_id":"%s","sequence":2,"status":"partial","input_digest":"%s","capabilities":[],"gaps":[],"error":null}`, id, strings.Repeat("b", 64))
	msg, err := model.DecodeProviderMessageJSON([]byte(wrong), 16384)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = model.AcceptProviderOutput(state, msg, 16384); err == nil {
		t.Fatal("different capture accepted")
	}
	if _, err = state.TerminalStatus(); err == nil {
		t.Fatal("stream without valid terminal accepted")
	}
	done := strings.Replace(wrong, strings.Repeat("b", 64), digest, 1)
	msg, err = model.DecodeProviderMessageJSON([]byte(done), 16384)
	if err != nil {
		t.Fatal(err)
	}
	state, err = model.AcceptProviderOutput(state, msg, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := state.TerminalStatus(); err != nil || status != "partial" {
		t.Fatalf("status %q %v", status, err)
	}
}
