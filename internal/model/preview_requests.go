package model

import (
	"encoding/json"
	"fmt"
)

type PreviewAttachRequest struct {
	Scope               *ScopeRequest `json:"scope,omitempty"`
	Origin              string        `json:"origin"`
	Path                string        `json:"path"`
	BuildMapID          string        `json:"build_map_id"`
	PermissionProfileID string        `json:"permission_profile_id"`
}
type PreviewBindRequest struct {
	SessionID        string   `json:"session_id"`
	ExpectedRevision int64    `json:"expected_revision"`
	DocumentEpoch    int64    `json:"document_epoch"`
	HMREpoch         int64    `json:"hmr_epoch"`
	BuildMapID       string   `json:"build_map_id"`
	PairingNonce     string   `json:"pairing_nonce"`
	Capabilities     []string `json:"capabilities"`
}
type PreviewModeRequest struct {
	SessionID                  string `json:"session_id"`
	ExpectedRevision           int64  `json:"expected_revision"`
	ExpectedAuthorizationEpoch int64  `json:"expected_authorization_epoch"`
	Mode                       string `json:"mode"`
	PermissionProfileID        string `json:"permission_profile_id"`
}
type PreviewAckRequest struct {
	SessionID          string `json:"session_id"`
	ExpectedRevision   int64  `json:"expected_revision"`
	AuthorizationEpoch int64  `json:"authorization_epoch"`
	CommandID          string `json:"command_id"`
	Acknowledged       bool   `json:"acknowledged"`
}
type PreviewInvalidateRequest struct {
	SessionID        string `json:"session_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}
type PreviewDetachRequest struct {
	SessionID string `json:"session_id"`
}

func positiveVersion(n int64) bool    { return n >= 1 && n <= maxExactInteger }
func nonnegativeVersion(n int64) bool { return n >= 0 && n <= maxExactInteger }
func decodeClosedRequest(data []byte, maxBytes int, names []string, dest any) error {
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return err
	}
	if _, err := scopeObject(data, names, nil); err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// These decoders validate wire syntax. Origin matching, permission, nonce, CAS,
// state transition, liveness and replay checks belong to the owning adapter.
func DecodePreviewAttachRequestJSON(data []byte, maxBytes int) (PreviewAttachRequest, error) {
	var r PreviewAttachRequest
	f, err := optionalObject(data, maxBytes, "scope", "origin", "path", "build_map_id", "permission_profile_id")
	if err != nil {
		return r, err
	}
	for _, k := range []string{"origin", "path", "build_map_id", "permission_profile_id"} {
		if _, ok := f[k]; !ok {
			return r, fmt.Errorf("missing attach field")
		}
	}
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return PreviewAttachRequest{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return PreviewAttachRequest{}, err
	}
	var body PreviewAttachRequest
	if err = json.Unmarshal(raw, &body); err != nil {
		return PreviewAttachRequest{}, err
	}
	body.Scope = r.Scope
	if !boundedString(body.Origin, 2048) || !boundedString(body.Path, 2048) || !lowerHex(body.BuildMapID, 64) || !validID(body.PermissionProfileID) {
		return PreviewAttachRequest{}, fmt.Errorf("invalid attach")
	}
	return body, nil
}
func DecodePreviewBindRequestJSON(data []byte, maxBytes int) (PreviewBindRequest, error) {
	var r PreviewBindRequest
	if err := decodeClosedRequest(data, maxBytes, []string{"session_id", "expected_revision", "document_epoch", "hmr_epoch", "build_map_id", "pairing_nonce", "capabilities"}, &r); err != nil {
		return PreviewBindRequest{}, err
	}
	if !uuid(r.SessionID) || !positiveVersion(r.ExpectedRevision) || !positiveVersion(r.DocumentEpoch) || !nonnegativeVersion(r.HMREpoch) || !lowerHex(r.BuildMapID, 64) || !boundedString(r.PairingNonce, 64) || r.Capabilities == nil || len(r.Capabilities) > 64 {
		return PreviewBindRequest{}, fmt.Errorf("invalid bind")
	}
	for _, c := range r.Capabilities {
		if !validID(c) {
			return PreviewBindRequest{}, fmt.Errorf("invalid capability")
		}
	}
	return r, nil
}
func DecodePreviewModeRequestJSON(data []byte, maxBytes int) (PreviewModeRequest, error) {
	var r PreviewModeRequest
	if err := decodeClosedRequest(data, maxBytes, []string{"session_id", "expected_revision", "expected_authorization_epoch", "mode", "permission_profile_id"}, &r); err != nil {
		return PreviewModeRequest{}, err
	}
	if !uuid(r.SessionID) || !positiveVersion(r.ExpectedRevision) || !nonnegativeVersion(r.ExpectedAuthorizationEpoch) || (r.Mode != "inspect" && r.Mode != "interact") || !validID(r.PermissionProfileID) {
		return PreviewModeRequest{}, fmt.Errorf("invalid mode")
	}
	return r, nil
}
func DecodePreviewAckRequestJSON(data []byte, maxBytes int) (PreviewAckRequest, error) {
	var r PreviewAckRequest
	if err := decodeClosedRequest(data, maxBytes, []string{"session_id", "expected_revision", "authorization_epoch", "command_id", "acknowledged"}, &r); err != nil {
		return PreviewAckRequest{}, err
	}
	if !uuid(r.SessionID) || !uuid(r.CommandID) || !positiveVersion(r.ExpectedRevision) || !nonnegativeVersion(r.AuthorizationEpoch) {
		return PreviewAckRequest{}, fmt.Errorf("invalid acknowledgement")
	}
	return r, nil
}
func DecodePreviewInvalidateRequestJSON(data []byte, maxBytes int) (PreviewInvalidateRequest, error) {
	var r PreviewInvalidateRequest
	if err := decodeClosedRequest(data, maxBytes, []string{"session_id", "expected_revision", "reason"}, &r); err != nil {
		return PreviewInvalidateRequest{}, err
	}
	if !uuid(r.SessionID) || !positiveVersion(r.ExpectedRevision) {
		return PreviewInvalidateRequest{}, fmt.Errorf("invalid invalidation")
	}
	switch r.Reason {
	case "hmr", "navigation", "heartbeat_lost", "policy_changed", "build_changed", "client_error":
	default:
		return PreviewInvalidateRequest{}, fmt.Errorf("invalid invalidation reason")
	}
	return r, nil
}
func DecodePreviewDetachRequestJSON(data []byte, maxBytes int) (PreviewDetachRequest, error) {
	var r PreviewDetachRequest
	if err := decodeClosedRequest(data, maxBytes, []string{"session_id"}, &r); err != nil {
		return PreviewDetachRequest{}, err
	}
	if !uuid(r.SessionID) {
		return PreviewDetachRequest{}, fmt.Errorf("invalid detach")
	}
	return r, nil
}
