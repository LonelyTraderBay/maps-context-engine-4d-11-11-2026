package model

import (
	"strings"
	"testing"
)

func TestBootstrapShapeDoesNotLeakValues(t *testing.T) {
	if _, err := DecodeBootstrapRequestJSON([]byte(`{"code":"test-token"}`), 1024); err != nil {
		t.Fatal(err)
	}
	raw := `{"operation":"bootstrap","bearer":"test-token","expires_at":"2026-11-11T00:00:00Z","capabilities":[]}`
	if _, err := DecodeBootstrapDataJSON([]byte(raw), 1024); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"capabilities":[]`, `"capabilities":null`, 1), strings.Replace(raw, `2026-11-11T00:00:00Z`, `invalid`, 1), strings.Replace(raw, `"bootstrap"`, `"unknown"`, 1)} {
		_, err := DecodeBootstrapDataJSON([]byte(bad), 1024)
		if err == nil || strings.Contains(err.Error(), "test-token") {
			t.Fatalf("unexpected error %v", err)
		}
	}
}
