package model

import (
	"encoding/json"
	"fmt"
)

type BootstrapRequest struct {
	Code string `json:"code"`
}
type BootstrapData struct {
	Operation    string   `json:"operation"`
	Bearer       string   `json:"bearer"`
	ExpiresAt    string   `json:"expires_at"`
	Capabilities []string `json:"capabilities"`
}

// These functions validate wire shape only; they do not authenticate a code,
// issue a bearer, authorize capabilities, or check expiry against server time.
func DecodeBootstrapRequestJSON(data []byte, maxBytes int) (BootstrapRequest, error) {
	var r BootstrapRequest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"code"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return BootstrapRequest{}, err
	}
	if !boundedString(r.Code, 64) {
		return BootstrapRequest{}, fmt.Errorf("invalid bootstrap code")
	}
	return r, nil
}
func DecodeBootstrapDataJSON(data []byte, maxBytes int) (BootstrapData, error) {
	var r BootstrapData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"operation", "bearer", "expires_at", "capabilities"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return BootstrapData{}, err
	}
	if r.Operation != "bootstrap" || !boundedString(r.Bearer, 64) || r.Capabilities == nil || len(r.Capabilities) > 64 {
		return BootstrapData{}, fmt.Errorf("invalid bootstrap data")
	}
	if _, err := utcTimestamp(r.ExpiresAt); err != nil {
		return BootstrapData{}, fmt.Errorf("invalid expiry")
	}
	for _, c := range r.Capabilities {
		if !validID(c) {
			return BootstrapData{}, fmt.Errorf("invalid capability")
		}
	}
	return r, nil
}
