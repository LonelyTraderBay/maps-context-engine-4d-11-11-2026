package model

import (
	"encoding/json"
	"fmt"
)

type Gap struct {
	Code       string      `json:"code"`
	Reason     string      `json:"reason"`
	Count      *int64      `json:"count"`
	SourceRefs []SourceRef `json:"source_refs"`
}
type Capability struct {
	Name            string `json:"name"`
	ProviderID      string `json:"provider_id"`
	ProviderVersion string `json:"provider_version"`
	Status          string `json:"status"`
}
type Coverage struct {
	Status           string       `json:"status"`
	Capabilities     []Capability `json:"capabilities"`
	Gaps             []Gap        `json:"gaps"`
	ScopeDescription string       `json:"scope_description"`
}
type Freshness struct {
	Status       string  `json:"status"`
	Scope        string  `json:"scope"`
	CheckedAt    *string `json:"checked_at"`
	BufferScope  string  `json:"buffer_scope"`
	Consistency  string  `json:"consistency"`
	ChangedCount *int64  `json:"changed_count"`
	SourceView   string  `json:"source_view"`
}

func coverageStatus(v string) bool {
	return v == "supported_scope_complete" || v == "partial" || v == "unsupported" || v == "unknown"
}
func DecodeGapJSON(data []byte, maxBytes int) (Gap, error) {
	var g Gap
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return g, err
	}
	fields, err := scopeObject(data, []string{"code", "reason", "count", "source_refs"}, map[string]bool{"count": true})
	if err != nil {
		return g, err
	}
	var refs []json.RawMessage
	if err := json.Unmarshal(fields["source_refs"], &refs); err != nil {
		return g, err
	}
	if len(refs) > 16 {
		return g, fmt.Errorf("too many source refs")
	}
	for _, ref := range refs {
		if _, err := DecodeSourceRefJSON(ref, maxBytes); err != nil {
			return g, err
		}
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return Gap{}, err
	}
	if !validID(g.Code) || !boundedString(g.Reason, 1024) || g.SourceRefs == nil || (g.Count != nil && (*g.Count < 0 || *g.Count > maxExactInteger)) {
		return Gap{}, fmt.Errorf("invalid gap")
	}
	return g, nil
}
func DecodeCapabilityJSON(data []byte, maxBytes int) (Capability, error) {
	var c Capability
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return c, err
	}
	if _, err := scopeObject(data, []string{"name", "provider_id", "provider_version", "status"}, nil); err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Capability{}, err
	}
	if !validID(c.Name) || !validID(c.ProviderID) || !boundedString(c.ProviderVersion, 128) || !coverageStatus(c.Status) {
		return Capability{}, fmt.Errorf("invalid capability")
	}
	return c, nil
}

// DecodeCoverageJSON checks labels and shape, not whether a provider actually
// achieved the claimed scope or whether source refs are authorized.
func DecodeCoverageJSON(data []byte, maxBytes int) (Coverage, error) {
	var c Coverage
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return c, err
	}
	fields, err := scopeObject(data, []string{"status", "capabilities", "gaps", "scope_description"}, nil)
	if err != nil {
		return c, err
	}
	var caps, gaps []json.RawMessage
	if err := json.Unmarshal(fields["capabilities"], &caps); err != nil {
		return c, err
	}
	if err := json.Unmarshal(fields["gaps"], &gaps); err != nil {
		return c, err
	}
	if len(caps) > 128 || len(gaps) > 64 {
		return c, fmt.Errorf("too many capabilities or gaps")
	}
	for _, item := range caps {
		if _, err := DecodeCapabilityJSON(item, maxBytes); err != nil {
			return c, err
		}
	}
	for _, item := range gaps {
		if _, err := DecodeGapJSON(item, maxBytes); err != nil {
			return c, err
		}
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Coverage{}, err
	}
	if !coverageStatus(c.Status) || !boundedString(c.ScopeDescription, 1024) || c.Capabilities == nil || c.Gaps == nil {
		return Coverage{}, fmt.Errorf("invalid coverage")
	}
	return c, nil
}
func DecodeFreshnessJSON(data []byte, maxBytes int) (Freshness, error) {
	var f Freshness
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return f, err
	}
	if _, err := scopeObject(data, []string{"status", "scope", "checked_at", "buffer_scope", "consistency", "changed_count", "source_view"}, map[string]bool{"checked_at": true, "changed_count": true}); err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return Freshness{}, err
	}
	switch f.Status {
	case "observed_current", "changed", "unknown", "not_checked":
	default:
		return Freshness{}, fmt.Errorf("invalid freshness status")
	}
	switch f.Scope {
	case "workspace", "returned_files", "none":
	default:
		return Freshness{}, fmt.Errorf("invalid freshness scope")
	}
	switch f.BufferScope {
	case "disk_only", "editor_session":
	default:
		return Freshness{}, fmt.Errorf("invalid buffer scope")
	}
	switch f.Consistency {
	case "captured_bytes_with_observed_stability", "captured_buffer_version":
	default:
		return Freshness{}, fmt.Errorf("invalid consistency")
	}
	switch f.SourceView {
	case "indexed_snapshot", "editor_snapshot":
	default:
		return Freshness{}, fmt.Errorf("invalid source view")
	}
	if f.Status == "observed_current" && f.Consistency != "captured_bytes_with_observed_stability" {
		return Freshness{}, fmt.Errorf("current freshness requires observed byte stability")
	}
	if f.CheckedAt != nil {
		if _, err := utcTimestamp(*f.CheckedAt); err != nil {
			return Freshness{}, err
		}
	}
	if f.ChangedCount != nil && (*f.ChangedCount < 0 || *f.ChangedCount > maxExactInteger) {
		return Freshness{}, fmt.Errorf("invalid changed count")
	}
	return f, nil
}
