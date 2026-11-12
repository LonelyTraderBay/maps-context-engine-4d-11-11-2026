package model

import (
	"encoding/json"
	"fmt"
)

type PreviewPermissionAdminRequest struct {
	Action          string                    `json:"action"`
	ProfileID       *string                   `json:"profile_id,omitempty"`
	ExpectedVersion *int64                    `json:"expected_version,omitempty"`
	Profile         *PreviewPermissionProfile `json:"profile,omitempty"`
	Reason          *string                   `json:"reason,omitempty"`
	Limit           *int64                    `json:"limit,omitempty"`
	Cursor          *string                   `json:"cursor,omitempty"`
}
type PreviewPermissionSummary struct {
	ProfileID       string                   `json:"profile_id"`
	Version         int64                    `json:"version"`
	DeclaredStatus  string                   `json:"declared_status"`
	EffectiveStatus string                   `json:"effective_status"`
	Digest          string                   `json:"digest"`
	Profile         PreviewPermissionProfile `json:"profile"`
}
type PreviewPermissionAdminData struct {
	Operation string                     `json:"operation"`
	Profiles  []PreviewPermissionSummary `json:"profiles"`
}

// These shapes do not authenticate the actor or perform permission CAS.
func DecodePreviewPermissionAdminRequestJSON(data []byte, maxBytes int) (PreviewPermissionAdminRequest, error) {
	var r PreviewPermissionAdminRequest
	f, err := optionalObject(data, maxBytes, "action", "profile_id", "expected_version", "profile", "reason", "limit", "cursor")
	if err != nil {
		return r, err
	}
	if _, ok := f["action"]; !ok {
		return r, fmt.Errorf("missing permission action")
	}
	if raw, ok := f["profile"]; ok {
		p, e := DecodePreviewPermissionProfileJSON(raw, maxBytes)
		if e != nil {
			return r, e
		}
		r.Profile = &p
		delete(f, "profile")
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return r, err
	}
	var body PreviewPermissionAdminRequest
	if err = json.Unmarshal(raw, &body); err != nil {
		return r, err
	}
	body.Profile = r.Profile
	if !optionalID(body.ProfileID) || !optionalRange(body.ExpectedVersion, 0, maxExactInteger) || !optionalRange(body.Limit, 1, 100) || body.Cursor != nil && !validToken(*body.Cursor) || body.Reason != nil && !boundedString(*body.Reason, 2048) {
		return PreviewPermissionAdminRequest{}, fmt.Errorf("invalid permission request")
	}
	switch body.Action {
	case "get":
		if body.ProfileID == nil || body.ExpectedVersion != nil || body.Profile != nil {
			return PreviewPermissionAdminRequest{}, fmt.Errorf("invalid permission get")
		}
	case "list":
		if body.ProfileID != nil || body.ExpectedVersion != nil || body.Profile != nil {
			return PreviewPermissionAdminRequest{}, fmt.Errorf("invalid permission list")
		}
		if body.Limit == nil {
			v := int64(20)
			body.Limit = &v
		}
	case "put":
		if body.Profile == nil || body.ExpectedVersion == nil || *body.ExpectedVersion == maxExactInteger || body.Profile.Version != *body.ExpectedVersion+1 || body.ProfileID != nil && *body.ProfileID != body.Profile.ProfileID {
			return PreviewPermissionAdminRequest{}, fmt.Errorf("invalid permission put")
		}
	case "revoke":
		if body.ProfileID == nil || body.ExpectedVersion == nil || *body.ExpectedVersion < 1 || body.Reason == nil || body.Profile != nil {
			return PreviewPermissionAdminRequest{}, fmt.Errorf("invalid permission revoke")
		}
	default:
		return PreviewPermissionAdminRequest{}, fmt.Errorf("invalid permission action")
	}
	return body, nil
}
func DecodePreviewPermissionSummaryJSON(data []byte, maxBytes int) (PreviewPermissionSummary, error) {
	var r PreviewPermissionSummary
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"profile_id", "version", "declared_status", "effective_status", "digest", "profile"}, nil)
	if err != nil {
		return r, err
	}
	r.Profile, err = DecodePreviewPermissionProfileJSON(f["profile"], maxBytes)
	if err != nil {
		return PreviewPermissionSummary{}, err
	}
	delete(f, "profile")
	raw, err := json.Marshal(f)
	if err != nil {
		return PreviewPermissionSummary{}, err
	}
	var body PreviewPermissionSummary
	if err = json.Unmarshal(raw, &body); err != nil {
		return PreviewPermissionSummary{}, err
	}
	body.Profile = r.Profile
	if !validID(body.ProfileID) || !positiveVersion(body.Version) || body.ProfileID != body.Profile.ProfileID || body.Version != body.Profile.Version || !lowerHex(body.Digest, 64) || (body.DeclaredStatus != "approved" && body.DeclaredStatus != "revoked") || (body.EffectiveStatus != "approved" && body.EffectiveStatus != "revoked" && body.EffectiveStatus != "expired") {
		return PreviewPermissionSummary{}, fmt.Errorf("invalid permission summary")
	}
	if body.DeclaredStatus == "revoked" && body.EffectiveStatus == "approved" {
		return PreviewPermissionSummary{}, fmt.Errorf("revoked permission cannot be effective approved")
	}
	digest, err := PreviewPermissionDigest(body.Profile, maxBytes)
	if err != nil || body.Digest != digest {
		return PreviewPermissionSummary{}, fmt.Errorf("permission digest mismatch")
	}
	return body, nil
}
func DecodePreviewPermissionAdminDataJSON(data []byte, maxBytes int) (PreviewPermissionAdminData, error) {
	var r PreviewPermissionAdminData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "profiles"}, nil)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(f["operation"], &r.Operation); err != nil || r.Operation != "preview_permission_admin" {
		return PreviewPermissionAdminData{}, fmt.Errorf("invalid permission operation")
	}
	var items []json.RawMessage
	if err = json.Unmarshal(f["profiles"], &items); err != nil {
		return r, err
	}
	if items == nil || len(items) > 100 {
		return r, fmt.Errorf("invalid permission profiles")
	}
	r.Profiles = make([]PreviewPermissionSummary, 0, len(items))
	for _, item := range items {
		v, e := DecodePreviewPermissionSummaryJSON(item, maxBytes)
		if e != nil {
			return PreviewPermissionAdminData{}, e
		}
		r.Profiles = append(r.Profiles, v)
	}
	return r, nil
}
