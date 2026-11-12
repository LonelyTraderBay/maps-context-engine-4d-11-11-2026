package model

import (
	"strings"
	"testing"
)

func TestPortableRelativePathAndModuleRoot(t *testing.T) {
	for _, path := range []string{"src/ລາວ/😀.go", "A/b c.go", "é/e\u0301.go"} {
		if err := ValidateRelativePath(path, false); err != nil {
			t.Fatalf("%q: %v", path, err)
		}
	}
	if err := ValidateRelativePath(".", true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", ".", "../a", "a/./b", "a//b", "a/", "/a", `C:\a`, `a\b`, "a:b", "a\x00b", "a\x7fb", string([]byte{0xff}), strings.Repeat("x", 4097)} {
		if err := ValidateRelativePath(path, false); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if err := ValidateRelativePath("../a", true); err == nil {
		t.Fatal("allowed traversal for module root")
	}
}
