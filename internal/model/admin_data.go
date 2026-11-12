package model

import (
	"encoding/json"
	"fmt"
)

type GroupSummary struct {
	Config   GroupConfig `json:"config"`
	Revision int64       `json:"revision"`
	Status   string      `json:"status"`
}
type GroupAdminData struct {
	Operation string         `json:"operation"`
	Groups    []GroupSummary `json:"groups"`
}
type ProfileAdminData struct {
	Operation   string `json:"operation"`
	WorktreeID  string `json:"worktree_id"`
	ProfileName string `json:"profile_name"`
	ProfileID   string `json:"profile_id"`
	Revision    int64  `json:"revision"`
}
type PolicyAdminData struct {
	Operation    string `json:"operation"`
	WorktreeID   string `json:"worktree_id"`
	PolicyDigest string `json:"policy_digest"`
	PolicyEpoch  int64  `json:"policy_epoch"`
}

// These response values do not prove catalog state or authorize mutations.
func DecodeGroupSummaryJSON(data []byte, maxBytes int) (GroupSummary, error) {
	var r GroupSummary
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"config", "revision", "status"}, nil)
	if err != nil {
		return r, err
	}
	r.Config, err = DecodeGroupConfigJSON(f["config"], maxBytes)
	if err != nil {
		return GroupSummary{}, err
	}
	if err = json.Unmarshal(f["revision"], &r.Revision); err != nil {
		return GroupSummary{}, err
	}
	if err = json.Unmarshal(f["status"], &r.Status); err != nil {
		return GroupSummary{}, err
	}
	if r.Revision < 1 || r.Revision > maxExactInteger || r.Status != "active" && r.Status != "removed" {
		return GroupSummary{}, fmt.Errorf("invalid group summary")
	}
	return r, nil
}
func DecodeGroupAdminDataJSON(data []byte, maxBytes int) (GroupAdminData, error) {
	var r GroupAdminData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "groups"}, nil)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(f["operation"], &r.Operation); err != nil || r.Operation != "group_admin" {
		return GroupAdminData{}, fmt.Errorf("invalid group operation")
	}
	var items []json.RawMessage
	if err = json.Unmarshal(f["groups"], &items); err != nil {
		return r, err
	}
	if items == nil || len(items) > 100 {
		return r, fmt.Errorf("invalid groups")
	}
	r.Groups = make([]GroupSummary, 0, len(items))
	for _, item := range items {
		v, e := DecodeGroupSummaryJSON(item, maxBytes)
		if e != nil {
			return GroupAdminData{}, e
		}
		r.Groups = append(r.Groups, v)
	}
	return r, nil
}
func DecodeProfileAdminDataJSON(data []byte, maxBytes int) (ProfileAdminData, error) {
	var r ProfileAdminData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"operation", "worktree_id", "profile_name", "profile_id", "revision"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return ProfileAdminData{}, err
	}
	if r.Operation != "profile_admin" || !uuid(r.WorktreeID) || !boundedString(r.ProfileName, 128) || !lowerHex(r.ProfileID, 64) || r.Revision < 1 || r.Revision > maxExactInteger {
		return ProfileAdminData{}, fmt.Errorf("invalid profile response")
	}
	return r, nil
}
func DecodePolicyAdminDataJSON(data []byte, maxBytes int) (PolicyAdminData, error) {
	var r PolicyAdminData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"operation", "worktree_id", "policy_digest", "policy_epoch"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return PolicyAdminData{}, err
	}
	if r.Operation != "policy_admin" || !uuid(r.WorktreeID) || !lowerHex(r.PolicyDigest, 64) || r.PolicyEpoch < 1 || r.PolicyEpoch > maxExactInteger {
		return PolicyAdminData{}, fmt.Errorf("invalid policy response")
	}
	return r, nil
}
