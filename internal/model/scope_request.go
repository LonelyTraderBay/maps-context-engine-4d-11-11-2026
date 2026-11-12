package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ScopeRequest describes a requested selection, not an authorized scope.
type ScopeRequest struct {
	WorktreeID   *string      `json:"worktree_id,omitempty"`
	ProfileID    *string      `json:"profile_id,omitempty"`
	GenerationID *string      `json:"generation_id,omitempty"`
	GroupID      *string      `json:"group_id,omitempty"`
	PinnedVector *ScopeVector `json:"pinned_vector,omitempty"`
}

func DecodeScopeRequestJSON(data []byte, maxBytes int) (ScopeRequest, error) {
	var request ScopeRequest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return request, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return request, err
	}
	if fields == nil {
		return request, fmt.Errorf("scope request must be an object")
	}
	for key, value := range fields {
		switch key {
		case "worktree_id", "profile_id", "generation_id", "group_id", "pinned_vector":
		default:
			return request, fmt.Errorf("unknown scope field %s", key)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return request, fmt.Errorf("null scope field %s", key)
		}
	}
	if raw, ok := fields["pinned_vector"]; ok {
		vector, err := DecodeScopeVectorJSON(raw, maxBytes)
		if err != nil {
			return request, err
		}
		request.PinnedVector = &vector
		delete(fields, "pinned_vector")
	}
	remaining, err := json.Marshal(fields)
	if err != nil {
		return ScopeRequest{}, err
	}
	var rest ScopeRequest
	if err := json.Unmarshal(remaining, &rest); err != nil {
		return ScopeRequest{}, err
	}
	rest.PinnedVector = request.PinnedVector
	// One is only a structural placeholder for the empty selector; the
	// caller must recheck the actual explicit binding count.
	if _, err := rest.SelectionMode(1); err != nil {
		return ScopeRequest{}, err
	}
	return rest, nil
}

// SelectionMode checks only the request's structural selection rule. Resolving
// the actual binding, digest, membership, authorization and revisions is separate.
func (r ScopeRequest) SelectionMode(explicitBindings int) (string, error) {
	if r.PinnedVector != nil {
		if r.WorktreeID != nil || r.ProfileID != nil || r.GenerationID != nil || r.GroupID != nil {
			return "", fmt.Errorf("pinned scope mixed with live selection")
		}
		if err := r.PinnedVector.validateShape(); err != nil {
			return "", err
		}
		return "pinned", nil
	}
	if r.GroupID != nil {
		if !uuid(*r.GroupID) || r.WorktreeID != nil || r.ProfileID != nil || r.GenerationID != nil {
			return "", fmt.Errorf("invalid group selection")
		}
		return "group", nil
	}
	if r.WorktreeID == nil && r.ProfileID == nil && r.GenerationID == nil {
		if explicitBindings != 1 {
			return "", fmt.Errorf("no unique explicit binding")
		}
		return "bound", nil
	}
	if r.WorktreeID == nil || r.ProfileID == nil || !uuid(*r.WorktreeID) || !lowerHex(*r.ProfileID, 64) || (r.GenerationID != nil && !uuid(*r.GenerationID)) {
		return "", fmt.Errorf("invalid single selection")
	}
	return "single", nil
}
