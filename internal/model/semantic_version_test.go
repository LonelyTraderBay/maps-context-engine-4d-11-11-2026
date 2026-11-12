package model

import (
	"strings"
	"testing"
)

func TestResolverSemanticVersionIsNotBranchName(t *testing.T) {
	h := strings.Repeat("a", 64)
	for _, tc := range []struct {
		version string
		valid   bool
	}{{"1.2.3", true}, {"0.1.0-alpha.1+build.001", true}, {"1.0.0+001", true}, {"main", false}, {"v1.2.3", false}, {"01.2.3", false}, {"1.2.3-alpha.01", false}, {"1.2.3+", false}, {"1.2.3-rc..1", false}, {"1.2.3-β", false}} {
		_, err := ResolverDigest(ResolverIdentity{ImplementationArtifactHash: h, SemanticVersion: tc.version, OptionsDigest: h}, 16384)
		if (err == nil) != tc.valid {
			t.Fatalf("%q: %v", tc.version, err)
		}
	}
}
