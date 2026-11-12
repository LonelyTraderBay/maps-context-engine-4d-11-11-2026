package model

import (
	"encoding/json"
	"fmt"
)

type PreviewStatusRequest struct {
	SessionID string `json:"session_id"`
}

// A status request neither refreshes the session nor proves SDK liveness.
func DecodePreviewStatusRequestJSON(data []byte, maxBytes int) (PreviewStatusRequest, error) {
	var r PreviewStatusRequest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"session_id"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return PreviewStatusRequest{}, err
	}
	if !uuid(r.SessionID) {
		return PreviewStatusRequest{}, fmt.Errorf("invalid session id")
	}
	return r, nil
}
