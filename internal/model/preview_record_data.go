package model

import (
	"encoding/json"
	"fmt"
)

type PreviewData struct {
	Operation          string  `json:"operation"`
	SessionID          string  `json:"session_id"`
	Revision           int64   `json:"revision"`
	DocumentEpoch      int64   `json:"document_epoch"`
	HMREpoch           int64   `json:"hmr_epoch"`
	AuthorizationEpoch int64   `json:"authorization_epoch"`
	State              string  `json:"state"`
	ModeState          string  `json:"mode_state"`
	PairingNonce       *string `json:"pairing_nonce"`
	CommandID          *string `json:"command_id"`
	ExpiresAt          string  `json:"expires_at"`
	BindingID          *string `json:"binding_id"`
	PermissionVersion  int64   `json:"permission_version"`
	ResolverDigest     *string `json:"resolver_digest"`
}
type RecordData struct {
	Operation       string `json:"operation"`
	RecordID        string `json:"record_id"`
	Version         int64  `json:"version"`
	RecordEpoch     int64  `json:"record_epoch"`
	DeclaredStatus  string `json:"declared_status"`
	EffectiveStatus string `json:"effective_status"`
}

// The values decoded here are wire claims, not proof of state transitions or authorization.
func DecodePreviewDataJSON(data []byte, maxBytes int) (PreviewData, error) {
	var r PreviewData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"operation", "session_id", "revision", "document_epoch", "hmr_epoch", "authorization_epoch", "state", "mode_state", "pairing_nonce", "command_id", "expires_at", "binding_id", "permission_version", "resolver_digest"}, map[string]bool{"pairing_nonce": true, "command_id": true, "binding_id": true, "resolver_digest": true}); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return PreviewData{}, err
	}
	if !uuid(r.SessionID) || !positiveVersion(r.Revision) || !positiveVersion(r.DocumentEpoch) || !nonnegativeVersion(r.HMREpoch) || !nonnegativeVersion(r.AuthorizationEpoch) || !positiveVersion(r.PermissionVersion) || r.PairingNonce != nil && !boundedString(*r.PairingNonce, 64) || r.CommandID != nil && !uuid(*r.CommandID) || r.BindingID != nil && !lowerHex(*r.BindingID, 64) || r.ResolverDigest != nil && !lowerHex(*r.ResolverDigest, 64) {
		return PreviewData{}, fmt.Errorf("invalid preview response")
	}
	switch r.Operation {
	case "preview_attach", "preview_bind", "preview_mode", "preview_ack", "preview_invalidate", "preview_detach":
	default:
		return PreviewData{}, fmt.Errorf("invalid preview operation")
	}
	switch r.State {
	case "pairing", "bound", "stale", "detached", "blocked":
	default:
		return PreviewData{}, fmt.Errorf("invalid preview state")
	}
	switch r.ModeState {
	case "inspect", "arming", "interact", "disarming":
	default:
		return PreviewData{}, fmt.Errorf("invalid preview mode")
	}
	if _, err := utcTimestamp(r.ExpiresAt); err != nil {
		return PreviewData{}, fmt.Errorf("invalid preview expiry")
	}
	return r, nil
}
func DecodeRecordDataJSON(data []byte, maxBytes int) (RecordData, error) {
	var r RecordData
	if err := decodeClosedRequest(data, maxBytes, []string{"operation", "record_id", "version", "record_epoch", "declared_status", "effective_status"}, &r); err != nil {
		return RecordData{}, err
	}
	if (r.Operation != "mapping_propose" && r.Operation != "mapping_review") || !uuid(r.RecordID) || !positiveVersion(r.Version) || !nonnegativeVersion(r.RecordEpoch) {
		return RecordData{}, fmt.Errorf("invalid record response")
	}
	switch r.DeclaredStatus {
	case "proposed", "approved", "revoked", "superseded":
	default:
		return RecordData{}, fmt.Errorf("invalid declared status")
	}
	switch r.EffectiveStatus {
	case "proposed", "approved", "revoked", "superseded", "needs_review":
	default:
		return RecordData{}, fmt.Errorf("invalid effective status")
	}
	if r.EffectiveStatus == "approved" && r.DeclaredStatus != "approved" {
		return RecordData{}, fmt.Errorf("record approval inconsistent with declared status")
	}
	return r, nil
}
