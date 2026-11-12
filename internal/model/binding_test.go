package model

import (
	"strings"
	"testing"
)

func TestBindingIdentityRecipe(t *testing.T) {
	input := BindingIdentity{BuildMapID: strings.Repeat("c", 64), SourceGenerationID: "00000000-0000-4000-8000-000000000001", ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ResolverDigest: strings.Repeat("d", 64)}
	digest, err := input.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if want := "3b352d73b1c13cbcd0ca363aa3baa59b65db0a2d725733fd4613375606cc2b49"; digest != want {
		t.Fatalf("got %s want %s", digest, want)
	}
	for name, bad := range map[string]BindingIdentity{
		"generation": func() BindingIdentity { x := input; x.SourceGenerationID = "latest"; return x }(),
		"profile":    func() BindingIdentity { x := input; x.ProfileID = strings.Repeat("A", 64); return x }(),
		"resolver":   func() BindingIdentity { x := input; x.ResolverDigest = ""; return x }(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := bad.Digest(); err == nil {
				t.Fatal("accepted invalid identity")
			}
		})
	}
	other := input
	other.ResolverDigest = strings.Repeat("e", 64)
	changed, err := other.Digest()
	if err != nil || changed == digest {
		t.Fatal("resolver change must affect binding")
	}
}
