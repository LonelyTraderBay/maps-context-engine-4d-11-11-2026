package model

import (
	"errors"
	"testing"
)

func TestEnabledKeysRejectUnknown(t *testing.T) {
	if err := RequireRegisteredKey("CALLS_DIRECT", []string{"CALLS_DIRECT", "REFERENCES"}); err != nil {
		t.Fatal(err)
	}
	if err := RequireRegisteredKey("INVENTED", []string{"CALLS_DIRECT"}); !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("expected unsupported: %v", err)
	}
	if err := RequireRegisteredKey("CALLS_DIRECT", []string{"CALLS_DIRECT", "CALLS_DIRECT"}); err == nil {
		t.Fatal("accepted duplicate registry")
	}
	if err := RequireRegisteredKey("CALLS_DIRECT", nil); err == nil {
		t.Fatal("accepted absent registry")
	}
}
