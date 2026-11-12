package model

import (
	"encoding/json"
	"fmt"
	"time"
)

type PreviewStatusData struct {
	Operation           string  `json:"operation"`
	SessionID           string  `json:"session_id"`
	Revision            int64   `json:"revision"`
	AuthorizationEpoch  int64   `json:"authorization_epoch"`
	DocumentEpoch       int64   `json:"document_epoch"`
	HMREpoch            int64   `json:"hmr_epoch"`
	State               string  `json:"state"`
	ModeState           string  `json:"mode_state"`
	BindingID           *string `json:"binding_id"`
	PermissionProfileID string  `json:"permission_profile_id"`
	PermissionVersion   int64   `json:"permission_version"`
	PermissionStatus    string  `json:"permission_status"`
	ExpiresAt           string  `json:"expires_at"`
	ServerTime          string  `json:"server_time"`
	InteractionAllowed  bool    `json:"interaction_allowed"`
}

func utcTimestamp(s string) (time.Time, error) {
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	_, offset := v.Zone()
	if offset != 0 {
		return time.Time{}, fmt.Errorf("timestamp must be UTC")
	}
	return v, nil
}

// DecodePreviewStatusDataJSON checks a status response's shape and fail-closed
// fields. It does not prove the SDK is alive or that the permission is current.
func DecodePreviewStatusDataJSON(data []byte, maxBytes int) (PreviewStatusData, error) {
	var r PreviewStatusData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	_, err := scopeObject(data, []string{"operation", "session_id", "revision", "authorization_epoch", "document_epoch", "hmr_epoch", "state", "mode_state", "binding_id", "permission_profile_id", "permission_version", "permission_status", "expires_at", "server_time", "interaction_allowed"}, map[string]bool{"binding_id": true})
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return PreviewStatusData{}, err
	}
	if r.Operation != "preview_status" || !uuid(r.SessionID) || r.Revision < 1 || r.Revision > maxExactInteger || r.AuthorizationEpoch < 0 || r.AuthorizationEpoch > maxExactInteger || r.DocumentEpoch < 1 || r.DocumentEpoch > maxExactInteger || r.HMREpoch < 0 || r.HMREpoch > maxExactInteger || !validID(r.PermissionProfileID) || r.PermissionVersion < 1 || r.PermissionVersion > maxExactInteger || (r.BindingID != nil && !lowerHex(*r.BindingID, 64)) {
		return PreviewStatusData{}, fmt.Errorf("invalid preview status fields")
	}
	switch r.State {
	case "pairing", "bound", "stale", "detached", "blocked":
	default:
		return PreviewStatusData{}, fmt.Errorf("invalid preview state")
	}
	switch r.ModeState {
	case "inspect", "arming", "interact", "disarming":
	default:
		return PreviewStatusData{}, fmt.Errorf("invalid mode state")
	}
	switch r.PermissionStatus {
	case "approved", "revoked", "expired", "unavailable":
	default:
		return PreviewStatusData{}, fmt.Errorf("invalid permission status")
	}
	expiry, err := utcTimestamp(r.ExpiresAt)
	if err != nil {
		return PreviewStatusData{}, err
	}
	now, err := utcTimestamp(r.ServerTime)
	if err != nil {
		return PreviewStatusData{}, err
	}
	if r.InteractionAllowed && (r.State != "bound" || r.PermissionStatus != "approved" || r.BindingID == nil || !now.Before(expiry)) {
		return PreviewStatusData{}, fmt.Errorf("interaction allowed without current bound permission")
	}
	return r, nil
}
