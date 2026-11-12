package model

import (
	"encoding/json"
	"fmt"
)

// RuntimeConfigDigest hashes an effective config after CPU sentinel resolution;
// it does not authorize resource use or prove a config file was persisted.
func RuntimeConfigDigest(config RuntimeConfig, maxBytes int) (string, error) {
	if err := validateRuntimeConfig(config); err != nil {
		return "", err
	}
	if config.Core.MaxParseWorkers == 0 {
		return "", fmt.Errorf("CPU sentinel is unresolved")
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	value, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("runtime-config-v1", value, maxBytes)
	return digest, err
}
