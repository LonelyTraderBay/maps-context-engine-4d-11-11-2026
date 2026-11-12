package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestSourceReadAndExactContent(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	source := []byte("a😀\n")
	digest := fmt.Sprintf("%x", sha256.Sum256(source))
	ref := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s","path":"a.go","content_hash":"%s","range":{"start_byte":1,"end_byte":5}}`, id, h, id, digest)
	req, err := DecodeSourceReadRequestJSON([]byte(`{"source_ref":`+ref+`}`), 16384)
	if err != nil || req.MaxOutputBytes != nil {
		t.Fatalf("%+v %v", req, err)
	}
	data, err := DecodeSourceDataJSON([]byte(`{"operation":"source_read","source_ref":`+ref+`,"content":"😀"}`), 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err := data.ValidateCapturedContent(source); err != nil {
		t.Fatal(err)
	}
	data.Content = "a"
	if err := data.ValidateCapturedContent(source); err == nil {
		t.Fatal("accepted truncated source")
	}
	if _, err := DecodeSourceReadRequestJSON([]byte(`{"source_ref":`+ref+`,"max_output_bytes":131073}`), 16384); err == nil {
		t.Fatal("accepted oversized output budget")
	}
}
