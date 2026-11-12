package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestProviderBatchDoneAndCancel(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	batch := fmt.Sprintf(`{"provider_protocol":1,"type":"analyze_batch","job_id":"%s","sequence":1,"packages":[],"symbols":[],"relations":[],"diagnostics":[]}`, id)
	if msg, err := DecodeProviderMessageJSON([]byte(batch), 16384); err != nil {
		t.Fatal(err)
	} else if _, ok := msg.(*ProviderBatch); !ok {
		t.Fatal("wrong provider variant")
	}
	if _, err := DecodeProviderBatchJSON([]byte(batch), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(batch, `"sequence":1`, `"sequence":0`, 1), strings.Replace(batch, `"packages":[]`, `"packages":null`, 1), strings.Replace(batch, `"type":"analyze_batch"`, `"type":"other"`, 1)} {
		if _, err := DecodeProviderBatchJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	done := fmt.Sprintf(`{"provider_protocol":1,"type":"analyze_done","job_id":"%s","sequence":2,"status":"partial","input_digest":"%s","capabilities":[],"gaps":[],"error":null}`, id, h)
	d, err := DecodeProviderDoneJSON([]byte(done), 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.VerifyInputDigest(h); err != nil {
		t.Fatal(err)
	}
	if err := d.VerifyInputDigest(strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted different captured input")
	}
	cancel := fmt.Sprintf(`{"provider_protocol":1,"type":"cancel","job_id":"%s"}`, id)
	if _, err := DecodeProviderCancelJSON([]byte(cancel), 8192); err != nil {
		t.Fatal(err)
	}
}
