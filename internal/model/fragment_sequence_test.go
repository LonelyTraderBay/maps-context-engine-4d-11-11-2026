package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestCompleteFragmentSequenceUTF8CRLF(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	source := []byte("ລາວ😀\r\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(source))
	p := SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: hash, Range: ByteRange{StartByte: 0, EndByte: int64(len(source))}}
	ranges := []ByteRange{{0, 9}, {9, 13}, {13, 15}}
	items := make([]EvidenceItem, 0, len(ranges))
	for _, r := range ranges {
		c := p
		c.Range = r
		id, err := SourceFragmentID(p, r, source, 16384)
		if err != nil {
			t.Fatal(err)
		}
		content := string(source[r.StartByte:r.EndByte])
		items = append(items, EvidenceItem{ItemID: id, Kind: "source_range_fragment", SourceRefs: []SourceRef{c}, Content: &content, FragmentOf: &p})
	}
	if err := ValidateCompleteFragmentSequence(p, items, source, 16384); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCompleteFragmentSequence(p, items[:2], source, 16384); err == nil {
		t.Fatal("accepted incomplete sequence")
	}
	bad := append([]EvidenceItem{}, items...)
	bad[0].ItemID = strings.Repeat("0", 64)
	if err := ValidateCompleteFragmentSequence(p, bad, source, 16384); err == nil {
		t.Fatal("accepted wrong fragment digest")
	}
	bad = append([]EvidenceItem{}, items...)
	text := "x"
	bad[0].Content = &text
	if err := ValidateCompleteFragmentSequence(p, bad, source, 16384); err == nil {
		t.Fatal("accepted wrong fragment bytes")
	}
	if err := ValidateCompleteFragmentSequence(p, items, source, 0); err == nil {
		t.Fatal("accepted zero budget")
	}
	items[0], items[1] = items[1], items[0]
	if err := ValidateCompleteFragmentSequence(p, items, source, 16384); err == nil {
		t.Fatal("accepted reordered fragments")
	}
}
