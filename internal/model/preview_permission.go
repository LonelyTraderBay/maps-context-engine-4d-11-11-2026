package model

import (
	"encoding/json"
	"fmt"
)

// Shape validation does not attest the environment, normalize browser origins,
// authorize a profile, or enforce its expiry and destinations.
type PreviewPermissionProfile struct {
	SchemaVersion          int64    `json:"schema_version"`
	ProfileID              string   `json:"profile_id"`
	ApprovedOrigin         string   `json:"approved_origin"`
	ApprovedPathPrefix     string   `json:"approved_path_prefix"`
	TargetEnvironment      string   `json:"target_environment"`
	SourceWorktreeIDs      []string `json:"source_worktree_ids"`
	AllowAttach            bool     `json:"allow_attach"`
	AllowInteract          bool     `json:"allow_interact"`
	AllowScroll            bool     `json:"allow_scroll"`
	AllowedDevDestinations []string `json:"allowed_dev_destinations"`
	AttestationID          string   `json:"attestation_id"`
	AttestedBy             string   `json:"attested_by"`
	AttestedAt             string   `json:"attested_at"`
	ExpiresAt              string   `json:"expires_at"`
	Version                int64    `json:"version"`
}

func (p PreviewPermissionProfile) normalized() (PreviewPermissionProfile, error) {
	if p.SchemaVersion != 1 || !validID(p.ProfileID) || !boundedString(p.ApprovedOrigin, 2048) || !boundedString(p.ApprovedPathPrefix, 2048) || (p.TargetEnvironment != "isolated_test" && p.TargetEnvironment != "local_mock") || !validID(p.AttestationID) || !validID(p.AttestedBy) || !positiveVersion(p.Version) {
		return PreviewPermissionProfile{}, fmt.Errorf("invalid preview permission profile")
	}
	if _, err := utcTimestamp(p.AttestedAt); err != nil {
		return PreviewPermissionProfile{}, fmt.Errorf("invalid attestation time")
	}
	if _, err := utcTimestamp(p.ExpiresAt); err != nil {
		return PreviewPermissionProfile{}, fmt.Errorf("invalid expiration time")
	}
	result := p
	var err error
	result.SourceWorktreeIDs, err = sortedStrings(p.SourceWorktreeIDs, 1, 32, 36, 0)
	if err != nil {
		return PreviewPermissionProfile{}, err
	}
	for _, id := range result.SourceWorktreeIDs {
		if !uuid(id) {
			return PreviewPermissionProfile{}, fmt.Errorf("invalid source worktree")
		}
	}
	result.AllowedDevDestinations, err = sortedStrings(p.AllowedDevDestinations, 0, 64, 2048, 0)
	if err != nil {
		return PreviewPermissionProfile{}, err
	}
	return result, nil
}
func DecodePreviewPermissionProfileJSON(data []byte, maxBytes int) (PreviewPermissionProfile, error) {
	var p PreviewPermissionProfile
	if err := decodeClosedRequest(data, maxBytes, []string{"schema_version", "profile_id", "approved_origin", "approved_path_prefix", "target_environment", "source_worktree_ids", "allow_attach", "allow_interact", "allow_scroll", "allowed_dev_destinations", "attestation_id", "attested_by", "attested_at", "expires_at", "version"}, &p); err != nil {
		return PreviewPermissionProfile{}, err
	}
	if _, err := p.normalized(); err != nil {
		return PreviewPermissionProfile{}, err
	}
	return p, nil
}

// PreviewPermissionDigest uses the approved recipe; no permission is granted by hashing it.
func PreviewPermissionDigest(p PreviewPermissionProfile, maxBytes int) (string, error) {
	n, err := p.normalized()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	v, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("preview-permission-v1", v, maxBytes)
	return digest, err
}
