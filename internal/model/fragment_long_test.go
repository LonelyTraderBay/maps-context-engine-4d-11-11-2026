package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLongSingleLineFragmentsCoverExactBytes(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	source := []byte(strings.Repeat("ລ😀", 2000) + "\r\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(source))
	parent := SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: hash, Range: ByteRange{StartByte: 0, EndByte: int64(len(source))}}
	items := []EvidenceItem{}
	var combined strings.Builder
	for start := 0; start < len(source); {
		end := start + 4096
		if end > len(source) {
			end = len(source)
		}
		for end < len(source) && !utf8.RuneStart(source[end]) {
			end--
		}
		if end == start {
			t.Fatal("no byte progress")
		}
		r := ByteRange{StartByte: int64(start), EndByte: int64(end)}
		fragment := parent
		fragment.Range = r
		itemID, err := SourceFragmentID(parent, r, source, 16384)
		if err != nil {
			t.Fatal(err)
		}
		content := string(source[start:end])
		combined.WriteString(content)
		items = append(items, EvidenceItem{ItemID: itemID, Kind: "source_range_fragment", SourceRefs: []SourceRef{fragment}, Content: &content, FragmentOf: &parent})
		start = end
	}
	if len(items) < 2 || combined.String() != string(source) {
		t.Fatal("incorrect fragment coverage")
	}
	if err := ValidateCompleteFragmentSequence(parent, items, source, 16384); err != nil {
		t.Fatal(err)
	}
}
