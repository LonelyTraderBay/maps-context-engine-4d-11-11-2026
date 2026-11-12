package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestSourceContentLengthMustMatchDeclaredBytes(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	source := []byte("a😀\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(source))
	ref := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s","path":"a.go","content_hash":"%s","range":{"start_byte":1,"end_byte":5}}`, id, h, id, hash)
	raw := `{"operation":"source_read","source_ref":` + ref + `,"content":"a"}`
	if _, err := DecodeSourceDataJSON([]byte(raw), 16384); err == nil {
		t.Fatal("accepted byte length mismatch")
	}
}
