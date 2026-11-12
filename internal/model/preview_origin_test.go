package model

import "testing"

func TestPreviewOriginRequiresExactBrowserOrigin(t *testing.T) {
	for _, tc := range []struct {
		approved, observed string
		valid              bool
	}{{"http://localhost:3000", "http://localhost:3000", true}, {"http://localhost:3000", "http://127.0.0.1:3000", false}, {"http://localhost:80", "http://localhost", false}, {"http://user@localhost", "http://user@localhost", false}, {"http://localhost/path", "http://localhost/path", false}, {"http://localhost?x=secret", "http://localhost?x=secret", false}, {"javascript:alert(1)", "javascript:alert(1)", false}} {
		err := VerifyPreviewEventOrigin(tc.approved, tc.observed)
		if (err == nil) != tc.valid {
			t.Fatalf("%s %s: %v", tc.approved, tc.observed, err)
		}
	}
}
