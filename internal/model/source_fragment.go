package model

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
)

// SourceFragmentID checks that the fragment makes byte-aligned progress within
// its parent before applying the CT-05 source-fragment-v1 identity recipe.
func SourceFragmentID(parent SourceRef, fragment ByteRange, source []byte, maxBytes int) (string, error) {
	if err := parent.ValidateCapturedBytes(source); err != nil {
		return "", err
	}
	if fragment.StartByte < parent.Range.StartByte || fragment.EndByte > parent.Range.EndByte || fragment.StartByte >= fragment.EndByte {
		return "", fmt.Errorf("invalid fragment range")
	}
	boundary := func(i int64) bool { return i == int64(len(source)) || source[int(i)]&0xc0 != 0x80 }
	if !boundary(fragment.StartByte) || !boundary(fragment.EndByte) {
		return "", fmt.Errorf("fragment splits UTF-8")
	}
	return sourceFragmentDigest(parent, fragment, maxBytes)
}

// Caller must first validate the source and the fragment bounds.
func sourceFragmentDigest(parent SourceRef, fragment ByteRange, maxBytes int) (string, error) {
	value := struct {
		ParentRef SourceRef `json:"parent_ref"`
		Range     ByteRange `json:"range"`
	}{parent, fragment}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("source-fragment-v1", canonical, maxBytes)
	return digest, err
}

// ValidateSourceFragment checks exact bytes and identity, not source policy or
// continuity between multiple pages.
func ValidateSourceFragment(parent, fragment SourceRef, content, itemID string, source []byte, maxBytes int) error {
	if parent.WorktreeID != fragment.WorktreeID || parent.ProfileID != fragment.ProfileID || parent.GenerationID != fragment.GenerationID || parent.Path != fragment.Path || parent.ContentHash != fragment.ContentHash || !lowerHex(itemID, 64) {
		return fmt.Errorf("fragment parent mismatch")
	}
	digest, err := SourceFragmentID(parent, fragment.Range, source, maxBytes)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(digest), []byte(itemID)) != 1 {
		return fmt.Errorf("fragment identity mismatch")
	}
	if !bytes.Equal([]byte(content), source[int(fragment.Range.StartByte):int(fragment.Range.EndByte)]) {
		return fmt.Errorf("fragment content mismatch")
	}
	return nil
}
