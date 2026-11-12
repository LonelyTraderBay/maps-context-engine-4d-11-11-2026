package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

func validateUnavailable(m MemberUnavailable) error {
	if !optionalID(m.ServiceID) || (m.WorktreeID != nil && !uuid(*m.WorktreeID)) || (m.ProfileID != nil && !lowerHex(*m.ProfileID, 64)) || !validID(m.ReasonCode) {
		return fmt.Errorf("invalid unavailable member")
	}
	switch m.Status {
	case "index_missing", "unavailable", "access_denied", "profile_missing":
		return nil
	default:
		return fmt.Errorf("invalid unavailable status")
	}
}
func compareOptional(a, b *string) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	if *a < *b {
		return -1
	}
	if *a > *b {
		return 1
	}
	return 0
}

// NormalizeUnavailableMembers follows the scope-v1 ordering, including the
// approved canonical-byte tie break. It neither verifies a scope nor hashes it.
func NormalizeUnavailableMembers(input []MemberUnavailable, maxBytes int) ([]MemberUnavailable, error) {
	if len(input) > 32 || maxBytes <= 0 {
		return nil, fmt.Errorf("invalid member count or byte budget")
	}
	type entry struct {
		member  MemberUnavailable
		encoded []byte
	}
	entries := make([]entry, 0, len(input))
	remaining := maxBytes
	for _, m := range input {
		if err := validateUnavailable(m); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		// JSON string escaping may be longer than E(value); bound it independently.
		value, err := DecodeIdentityJSON(raw, len(raw))
		if err != nil {
			return nil, err
		}
		writer := canonicalWriter{limit: remaining}
		if err := writer.value(value, 0); err != nil {
			return nil, err
		}
		remaining -= len(writer.data)
		entries = append(entries, entry{member: m, encoded: writer.data})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if c := compareOptional(a.member.ServiceID, b.member.ServiceID); c != 0 {
			return c < 0
		}
		if c := compareOptional(a.member.WorktreeID, b.member.WorktreeID); c != 0 {
			return c < 0
		}
		if a.member.Status != b.member.Status {
			return a.member.Status < b.member.Status
		}
		return bytes.Compare(a.encoded, b.encoded) < 0
	})
	result := make([]MemberUnavailable, 0, len(entries))
	for _, e := range entries {
		result = append(result, e.member)
	}
	return result, nil
}
