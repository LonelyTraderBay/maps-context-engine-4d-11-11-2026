package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type RuntimeConfigRequest struct {
	Action            string
	Config            *RuntimeConfigPatch
	ExpectedDigest    *string
	ExpectedDigestSet bool
	Effective         *RuntimeConfig
}

// MarshalJSON preserves the distinction between absent expected_digest and null
// expected_digest, which represents an explicitly expected absent config file.
func (r RuntimeConfigRequest) MarshalJSON() ([]byte, error) {
	if r.ExpectedDigestSet {
		return json.Marshal(struct {
			Action         string              `json:"action"`
			Config         *RuntimeConfigPatch `json:"config,omitempty"`
			ExpectedDigest *string             `json:"expected_digest"`
		}{r.Action, r.Config, r.ExpectedDigest})
	}
	return json.Marshal(struct {
		Action string              `json:"action"`
		Config *RuntimeConfigPatch `json:"config,omitempty"`
	}{r.Action, r.Config})
}

// DecodeRuntimeConfigRequestJSON checks request shape and expected digest, but
// does not read a file, authorize a caller or perform the actual CAS.
func DecodeRuntimeConfigRequestJSON(data []byte, maxBytes, availableCPUs int) (RuntimeConfigRequest, error) {
	var r RuntimeConfigRequest
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return r, err
	}
	if fields == nil {
		return r, fmt.Errorf("expected object")
	}
	for name, raw := range fields {
		switch name {
		case "action", "config", "expected_digest":
		default:
			return r, fmt.Errorf("unknown config request field")
		}
		if name != "expected_digest" && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return r, fmt.Errorf("null config request field")
		}
	}
	action, ok := fields["action"]
	if !ok {
		return r, fmt.Errorf("missing action")
	}
	if err := json.Unmarshal(action, &r.Action); err != nil {
		return r, err
	}
	switch r.Action {
	case "show", "validate", "set":
	default:
		return r, fmt.Errorf("invalid config action")
	}
	if raw, ok := fields["expected_digest"]; ok {
		r.ExpectedDigestSet = true
		if err := json.Unmarshal(raw, &r.ExpectedDigest); err != nil {
			return RuntimeConfigRequest{}, err
		}
		if r.ExpectedDigest != nil && !lowerHex(*r.ExpectedDigest, 64) {
			return RuntimeConfigRequest{}, fmt.Errorf("invalid expected digest")
		}
	}
	if raw, ok := fields["config"]; ok {
		effective, err := DecodeRuntimeConfigJSON(raw, maxBytes, availableCPUs)
		if err != nil {
			return RuntimeConfigRequest{}, err
		}
		var patch RuntimeConfigPatch
		if err := json.Unmarshal(raw, &patch); err != nil {
			return RuntimeConfigRequest{}, err
		}
		r.Config = &patch
		r.Effective = &effective
	}
	if r.Action == "show" && r.Config != nil || (r.Action == "validate" || r.Action == "set") && r.Config == nil || r.Action == "set" && !r.ExpectedDigestSet {
		return RuntimeConfigRequest{}, fmt.Errorf("invalid config action fields")
	}
	return r, nil
}
