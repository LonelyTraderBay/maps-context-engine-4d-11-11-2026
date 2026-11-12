package model

import (
	"strings"
	"testing"
)

func TestWireJSONValidationAllowsDisplayFractionsAndRejectsAmbiguity(t *testing.T) {
	for _, raw := range []string{`{"x":1.5,"y":-2e-3}`, `{"x":1.0}`, `{"s":"\ud83d\ude00"}`} {
		if err := ValidateWireJSON([]byte(raw), 8192); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"x":1,"\u0078":2}`, `{"s":"\ud800"}`, `{"n":1e309}`, `{"n":1e-9999}`, `{"a":1} true`, strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)} {
		if err := ValidateWireJSON([]byte(raw), 8192); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := ValidateWireJSON([]byte(`{"x":1}`), 1); err == nil {
		t.Fatal("ignored byte budget")
	}
}
