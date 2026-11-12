package model

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"unicode/utf8"
)

// VerifyBuildManifestCaptured checks a manifest against bytes supplied by the
// caller. It does not open files or establish authorization or freshness.
func VerifyBuildManifestCaptured(m BuildManifest, captured map[string][]byte, maxManifestBytes int, maxSourceBytes int64) error {
	if maxSourceBytes < 1 {
		return fmt.Errorf("invalid source budget")
	}
	if err := VerifyBuildMapDigest(m, maxManifestBytes); err != nil {
		return err
	}
	var total int64
	for _, source := range m.Sources {
		data, ok := captured[source.Path]
		if !ok || !utf8.Valid(data) {
			return fmt.Errorf("source missing or invalid UTF-8")
		}
		if int64(len(data)) > maxSourceBytes-total {
			return fmt.Errorf("source budget exceeded")
		}
		total += int64(len(data))
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != source.ContentHash {
			return fmt.Errorf("source hash mismatch")
		}
	}
	for _, anchor := range m.Anchors {
		data := captured[anchor.Path]
		r := anchor.Range
		if r.EndByte > int64(len(data)) {
			return fmt.Errorf("anchor range outside source")
		}
		boundary := func(i int64) bool { return i == int64(len(data)) || data[int(i)]&0xc0 != 0x80 }
		if !boundary(r.StartByte) || !boundary(r.EndByte) {
			return fmt.Errorf("anchor splits UTF-8")
		}
		identity := SourceAnchorIdentity{Path: anchor.Path, ContentHash: anchor.ContentHash, Kind: anchor.Kind, Range: r, ProviderDigest: m.ProviderDigest}
		digest, err := encodeSourceAnchor(identity, maxManifestBytes)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(digest), []byte(anchor.AnchorID)) != 1 {
			return fmt.Errorf("anchor identity mismatch")
		}
	}
	return nil
}
