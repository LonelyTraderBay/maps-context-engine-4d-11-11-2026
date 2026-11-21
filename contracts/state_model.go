package contracts

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

const StateModelMaxBytes = 128 << 10

//go:embed state-model.v1.schema.json
var stateModelSchema []byte

var ErrStateModelSchema = errors.New("state model source does not conform to mce4d-state-model-v1")

var resolveStateModelSchema = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(stateModelSchema, &schema); err != nil {
		return nil, err
	}
	return schema.Resolve(nil)
})

// ValidateStateModelSource checks the closed source format without loading
// external references or returning raw source in validation errors.
func ValidateStateModelSource(raw []byte) error {
	if _, err := model.DecodeIdentityJSON(raw, StateModelMaxBytes); err != nil {
		return ErrStateModelSchema
	}
	schema, err := resolveStateModelSchema()
	if err != nil {
		return ErrStateModelSchema
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || schema.Validate(value) != nil {
		return ErrStateModelSchema
	}
	return nil
}
