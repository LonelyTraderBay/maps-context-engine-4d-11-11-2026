package model

import (
	"strings"
	"testing"
)

func TestReferenceDigestsAgainstPython(t *testing.T) {
	h := strings.Repeat("a", 64)
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ref := &SymbolRef{Kind: "symbol", GraphIdentity: GraphIdentity{WorktreeID: id, ProfileID: h, GenerationID: id}, SymbolID: "item"}
	got, err := GraphRefDigest(ref, 16384)
	if err != nil || got != "a1202c051a932e4addd2e80fa34574ef10eaadbf9de502a0aee51032c10ae0c6" {
		t.Fatalf("graph digest %s %v", got, err)
	}
	ref.SymbolID = ""
	if _, err := GraphRefDigest(ref, 16384); err == nil {
		t.Fatal("invalid graph ref accepted")
	}
	got, err = ResolverDigest(ResolverIdentity{ImplementationArtifactHash: h, SemanticVersion: "1.2.3", OptionsDigest: h}, 16384)
	if err != nil || got != "b83cf0c53d0b4d7669865231dda7ca72acde261e39fb9499babd55ba513de368" {
		t.Fatalf("resolver digest %s %v", got, err)
	}
	if _, err := ResolverDigest(ResolverIdentity{ImplementationArtifactHash: h, OptionsDigest: h}, 16384); err == nil {
		t.Fatal("missing resolver version accepted")
	}
}
