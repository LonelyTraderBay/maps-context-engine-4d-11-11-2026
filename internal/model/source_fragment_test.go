package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestSourceFragmentBoundariesAndIdentity(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	source := []byte("a😀\r\nລາວ")
	digest := fmt.Sprintf("%x", sha256.Sum256(source))
	parent := SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: digest, Range: ByteRange{StartByte: 0, EndByte: int64(len(source))}}
	child := parent
	child.Range = ByteRange{StartByte: 1, EndByte: 5}
	itemID, err := SourceFragmentID(parent, child.Range, source, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if itemID != "37a757b7fcfa59ae822c4f48f2728520d680cea16b0ab1284e82ea404724eb11" {
		t.Fatalf("does not match Python reference: %s", itemID)
	}
	if err := ValidateSourceFragment(parent, child, "😀", itemID, source, 8192); err != nil {
		t.Fatal(err)
	}
	bad := child
	bad.Range.StartByte = 2
	if err := ValidateSourceFragment(parent, bad, "😀", itemID, source, 8192); err == nil {
		t.Fatal("accepted split UTF-8")
	}
	if err := ValidateSourceFragment(parent, child, "x", itemID, source, 8192); err == nil {
		t.Fatal("accepted truncated content")
	}
	if err := ValidateSourceFragment(parent, child, "😀", strings.Repeat("0", 64), source, 8192); err == nil {
		t.Fatal("accepted wrong item ID")
	}
	if _, err := SourceFragmentID(parent, ByteRange{StartByte: 1, EndByte: 1}, source, 8192); err == nil {
		t.Fatal("accepted empty progress")
	}
}
