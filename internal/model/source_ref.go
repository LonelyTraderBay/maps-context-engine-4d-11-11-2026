package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ByteRange describes a half-open range of original captured bytes.
type ByteRange struct {
	StartByte int64 `json:"start_byte"`
	EndByte   int64 `json:"end_byte"`
}
type SourceRef struct {
	WorktreeID   string    `json:"worktree_id"`
	ProfileID    string    `json:"profile_id"`
	GenerationID string    `json:"generation_id"`
	Path         string    `json:"path"`
	ContentHash  string    `json:"content_hash"`
	Range        ByteRange `json:"range"`
}

// DecodeSourceRefJSON checks this closed wire shape and its portable identity fields.
// Captured membership and authorization still require the owning generation and policy.
func DecodeSourceRefJSON(data []byte, maxBytes int) (SourceRef, error) {
	var ref SourceRef
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return ref, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ref, err
	}
	if err := closedFields(fields, "worktree_id", "profile_id", "generation_id", "path", "content_hash", "range"); err != nil {
		return ref, err
	}
	var bounds map[string]json.RawMessage
	if err := json.Unmarshal(fields["range"], &bounds); err != nil {
		return ref, err
	}
	if err := closedFields(bounds, "start_byte", "end_byte"); err != nil {
		return ref, err
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		return SourceRef{}, err
	}
	if err := ref.validateIdentity(); err != nil {
		return SourceRef{}, err
	}
	return ref, nil
}
func closedFields(fields map[string]json.RawMessage, names ...string) error {
	if len(fields) != len(names) {
		return fmt.Errorf("invalid object fields")
	}
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("missing or null field %s", name)
		}
	}
	return nil
}
func lowerHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func uuid(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	return lowerHex(strings.ReplaceAll(value, "-", ""), 32)
}
func validSourcePath(path string) bool {
	if !utf8.ValidString(path) || utf8.RuneCountInString(path) > 4096 || strings.ContainsAny(path, `\:`) {
		return false
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
func (ref SourceRef) validateIdentity() error {
	if !uuid(ref.WorktreeID) || !uuid(ref.GenerationID) || !lowerHex(ref.ProfileID, 64) || !lowerHex(ref.ContentHash, 64) || !validSourcePath(ref.Path) {
		return fmt.Errorf("invalid source identity")
	}
	if ref.Range.StartByte < 0 || ref.Range.EndByte < ref.Range.StartByte || ref.Range.EndByte > 9007199254740991 {
		return fmt.Errorf("invalid byte range")
	}
	return nil
}

// ValidateCapturedBytes verifies byte range and raw content hash. It does not
// establish generation membership, authorization, or secure filesystem access.
func (ref SourceRef) ValidateCapturedBytes(source []byte) error {
	if err := ref.validateIdentity(); err != nil {
		return err
	}
	if !utf8.Valid(source) || ref.Range.EndByte > int64(len(source)) {
		return fmt.Errorf("invalid captured UTF-8 or range")
	}
	boundary := func(i int64) bool { return i == int64(len(source)) || source[int(i)]&0xc0 != 0x80 }
	if !boundary(ref.Range.StartByte) || !boundary(ref.Range.EndByte) {
		return fmt.Errorf("range splits UTF-8 encoding")
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != ref.ContentHash {
		return fmt.Errorf("captured content hash mismatch")
	}
	return nil
}
