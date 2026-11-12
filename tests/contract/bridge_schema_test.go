package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestBridgeVariantsAgainstLockedSchema(t *testing.T) {
	data, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	var fixture func(json.RawMessage) (any, error)
	fixture = func(raw json.RawMessage) (any, error) {
		var schema struct {
			Ref        string                     `json:"$ref"`
			Type       string                     `json:"type"`
			Const      json.RawMessage            `json:"const"`
			Enum       []json.RawMessage          `json:"enum"`
			AnyOf      []json.RawMessage          `json:"anyOf"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
			Items      json.RawMessage            `json:"items"`
			MinItems   int                        `json:"minItems"`
			Minimum    *float64                   `json:"minimum"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, err
		}
		if len(schema.Const) > 0 {
			var v any
			err := json.Unmarshal(schema.Const, &v)
			return v, err
		}
		if len(schema.Enum) > 0 {
			var v any
			err := json.Unmarshal(schema.Enum[0], &v)
			return v, err
		}
		if schema.Ref != "" {
			name := strings.TrimPrefix(schema.Ref, "#/$defs/")
			switch name {
			case "UUID":
				return id, nil
			case "Hash":
				return h, nil
			case "ID", "Token", "Path":
				return "x", nil
			case "Time":
				return "2026-11-11T00:00:00Z", nil
			}
			target, ok := document.Defs[name]
			if !ok {
				return nil, fmt.Errorf("missing %s", name)
			}
			return fixture(target)
		}
		if len(schema.AnyOf) > 0 {
			return fixture(schema.AnyOf[0])
		}
		switch schema.Type {
		case "string":
			return "x", nil
		case "integer":
			if schema.Minimum != nil {
				return *schema.Minimum, nil
			}
			return 0, nil
		case "number":
			return 0.5, nil
		case "boolean":
			return false, nil
		case "array":
			items := make([]any, 0, schema.MinItems)
			for i := 0; i < schema.MinItems; i++ {
				v, err := fixture(schema.Items)
				if err != nil {
					return nil, err
				}
				items = append(items, v)
			}
			return items, nil
		case "object":
			obj := map[string]any{}
			for _, key := range schema.Required {
				v, err := fixture(schema.Properties[key])
				if err != nil {
					return nil, err
				}
				obj[key] = v
			}
			return obj, nil
		case "null":
			return nil, nil
		}
		return nil, fmt.Errorf("unsupported fixture schema")
	}
	count := 0
	for name, definition := range document.Defs {
		if !strings.HasPrefix(name, "Bridge_") {
			continue
		}
		count++
		t.Run(name, func(t *testing.T) {
			v, err := fixture(definition)
			if err != nil {
				t.Fatal(err)
			}
			var schema jsonschema.Schema
			if err := json.Unmarshal(data, &schema); err != nil {
				t.Fatal(err)
			}
			schema.Ref = "#/$defs/" + name
			resolved, err := schema.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := resolved.Validate(v); err != nil {
				t.Fatalf("fixture invalid: %v", err)
			}
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			msg, err := model.DecodeBridgeMessageJSON(raw, 16384)
			if err != nil {
				t.Fatalf("model rejected %s: %v", raw, err)
			}
			direction := model.ParentToSDK
			if !model.BridgeDirectionAllowed(msg.Type, direction) {
				direction = model.SDKToParent
			}
			if _, err := model.DecodeBridgeMessageForDirectionJSON(raw, 16384, direction); err != nil {
				t.Fatalf("allowed direction %s: %v", name, err)
			}
			opposite := model.ParentToSDK
			if direction == model.ParentToSDK {
				opposite = model.SDKToParent
			}
			_, directionErr := model.DecodeBridgeMessageForDirectionJSON(raw, 16384, opposite)
			if (msg.Type == "ERROR") != (directionErr == nil) {
				t.Fatalf("wrong opposite direction behavior for %s: %v", name, directionErr)
			}
			encoded, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := model.DecodeBridgeMessageJSON(encoded, 16384); err != nil {
				t.Fatalf("roundtrip %s: %v", encoded, err)
			}
		})
	}
	if count != 23 {
		t.Fatalf("got %d bridge variants", count)
	}
}
