package model

import "testing"

func TestPreviewPathPrefixUsesSegmentBoundary(t *testing.T) {
	for _, tc := range []struct {
		prefix, path string
		valid        bool
	}{{"/app", "/app", true}, {"/app", "/app/page", true}, {"/app", "/application", false}, {"/app/", "/app/page", true}, {"/app/", "/app", false}, {"/", "/anything", true}, {"/app", "/app?secret", false}, {"/app", "/app\\admin", false}, {"app", "/app", false}} {
		err := VerifyCanonicalPreviewPathPrefix(tc.prefix, tc.path)
		if (err == nil) != tc.valid {
			t.Fatalf("%q %q: %v", tc.prefix, tc.path, err)
		}
	}
}
