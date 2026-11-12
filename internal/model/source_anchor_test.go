package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestSourceAnchorDigestBindsCapturedBytes(t *testing.T) {
	source := []byte("a😀\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(source))
	anchor := SourceAnchorIdentity{Path: "web/a.tsx", ContentHash: hash, Kind: "element_site", Range: ByteRange{StartByte: 1, EndByte: 5}, ProviderDigest: strings.Repeat("b", 64)}
	digest, err := SourceAnchorDigest(anchor, source, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "089c6815e57fe9c2ed3484a54d3e9aabedf09a731e8ec17adca3f6de65a24717" {
		t.Fatalf("does not match Python reference: %s", digest)
	}
	alternate := anchor
	alternate.ProviderDigest = strings.Repeat("c", 64)
	other, err := SourceAnchorDigest(alternate, source, 8192)
	if err != nil || digest == other {
		t.Fatal("provider not bound")
	}
	for _, bad := range []SourceAnchorIdentity{func() SourceAnchorIdentity { x := anchor; x.Range.StartByte = 2; return x }(), func() SourceAnchorIdentity { x := anchor; x.ContentHash = strings.Repeat("a", 64); return x }(), func() SourceAnchorIdentity { x := anchor; x.Kind = "unknown"; return x }()} {
		if _, err := SourceAnchorDigest(bad, source, 8192); err == nil {
			t.Fatal("accepted invalid anchor")
		}
	}
}
