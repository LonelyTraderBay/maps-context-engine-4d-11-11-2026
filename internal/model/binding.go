package model

import "fmt"

// BindingIdentity is the immutable input tuple for the CT-04 binding-v1 recipe.
// Possession of the digest is not authorization or proof of a stored binding.
type BindingIdentity struct {
	BuildMapID         string
	SourceGenerationID string
	ProfileID          string
	PolicyDigest       string
	ResolverDigest     string
}

func (b BindingIdentity) Digest() (string, error) {
	if !lowerHex(b.BuildMapID, 64) || !uuid(b.SourceGenerationID) || !lowerHex(b.ProfileID, 64) || !lowerHex(b.PolicyDigest, 64) || !lowerHex(b.ResolverDigest, 64) {
		return "", fmt.Errorf("invalid binding identity")
	}
	entries := []struct{ key, value string }{
		{"build_map_id", b.BuildMapID}, {"source_generation_id", b.SourceGenerationID}, {"profile_id", b.ProfileID}, {"policy_digest", b.PolicyDigest}, {"resolver_digest", b.ResolverDigest},
	}
	fields := make([]CanonicalField, 0, len(entries))
	for _, entry := range entries {
		value, err := CanonicalString(entry.value)
		if err != nil {
			return "", err
		}
		fields = append(fields, CanonicalField{Key: entry.key, Value: value})
	}
	value, err := CanonicalObject(fields)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("binding-v1", value, 4096)
	return digest, err
}
