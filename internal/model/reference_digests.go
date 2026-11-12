package model

import (
	"encoding/json"
	"fmt"
)

// GraphRefDigest hashes the complete, validated typed reference without labels.
func GraphRefDigest(ref GraphRef, maxBytes int) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("missing graph ref")
	}
	if _, err := ref.graphIdentity(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		return "", err
	}
	value, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("graph-ref-v1", value, maxBytes)
	return digest, err
}

type ResolverIdentity struct {
	ImplementationArtifactHash string `json:"implementation_artifact_hash"`
	SemanticVersion            string `json:"semantic_version"`
	OptionsDigest              string `json:"options_digest"`
}

// ResolverDigest binds supplied fingerprints; artifact bytes and adapter options
// must be independently verified by the owning implementation.
func ResolverDigest(r ResolverIdentity, maxBytes int) (string, error) {
	if !lowerHex(r.ImplementationArtifactHash, 64) || !lowerHex(r.OptionsDigest, 64) || len(r.SemanticVersion) > maxBytes || !validSemanticVersion(r.SemanticVersion) {
		return "", fmt.Errorf("invalid resolver identity")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	v, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("resolver-v1", v, maxBytes)
	return digest, err
}
