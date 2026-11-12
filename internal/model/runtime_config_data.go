package model

import (
	"encoding/json"
	"fmt"
)

type RuntimeConfigData struct {
	Operation       string        `json:"operation"`
	Config          RuntimeConfig `json:"config"`
	ConfigDigest    string        `json:"config_digest"`
	RestartRequired bool          `json:"restart_required"`
}

// CPU count must be the producer's resolved count when the wire config uses
// the CPU sentinel; validating this response does not persist or apply config.
func DecodeRuntimeConfigDataJSON(data []byte, maxBytes, availableCPUs int) (RuntimeConfigData, error) {
	var r RuntimeConfigData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "config", "config_digest", "restart_required"}, nil)
	if err != nil {
		return r, err
	}
	r.Config, err = DecodeRuntimeConfigJSON(f["config"], maxBytes, availableCPUs)
	if err != nil {
		return RuntimeConfigData{}, err
	}
	delete(f, "config")
	raw, err := json.Marshal(f)
	if err != nil {
		return RuntimeConfigData{}, err
	}
	var body RuntimeConfigData
	if err = json.Unmarshal(raw, &body); err != nil {
		return RuntimeConfigData{}, err
	}
	body.Config = r.Config
	if body.Operation != "runtime_config" || !lowerHex(body.ConfigDigest, 64) {
		return RuntimeConfigData{}, fmt.Errorf("invalid runtime config response")
	}
	digest, err := RuntimeConfigDigest(body.Config, maxBytes)
	if err != nil || body.ConfigDigest != digest {
		return RuntimeConfigData{}, fmt.Errorf("runtime config digest mismatch")
	}
	return body, nil
}
