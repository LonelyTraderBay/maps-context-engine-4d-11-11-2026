package compose

import (
	"strings"
	"testing"
)

func TestDeclarationRevisionDigest(t *testing.T) {
	// Independently calculated from MCE-C14N1 bytes using Python hashlib.
	vectors := []struct{ authority, revision, want string }{
		{"fixture-registry", "revision-1", "7fb03f789e6fcd15b3f8c09397660b02402cd0d692c56140e4d801ec6f9c6315"},
		{"quyền-hạn", "phiên-bản-1", "806c4e9d797c9a91f31f0351f4a0782a06e893f1d764e53f5aa78e862b375879"},
	}
	for _, v := range vectors {
		got, err := DeclarationRevisionDigest(v.authority, v.revision)
		if err != nil || got != v.want {
			t.Fatalf("digest %q/%q = %q, %v", v.authority, v.revision, got, err)
		}
	}
	first, _ := DeclarationRevisionDigest("fixture-registry", "revision-1")
	otherAuthority, _ := DeclarationRevisionDigest("other-registry", "revision-1")
	otherRevision, _ := DeclarationRevisionDigest("fixture-registry", "revision-2")
	if first == otherAuthority || first == otherRevision || otherAuthority == otherRevision {
		t.Fatal("different authority/revision pairs shared a digest")
	}
	validBoundary := strings.Repeat("a", 768)
	if _, err := DeclarationRevisionDigest(validBoundary, validBoundary); err != nil {
		t.Fatalf("valid boundary rejected: %v", err)
	}
	invalid := []struct{ authority, revision string }{
		{"", "revision"}, {"authority", ""},
		{strings.Repeat("a", 769), "revision"}, {"authority", strings.Repeat("b", 769)},
		{string([]byte{0xff}), "revision"}, {"authority", string([]byte{0xff})},
	}
	for _, v := range invalid {
		if got, err := DeclarationRevisionDigest(v.authority, v.revision); err == nil || got != "" {
			t.Fatalf("invalid input accepted: %q/%q", v.authority, v.revision)
		}
	}
}
