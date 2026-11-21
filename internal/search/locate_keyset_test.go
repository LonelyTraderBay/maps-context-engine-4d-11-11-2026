package search

import (
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
)

func TestLocateKeyRoundTripPreservesBinaryProgress(t *testing.T) {
	values := []string{"a", "aa", "b", strings.Repeat("é", 192), strings.Repeat("😀", 192)}
	previous := contextpack.SignedPosition{LastKey: []string{}}
	for i, value := range values {
		parts, err := EncodeLocateKey(value)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeLocateKey(parts)
		if err != nil || decoded != value {
			t.Fatalf("round trip failed: %q %v", decoded, err)
		}
		next := contextpack.SignedPosition{NextItem: int64(i + 1), LastKey: parts}
		if !contextpack.CursorAdvances(previous, next) {
			t.Fatalf("no progress for %q", value)
		}
		previous = next
	}
	for _, parts := range [][]string{{"A0"}, {"f"}, {"61", "62"}, {strings.Repeat("f", 514)}} {
		if _, err := DecodeLocateKey(parts); err == nil {
			t.Fatalf("noncanonical key accepted: %v", parts)
		}
	}
}

func TestFileLocateKeyUsesCanonicalPathRecipe(t *testing.T) {
	value, err := FileLocateKey("src/é.go")
	if err != nil || value != "file-60138e981d474156c91010341a79817a0792c07bb346c3654fffb05f1a09ceff" {
		t.Fatalf("file key differs from Python reference: %q %v", value, err)
	}
	for _, invalid := range []string{"../secret", "/absolute", "src\\file.go", "src//file.go"} {
		if _, err := FileLocateKey(invalid); err == nil {
			t.Fatalf("invalid file path accepted: %q", invalid)
		}
	}
}
