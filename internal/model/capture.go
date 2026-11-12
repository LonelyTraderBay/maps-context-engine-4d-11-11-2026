package model

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"sort"
)

type ManifestFile struct {
	Path        string  `json:"path"`
	Status      string  `json:"status"`
	ContentHash *string `json:"content_hash"`
	ByteLength  *int64  `json:"byte_length"`
	ReasonCode  *string `json:"reason_code"`
}
type CaptureManifest struct {
	SchemaVersion          int64          `json:"schema_version"`
	WorktreeID             string         `json:"worktree_id"`
	ProfileID              string         `json:"profile_id"`
	PolicyDigest           string         `json:"policy_digest"`
	ProviderDigest         string         `json:"provider_digest"`
	Files                  []ManifestFile `json:"files"`
	DependencyInputDigests []string       `json:"dependency_input_digests"`
	SnapshotDigest         string         `json:"snapshot_digest"`
}

func (m CaptureManifest) normalized(requireDigest bool) (CaptureManifest, error) {
	if m.SchemaVersion != 1 || !uuid(m.WorktreeID) || !lowerHex(m.ProfileID, 64) || !lowerHex(m.PolicyDigest, 64) || !lowerHex(m.ProviderDigest, 64) || (requireDigest && !lowerHex(m.SnapshotDigest, 64)) || m.Files == nil || len(m.Files) > 100000 || m.DependencyInputDigests == nil || len(m.DependencyInputDigests) > 100000 {
		return CaptureManifest{}, fmt.Errorf("invalid capture manifest")
	}
	result := m
	result.Files = append([]ManifestFile{}, m.Files...)
	result.DependencyInputDigests = append([]string{}, m.DependencyInputDigests...)
	for _, f := range result.Files {
		if !validSourcePath(f.Path) || (f.ContentHash != nil && !lowerHex(*f.ContentHash, 64)) || (f.ByteLength != nil && (*f.ByteLength < 0 || *f.ByteLength > maxExactInteger)) || !optionalID(f.ReasonCode) {
			return CaptureManifest{}, fmt.Errorf("invalid manifest file")
		}
		switch f.Status {
		case "captured":
			if f.ContentHash == nil || f.ByteLength == nil {
				return CaptureManifest{}, fmt.Errorf("captured file has no content identity")
			}
		case "metadata_only", "excluded", "missing", "read_error":
		default:
			return CaptureManifest{}, fmt.Errorf("invalid capture status")
		}
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	for i := 1; i < len(result.Files); i++ {
		if result.Files[i-1].Path == result.Files[i].Path {
			return CaptureManifest{}, fmt.Errorf("duplicate capture path")
		}
	}
	for _, digest := range result.DependencyInputDigests {
		if !lowerHex(digest, 64) {
			return CaptureManifest{}, fmt.Errorf("invalid dependency digest")
		}
	}
	sort.Strings(result.DependencyInputDigests)
	return result, nil
}

// DecodeCaptureManifestJSON checks shape and identity, not filesystem capture.
func DecodeCaptureManifestJSON(data []byte, maxBytes int) (CaptureManifest, error) {
	var result CaptureManifest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return result, err
	}
	fields, err := scopeObject(data, []string{"schema_version", "worktree_id", "profile_id", "policy_digest", "provider_digest", "files", "dependency_input_digests", "snapshot_digest"}, nil)
	if err != nil {
		return result, err
	}
	var files []json.RawMessage
	if err := json.Unmarshal(fields["files"], &files); err != nil {
		return result, err
	}
	if len(files) > 100000 {
		return result, fmt.Errorf("too many files")
	}
	for _, file := range files {
		if _, err := scopeObject(file, []string{"path", "status", "content_hash", "byte_length", "reason_code"}, map[string]bool{"content_hash": true, "byte_length": true, "reason_code": true}); err != nil {
			return result, err
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return CaptureManifest{}, err
	}
	if _, err := result.normalized(true); err != nil {
		return CaptureManifest{}, err
	}
	return result, nil
}

// ComputeCaptureDigest hashes the capture input only; parse/type results have
// no place in this preimage. The caller still verifies actual captured bytes.
func ComputeCaptureDigest(manifest CaptureManifest, maxBytes int) (string, error) {
	if maxBytes <= 0 || len(manifest.Files) > maxBytes || len(manifest.DependencyInputDigests) > maxBytes-len(manifest.Files) {
		return "", fmt.Errorf("capture input exceeds budget")
	}
	m, err := manifest.normalized(false)
	if err != nil {
		return "", err
	}
	// Bound the unavoidable aggregate JSON allocation before marshaling it. Each
	// item is independently bounded by the validated path and scalar limits.
	remaining := maxBytes
	for _, f := range m.Files {
		part, err := json.Marshal(f)
		if err != nil {
			return "", err
		}
		if len(part) > remaining {
			return "", fmt.Errorf("capture input exceeds budget")
		}
		remaining -= len(part)
	}
	for _, d := range m.DependencyInputDigests {
		if len(d)+2 > remaining {
			return "", fmt.Errorf("capture input exceeds budget")
		}
		remaining -= len(d) + 2
	}
	value := struct {
		SchemaVersion          int64          `json:"schema_version"`
		WorktreeID             string         `json:"worktree_id"`
		ProfileID              string         `json:"profile_id"`
		PolicyDigest           string         `json:"policy_digest"`
		ProviderDigest         string         `json:"provider_digest"`
		Files                  []ManifestFile `json:"files"`
		DependencyInputDigests []string       `json:"dependency_input_digests"`
	}{m.SchemaVersion, m.WorktreeID, m.ProfileID, m.PolicyDigest, m.ProviderDigest, m.Files, m.DependencyInputDigests}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("capture-v1", canonical, maxBytes)
	return digest, err
}
func VerifyCaptureDigest(m CaptureManifest, maxBytes int) error {
	if !lowerHex(m.SnapshotDigest, 64) {
		return fmt.Errorf("invalid capture digest")
	}
	digest, err := ComputeCaptureDigest(m, maxBytes)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(m.SnapshotDigest), []byte(digest)) != 1 {
		return fmt.Errorf("capture digest mismatch")
	}
	return nil
}
