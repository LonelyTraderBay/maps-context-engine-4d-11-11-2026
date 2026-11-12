package model

import (
	"errors"
	"strings"
	"testing"
)

func TestConfigCASExpectedAbsentAndChangedHead(t *testing.T) {
	r, err := DecodeRuntimeConfigRequestJSON([]byte(`{"action":"set","config":{"schema_version":1},"expected_digest":null}`), 16384, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckExpectedConfigDigest(r, nil); err != nil {
		t.Fatal(err)
	}
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	if err = CheckExpectedConfigDigest(r, &a); !errors.Is(err, ErrConfigConflict) {
		t.Fatal("expected absent must reject existing")
	}
	r.ExpectedDigest = &a
	if err = CheckExpectedConfigDigest(r, &a); err != nil {
		t.Fatal(err)
	}
	if err = CheckExpectedConfigDigest(r, &b); !errors.Is(err, ErrConfigConflict) {
		t.Fatal("stale setter accepted")
	}
	r.ExpectedDigestSet = false
	if err = CheckExpectedConfigDigest(r, &a); err == nil {
		t.Fatal("accepted absent CAS field")
	}
}
