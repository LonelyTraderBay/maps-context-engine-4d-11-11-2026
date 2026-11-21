package model

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"sort"
)

type ManifestSource struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}
type AnchorEntry struct {
	AnchorID      string    `json:"anchor_id"`
	Kind          string    `json:"kind"`
	Path          string    `json:"path"`
	ContentHash   string    `json:"content_hash"`
	Range         ByteRange `json:"range"`
	OwnerAnchorID *string   `json:"owner_anchor_id"`
}
type BuildManifest struct {
	SchemaVersion       int64            `json:"schema_version"`
	WorktreeID          string           `json:"worktree_id"`
	ProfileID           string           `json:"profile_id"`
	BuildMapID          string           `json:"build_map_id"`
	ProviderVersion     string           `json:"provider_version"`
	BuildConfigDigest   string           `json:"build_config_digest"`
	InputManifestDigest string           `json:"input_manifest_digest"`
	Mode                string           `json:"mode"`
	ModuleCoverage      string           `json:"module_coverage"`
	Sources             []ManifestSource `json:"sources"`
	Anchors             []AnchorEntry    `json:"anchors"`
	ProviderDigest      string           `json:"provider_digest"`
}

func (m BuildManifest) normalized(requireDigest bool) (BuildManifest, error) {
	if m.SchemaVersion != 1 || !uuid(m.WorktreeID) || !lowerHex(m.ProfileID, 64) || (requireDigest && !lowerHex(m.BuildMapID, 64)) || !boundedString(m.ProviderVersion, 128) || !lowerHex(m.BuildConfigDigest, 64) || !lowerHex(m.InputManifestDigest, 64) || !lowerHex(m.ProviderDigest, 64) || m.Sources == nil || len(m.Sources) > 100000 || m.Anchors == nil || len(m.Anchors) > 100000 {
		return BuildManifest{}, fmt.Errorf("invalid build manifest")
	}
	if m.Mode != "development" && m.Mode != "test" {
		return BuildManifest{}, fmt.Errorf("invalid build mode")
	}
	if m.ModuleCoverage != "all_declared_modules" && m.ModuleCoverage != "loaded_modules_only" && m.ModuleCoverage != "partial" {
		return BuildManifest{}, fmt.Errorf("invalid module coverage")
	}
	result := m
	result.Sources = append([]ManifestSource{}, m.Sources...)
	result.Anchors = append([]AnchorEntry{}, m.Anchors...)
	sources := make(map[string]string, len(m.Sources))
	for _, s := range result.Sources {
		if !validSourcePath(s.Path) || !lowerHex(s.ContentHash, 64) {
			return BuildManifest{}, fmt.Errorf("invalid manifest source")
		}
		if _, ok := sources[s.Path]; ok {
			return BuildManifest{}, fmt.Errorf("duplicate source path")
		}
		sources[s.Path] = s.ContentHash
	}
	anchors := make(map[string]AnchorEntry, len(m.Anchors))
	for _, a := range result.Anchors {
		if !lowerHex(a.AnchorID, 64) || !validSourcePath(a.Path) || !lowerHex(a.ContentHash, 64) || sources[a.Path] != a.ContentHash || a.Range.StartByte < 0 || a.Range.EndByte < a.Range.StartByte || a.Range.EndByte > maxExactInteger || (a.OwnerAnchorID != nil && !lowerHex(*a.OwnerAnchorID, 64)) {
			return BuildManifest{}, fmt.Errorf("invalid manifest anchor")
		}
		switch a.Kind {
		case "element_site", "component_definition", "component_render_site", "event_binding_site":
		default:
			return BuildManifest{}, fmt.Errorf("invalid anchor kind")
		}
		if _, ok := anchors[a.AnchorID]; ok {
			return BuildManifest{}, fmt.Errorf("duplicate anchor ID")
		}
		anchors[a.AnchorID] = a
	}
	done := make(map[string]bool, len(anchors))
	for id := range anchors {
		seen := map[string]bool{}
		current := id
		for !done[current] {
			if seen[current] {
				return BuildManifest{}, fmt.Errorf("anchor owner cycle")
			}
			seen[current] = true
			a, ok := anchors[current]
			if !ok {
				return BuildManifest{}, fmt.Errorf("missing owner anchor")
			}
			if a.OwnerAnchorID == nil {
				break
			}
			current = *a.OwnerAnchorID
		}
		for id := range seen {
			done[id] = true
		}
	}
	sort.Slice(result.Sources, func(i, j int) bool { return result.Sources[i].Path < result.Sources[j].Path })
	sort.Slice(result.Anchors, func(i, j int) bool {
		a, b := result.Anchors[i], result.Anchors[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		if a.Range.EndByte != b.Range.EndByte {
			return a.Range.EndByte < b.Range.EndByte
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.AnchorID < b.AnchorID
	})
	return result, nil
}

// DecodeBuildManifestJSON validates structural links and acyclic owners. It
// does not verify source bytes, anchor IDs or filesystem authorization.
func DecodeBuildManifestJSON(data []byte, maxBytes int) (BuildManifest, error) {
	var m BuildManifest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return m, err
	}
	fields, err := scopeObject(data, []string{"schema_version", "worktree_id", "profile_id", "build_map_id", "provider_version", "build_config_digest", "input_manifest_digest", "mode", "module_coverage", "sources", "anchors", "provider_digest"}, nil)
	if err != nil {
		return m, err
	}
	var sources, anchors []json.RawMessage
	if err := json.Unmarshal(fields["sources"], &sources); err != nil {
		return m, err
	}
	if err := json.Unmarshal(fields["anchors"], &anchors); err != nil {
		return m, err
	}
	if len(sources) > 100000 || len(anchors) > 100000 {
		return m, fmt.Errorf("too many manifest items")
	}
	for _, s := range sources {
		if _, err := scopeObject(s, []string{"path", "content_hash"}, nil); err != nil {
			return m, err
		}
	}
	for _, a := range anchors {
		f, err := scopeObject(a, []string{"anchor_id", "kind", "path", "content_hash", "range", "owner_anchor_id"}, map[string]bool{"owner_anchor_id": true})
		if err != nil {
			return m, err
		}
		if _, err := scopeObject(f["range"], []string{"start_byte", "end_byte"}, nil); err != nil {
			return m, err
		}
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return BuildManifest{}, err
	}
	if _, err := m.normalized(true); err != nil {
		return BuildManifest{}, err
	}
	return m, nil
}

// NormalizeBuildManifest validates the closed identity and returns sorted copies.
func NormalizeBuildManifest(manifest BuildManifest) (BuildManifest, error) {
	return manifest.normalized(true)
}

func ComputeBuildMapDigest(manifest BuildManifest, maxBytes int) (string, error) {
	if maxBytes <= 0 || len(manifest.Sources) > maxBytes || len(manifest.Anchors) > maxBytes-len(manifest.Sources) {
		return "", fmt.Errorf("manifest exceeds budget")
	}
	m, err := manifest.normalized(false)
	if err != nil {
		return "", err
	}
	remaining := maxBytes
	for _, item := range m.Sources {
		raw, err := json.Marshal(item)
		if err != nil {
			return "", err
		}
		if len(raw) > remaining {
			return "", fmt.Errorf("manifest exceeds budget")
		}
		remaining -= len(raw)
	}
	for _, item := range m.Anchors {
		raw, err := json.Marshal(item)
		if err != nil {
			return "", err
		}
		if len(raw) > remaining {
			return "", fmt.Errorf("manifest exceeds budget")
		}
		remaining -= len(raw)
	}

	value := struct {
		SchemaVersion       int64            `json:"schema_version"`
		WorktreeID          string           `json:"worktree_id"`
		ProfileID           string           `json:"profile_id"`
		ProviderVersion     string           `json:"provider_version"`
		BuildConfigDigest   string           `json:"build_config_digest"`
		InputManifestDigest string           `json:"input_manifest_digest"`
		Mode                string           `json:"mode"`
		ModuleCoverage      string           `json:"module_coverage"`
		Sources             []ManifestSource `json:"sources"`
		Anchors             []AnchorEntry    `json:"anchors"`
		ProviderDigest      string           `json:"provider_digest"`
	}{m.SchemaVersion, m.WorktreeID, m.ProfileID, m.ProviderVersion, m.BuildConfigDigest, m.InputManifestDigest, m.Mode, m.ModuleCoverage, m.Sources, m.Anchors, m.ProviderDigest}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	v, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("build-map-v1", v, maxBytes)
	return digest, err
}
func VerifyBuildMapDigest(m BuildManifest, maxBytes int) error {
	if !lowerHex(m.BuildMapID, 64) {
		return fmt.Errorf("invalid build map ID")
	}
	digest, err := ComputeBuildMapDigest(m, maxBytes)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(m.BuildMapID), []byte(digest)) != 1 {
		return fmt.Errorf("build map digest mismatch")
	}
	return nil
}
