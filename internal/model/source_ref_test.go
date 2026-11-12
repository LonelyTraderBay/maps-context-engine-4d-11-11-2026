package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestSourceRefCapturedBytes(t *testing.T) {
	source := []byte("a😀\r\nລາວ")
	sum := sha256.Sum256(source)
	hash := hex.EncodeToString(sum[:])
	good := fmt.Sprintf(`{"worktree_id":"01234567-89ab-cdef-0123-456789abcdef","profile_id":"%s","generation_id":"abcdef01-2345-6789-abcd-ef0123456789","path":"src/ລາວ.go","content_hash":"%s","range":{"start_byte":1,"end_byte":5}}`, strings.Repeat("a", 64), hash)
	ref, err := DecodeSourceRefJSON([]byte(good), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := ref.ValidateCapturedBytes(source); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown":       strings.Replace(good, `"path":`, `"extra":0,"path":`, 1),
		"missing":       strings.Replace(good, `"path":"src/ລາວ.go",`, ``, 1),
		"duplicate":     strings.Replace(good, `"path":`, `"path":"elsewhere","path":`, 1),
		"float":         strings.Replace(good, `"start_byte":1`, `"start_byte":1.0`, 1),
		"null range":    strings.Replace(good, `{"start_byte":1,"end_byte":5}`, `null`, 1),
		"unknown range": strings.Replace(good, `"start_byte":1`, `"extra":0,"start_byte":1`, 1),
		"upper uuid":    strings.Replace(good, `01234567-89ab-cdef`, `01234567-89AB-cdef`, 1),
		"upper hash":    strings.Replace(good, hash, strings.ToUpper(hash), 1),
	}
	for _, path := range []string{".", "../x", "a//b", "a/", "/x", "C:/x", `a\\b`, "a:b", `a\u0000b`} {
		cases["path "+path] = strings.Replace(good, "src/ລາວ.go", path, 1)
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSourceRefJSON([]byte(input), 4096); err == nil {
				t.Fatal("accepted invalid source ref")
			}
		})
	}
	for name, changed := range map[string]SourceRef{
		"inside rune":  func() SourceRef { x := ref; x.Range.StartByte = 2; return x }(),
		"out of range": func() SourceRef { x := ref; x.Range.EndByte = 100; return x }(),
		"reversed":     func() SourceRef { x := ref; x.Range.StartByte = 6; return x }(),
		"wrong hash":   func() SourceRef { x := ref; x.ContentHash = strings.Repeat("0", 64); return x }(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := changed.ValidateCapturedBytes(source); err == nil {
				t.Fatal("accepted invalid capture")
			}
		})
	}
	if err := ref.ValidateCapturedBytes([]byte{0xff}); err == nil {
		t.Fatal("accepted invalid UTF-8 capture")
	}
}
