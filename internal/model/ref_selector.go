package model

import (
	"encoding/json"
	"fmt"
)

// RefSelector retains the selected reference and optional service qualifier.
type RefSelector struct {
	Kind      string   `json:"kind"`
	Ref       GraphRef `json:"ref"`
	ServiceID *string  `json:"service_id"`
}

func DecodeRefSelectorJSON(data []byte, maxBytes int) (RefSelector, error) {
	var result RefSelector
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return result, err
	}
	fields, err := scopeObject(data, []string{"kind", "ref", "service_id"}, map[string]bool{"service_id": true})
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(fields["kind"], &result.Kind); err != nil {
		return RefSelector{}, err
	}
	if result.Kind != "ref" {
		return RefSelector{}, fmt.Errorf("invalid ref selector kind")
	}
	result.Ref, err = DecodeGraphRefJSON(fields["ref"], maxBytes)
	if err != nil {
		return RefSelector{}, err
	}
	if err := json.Unmarshal(fields["service_id"], &result.ServiceID); err != nil {
		return RefSelector{}, err
	}
	if !optionalID(result.ServiceID) {
		return RefSelector{}, fmt.Errorf("invalid service id")
	}
	return result, nil
}
