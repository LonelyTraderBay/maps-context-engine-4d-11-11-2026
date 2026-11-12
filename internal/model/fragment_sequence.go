package model

import (
	"bytes"
	"crypto/subtle"
	"fmt"
)

// ValidateCompleteFragmentSequence checks exact, contiguous coverage of a parent
// against captured bytes. It does not fetch pages or authenticate cursors.
func ValidateCompleteFragmentSequence(parent SourceRef, items []EvidenceItem, source []byte, maxBytes int) error {
	if maxBytes <= 0 || items == nil || len(items) > maxBytes {
		return fmt.Errorf("invalid fragment sequence")
	}
	if err := parent.ValidateCapturedBytes(source); err != nil {
		return err
	}
	offset := parent.Range.StartByte
	boundary := func(i int64) bool { return i == int64(len(source)) || source[int(i)]&0xc0 != 0x80 }
	for _, item := range items {
		if item.Kind != "source_range_fragment" || item.FragmentOf == nil || *item.FragmentOf != parent || len(item.SourceRefs) != 1 || item.Content == nil || !lowerHex(item.ItemID, 64) {
			return fmt.Errorf("invalid fragment")
		}
		child := item.SourceRefs[0]
		r := child.Range
		if child.WorktreeID != parent.WorktreeID || child.ProfileID != parent.ProfileID || child.GenerationID != parent.GenerationID || child.Path != parent.Path || child.ContentHash != parent.ContentHash || r.StartByte != offset || r.StartByte >= r.EndByte || r.EndByte > parent.Range.EndByte || !boundary(r.StartByte) || !boundary(r.EndByte) {
			return fmt.Errorf("fragment sequence gap or parent mismatch")
		}
		digest, err := sourceFragmentDigest(parent, r, maxBytes)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(digest), []byte(item.ItemID)) != 1 || !bytes.Equal([]byte(*item.Content), source[int(r.StartByte):int(r.EndByte)]) {
			return fmt.Errorf("fragment identity or content mismatch")
		}
		offset = r.EndByte
	}
	if offset != parent.Range.EndByte {
		return fmt.Errorf("fragment sequence incomplete")
	}
	return nil
}
