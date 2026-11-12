package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// ScopeVector is a wire value. Its fields are assertions until verified against
// trusted stores, policy, incarnations and current revisions by the caller.
type ScopeVector struct {
	Kind               string              `json:"kind"`
	Members            []ScopeMember       `json:"members"`
	VectorDigest       string              `json:"vector_digest"`
	GroupID            *string             `json:"group_id"`
	GroupRevision      *int64              `json:"group_revision"`
	CatalogIncarnation string              `json:"catalog_incarnation"`
	UnavailableMembers []MemberUnavailable `json:"unavailable_members"`
}
type ScopeMember struct {
	WorktreeID         string  `json:"worktree_id"`
	ProfileID          string  `json:"profile_id"`
	GenerationID       string  `json:"generation_id"`
	SnapshotDigest     string  `json:"snapshot_digest"`
	PolicyDigest       string  `json:"policy_digest"`
	PolicyEpoch        int64   `json:"policy_epoch"`
	BindingRevision    int64   `json:"binding_revision"`
	RecordEpoch        int64   `json:"record_epoch"`
	ServiceID          *string `json:"service_id"`
	IndexIncarnation   string  `json:"index_incarnation"`
	RecordsIncarnation string  `json:"records_incarnation"`
}
type MemberUnavailable struct {
	ServiceID  *string `json:"service_id"`
	WorktreeID *string `json:"worktree_id"`
	ProfileID  *string `json:"profile_id"`
	Status     string  `json:"status"`
	ReasonCode string  `json:"reason_code"`
}

// DecodeScopeVectorJSON validates the closed wire shape and local invariants.
// It does not authenticate the digest or verify membership, policy or freshness.
func DecodeScopeVectorJSON(data []byte, maxBytes int) (ScopeVector, error) {
	var result ScopeVector
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return result, err
	}
	fields, err := scopeObject(data, []string{"kind", "members", "vector_digest", "group_id", "group_revision", "catalog_incarnation", "unavailable_members"}, map[string]bool{"group_id": true, "group_revision": true})
	if err != nil {
		return result, err
	}
	for _, entry := range []struct {
		name     string
		required []string
		nullable map[string]bool
	}{
		{"members", []string{"worktree_id", "profile_id", "generation_id", "snapshot_digest", "policy_digest", "policy_epoch", "binding_revision", "record_epoch", "service_id", "index_incarnation", "records_incarnation"}, map[string]bool{"service_id": true}},
		{"unavailable_members", []string{"service_id", "worktree_id", "profile_id", "status", "reason_code"}, map[string]bool{"service_id": true, "worktree_id": true, "profile_id": true}},
	} {
		var items []json.RawMessage
		if err := json.Unmarshal(fields[entry.name], &items); err != nil {
			return result, err
		}
		if len(items) > 32 {
			return result, fmt.Errorf("too many %s", entry.name)
		}
		for _, item := range items {
			if _, err := scopeObject(item, entry.required, entry.nullable); err != nil {
				return result, err
			}
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return ScopeVector{}, err
	}
	if err := result.validateShape(); err != nil {
		return ScopeVector{}, err
	}
	return result, nil
}
func scopeObject(data []byte, required []string, nullable map[string]bool) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil || len(fields) != len(required) {
		return nil, fmt.Errorf("invalid scope object fields")
	}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || (!nullable[key] && bytes.Equal(bytes.TrimSpace(v), []byte("null"))) {
			return nil, fmt.Errorf("missing or null scope field %s", key)
		}
	}
	return fields, nil
}
func validID(id string) bool {
	return utf8.ValidString(id) && utf8.RuneCountInString(id) >= 1 && utf8.RuneCountInString(id) <= 192
}
func optionalID(id *string) bool           { return id == nil || validID(*id) }
func (s ScopeVector) validateShape() error { return s.validateShapeForDigest(true) }

func (s ScopeVector) validateShapeForDigest(requireDigest bool) error {
	if s.Members == nil || s.UnavailableMembers == nil || (requireDigest && !lowerHex(s.VectorDigest, 64)) || !uuid(s.CatalogIncarnation) || len(s.Members) > 32 || len(s.UnavailableMembers) > 32 {
		return fmt.Errorf("invalid vector identity or member count")
	}
	switch s.Kind {
	case "single":
		if len(s.Members) != 1 || s.GroupID != nil || s.GroupRevision != nil {
			return fmt.Errorf("invalid single scope")
		}
	case "federated":
		if s.GroupID == nil || !uuid(*s.GroupID) || s.GroupRevision == nil || *s.GroupRevision < 1 || *s.GroupRevision > maxExactInteger {
			return fmt.Errorf("invalid federated scope")
		}
	default:
		return fmt.Errorf("invalid scope kind")
	}
	for _, m := range s.Members {
		if err := validateScopeMember(m); err != nil {
			return err
		}
	}

	for _, m := range s.UnavailableMembers {
		if err := validateUnavailable(m); err != nil {
			return err
		}
	}

	return nil
}

func validateScopeMember(m ScopeMember) error {
	if !uuid(m.WorktreeID) || !uuid(m.GenerationID) || !uuid(m.IndexIncarnation) || !uuid(m.RecordsIncarnation) || !lowerHex(m.ProfileID, 64) || !lowerHex(m.SnapshotDigest, 64) || !lowerHex(m.PolicyDigest, 64) || m.PolicyEpoch < 1 || m.PolicyEpoch > maxExactInteger || m.BindingRevision < 1 || m.BindingRevision > maxExactInteger || m.RecordEpoch < 0 || m.RecordEpoch > maxExactInteger || !optionalID(m.ServiceID) {
		return fmt.Errorf("invalid resolved member")
	}
	return nil
}
func DecodeScopeMemberJSON(data []byte, maxBytes int) (ScopeMember, error) {
	var m ScopeMember
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return m, err
	}
	if _, err := scopeObject(data, []string{"worktree_id", "profile_id", "generation_id", "snapshot_digest", "policy_digest", "policy_epoch", "binding_revision", "record_epoch", "service_id", "index_incarnation", "records_incarnation"}, map[string]bool{"service_id": true}); err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ScopeMember{}, err
	}
	if err := validateScopeMember(m); err != nil {
		return ScopeMember{}, err
	}
	return m, nil
}
