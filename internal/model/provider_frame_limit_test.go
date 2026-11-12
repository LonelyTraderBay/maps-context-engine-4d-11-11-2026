package model

import (
	"bytes"
	"testing"
)

func TestProviderFrameHardLimitCannotBeRaised(t *testing.T) {
	frame := append([]byte(`{"provider_protocol":1,"type":"cancel","job_id":"01234567-89ab-cdef-0123-456789abcdef"}`), bytes.Repeat([]byte(" "), providerFrameHardLimit)...)
	if _, err := DecodeProviderMessageJSON(frame, len(frame)); err == nil {
		t.Fatal("accepted frame larger than hard cap")
	}
	if _, err := DecodeProviderCancelJSON(frame, len(frame)); err == nil {
		t.Fatal("direct decoder exceeded hard cap")
	}
	small := []byte(`{"provider_protocol":1,"type":"cancel","job_id":"01234567-89ab-cdef-0123-456789abcdef"}`)
	if _, err := DecodeProviderMessageJSON(small, 16); err == nil {
		t.Fatal("ignored caller lower cap")
	}
}
