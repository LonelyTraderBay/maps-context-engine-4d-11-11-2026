package model

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
)

type ProviderStart struct {
	ProviderProtocol int64            `json:"provider_protocol"`
	Type             string           `json:"type"`
	JobID            string           `json:"job_id"`
	ProviderID       string           `json:"provider_id"`
	ProviderDigest   string           `json:"provider_digest"`
	ProfileID        string           `json:"profile_id"`
	GenerationID     string           `json:"generation_id"`
	SnapshotRoot     string           `json:"snapshot_root"`
	ManifestPath     string           `json:"manifest_path"`
	ManifestDigest   string           `json:"manifest_digest"`
	Profile          EffectiveProfile `json:"profile"`
	MaxFrameBytes    int64            `json:"max_frame_bytes"`
	DeadlineAt       string           `json:"deadline_at"`
}

// DecodeProviderStartJSON checks declared identities; the coordinator must
// independently prove authorized snapshot paths and captured bytes.
func DecodeProviderStartJSON(data []byte, maxBytes int) (ProviderStart, error) {
	maxBytes = providerFrameLimit(maxBytes)
	var p ProviderStart
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	f, err := scopeObject(data, []string{"provider_protocol", "type", "job_id", "provider_id", "provider_digest", "profile_id", "generation_id", "snapshot_root", "manifest_path", "manifest_digest", "profile", "max_frame_bytes", "deadline_at"}, nil)
	if err != nil {
		return p, err
	}
	profile, err := DecodeEffectiveProfileJSON(f["profile"], maxBytes)
	if err != nil {
		return p, err
	}
	delete(f, "profile")
	raw, err := json.Marshal(f)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ProviderStart{}, err
	}
	p.Profile = profile
	if p.ProviderProtocol != 1 || p.Type != "analyze_start" || !uuid(p.JobID) || !validID(p.ProviderID) || !lowerHex(p.ProviderDigest, 64) || !lowerHex(p.ProfileID, 64) || !uuid(p.GenerationID) || !boundedString(p.SnapshotRoot, 4096) || !boundedString(p.ManifestPath, 4096) || !lowerHex(p.ManifestDigest, 64) || p.MaxFrameBytes < 65536 || p.MaxFrameBytes > 16777216 {
		return ProviderStart{}, fmt.Errorf("invalid provider start")
	}
	if _, err := utcTimestamp(p.DeadlineAt); err != nil {
		return ProviderStart{}, err
	}
	var provider string
	switch profile := profile.(type) {
	case *GoProfile:
		provider = profile.ProviderDigest
	case *TSProfile:
		provider = profile.ProviderDigest
	default:
		return ProviderStart{}, fmt.Errorf("unsupported provider profile")
	}
	digest, err := ProfileDigest(profile, maxBytes)
	if err != nil {
		return ProviderStart{}, err
	}
	if subtle.ConstantTimeCompare([]byte(digest), []byte(p.ProfileID)) != 1 || subtle.ConstantTimeCompare([]byte(provider), []byte(p.ProviderDigest)) != 1 {
		return ProviderStart{}, fmt.Errorf("provider profile mismatch")
	}
	return p, nil
}
