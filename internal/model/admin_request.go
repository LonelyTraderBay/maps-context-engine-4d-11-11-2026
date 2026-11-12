package model

import (
	"encoding/json"
	"fmt"
)

type ProfileAdminRequest struct {
	WorktreeID       string           `json:"worktree_id"`
	ProfileName      string           `json:"profile_name"`
	ExpectedRevision int64            `json:"expected_revision"`
	Profile          EffectiveProfile `json:"profile"`
}
type PolicyAdminRequest struct {
	WorktreeID    string       `json:"worktree_id"`
	ExpectedEpoch int64        `json:"expected_epoch"`
	Policy        PolicyConfig `json:"policy"`
}

// Admin decoders require a typed body and expected version. They neither expose
// these CLI-only operations on a transport nor perform authorization or CAS.
func DecodeProfileAdminRequestJSON(data []byte, maxBytes int) (ProfileAdminRequest, error) {
	var r ProfileAdminRequest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	fields, err := scopeObject(data, []string{"worktree_id", "profile_name", "expected_revision", "profile"}, nil)
	if err != nil {
		return r, err
	}
	p, err := DecodeEffectiveProfileJSON(fields["profile"], maxBytes)
	if err != nil {
		return r, err
	}
	delete(fields, "profile")
	raw, err := json.Marshal(fields)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ProfileAdminRequest{}, err
	}
	r.Profile = p
	if !uuid(r.WorktreeID) || !boundedString(r.ProfileName, 128) || r.ExpectedRevision < 0 || r.ExpectedRevision > maxExactInteger {
		return ProfileAdminRequest{}, fmt.Errorf("invalid profile admin request")
	}
	return r, nil
}
func DecodePolicyAdminRequestJSON(data []byte, maxBytes int) (PolicyAdminRequest, error) {
	var r PolicyAdminRequest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	fields, err := scopeObject(data, []string{"worktree_id", "expected_epoch", "policy"}, nil)
	if err != nil {
		return r, err
	}
	p, err := DecodePolicyConfigJSON(fields["policy"], maxBytes)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return PolicyAdminRequest{}, err
	}
	r.Policy = p
	if !uuid(r.WorktreeID) || r.ExpectedEpoch < 0 || r.ExpectedEpoch > maxExactInteger {
		return PolicyAdminRequest{}, fmt.Errorf("invalid policy admin request")
	}
	return r, nil
}
