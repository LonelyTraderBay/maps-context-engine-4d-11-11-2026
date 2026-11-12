package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode/utf8"
)

type SourceAnchorIdentity struct {
	Path           string
	ContentHash    string
	Kind           string
	Range          ByteRange
	ProviderDigest string
}

// SourceAnchorDigest binds a canonical anchor to the supplied original bytes.
// Filesystem containment, policy and generation membership remain caller duties.
func SourceAnchorDigest(a SourceAnchorIdentity, source []byte, maxBytes int) (string, error) {
	if !validSourcePath(a.Path) || !lowerHex(a.ContentHash, 64) || !lowerHex(a.ProviderDigest, 64) || !utf8.Valid(source) || a.Range.StartByte < 0 || a.Range.EndByte < a.Range.StartByte || a.Range.EndByte > int64(len(source)) || a.Range.EndByte > maxExactInteger {
		return "", fmt.Errorf("invalid source anchor")
	}
	switch a.Kind {
	case "element_site", "component_definition", "component_render_site", "event_binding_site":
	default:
		return "", fmt.Errorf("invalid source anchor kind")
	}
	boundary := func(i int64) bool { return i == int64(len(source)) || source[int(i)]&0xc0 != 0x80 }
	if !boundary(a.Range.StartByte) || !boundary(a.Range.EndByte) {
		return "", fmt.Errorf("anchor splits UTF-8")
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != a.ContentHash {
		return "", fmt.Errorf("anchor content mismatch")
	}
	return encodeSourceAnchor(a, maxBytes)
}

// encodeSourceAnchor requires prior shape and captured-source validation.
func encodeSourceAnchor(a SourceAnchorIdentity, maxBytes int) (string, error) {
	values := []struct{ key, value string }{{"path", a.Path}, {"content_hash", a.ContentHash}, {"kind", a.Kind}, {"provider_digest", a.ProviderDigest}}
	fields := make([]CanonicalField, 0, 6)
	for _, entry := range values {
		v, err := CanonicalString(entry.value)
		if err != nil {
			return "", err
		}
		fields = append(fields, CanonicalField{Key: entry.key, Value: v})
	}
	start, err := CanonicalInt(a.Range.StartByte)
	if err != nil {
		return "", err
	}
	end, err := CanonicalInt(a.Range.EndByte)
	if err != nil {
		return "", err
	}
	fields = append(fields, CanonicalField{Key: "start_byte", Value: start}, CanonicalField{Key: "end_byte", Value: end})
	value, err := CanonicalObject(fields)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("anchor-v1", value, maxBytes)
	return digest, err
}
