package contract

import (
	"encoding/json"
	"fmt"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
	"os"
	"strings"
	"testing"
)

func TestAllResponseDataVariantsAgainstSchema(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	var fixture func(json.RawMessage) (any, error)
	fixture = func(b json.RawMessage) (any, error) {
		var s struct {
			Ref        string                     `json:"$ref"`
			Type       string                     `json:"type"`
			Const      json.RawMessage            `json:"const"`
			Enum       []json.RawMessage          `json:"enum"`
			OneOf      []json.RawMessage          `json:"oneOf"`
			AnyOf      []json.RawMessage          `json:"anyOf"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
			Items      json.RawMessage            `json:"items"`
			MinItems   int                        `json:"minItems"`
			Minimum    *float64                   `json:"minimum"`
		}
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, err
		}
		if len(s.Const) > 0 {
			var v any
			err := json.Unmarshal(s.Const, &v)
			return v, err
		}
		if len(s.Enum) > 0 {
			var v any
			err := json.Unmarshal(s.Enum[0], &v)
			return v, err
		}
		if s.Ref != "" {
			name := strings.TrimPrefix(s.Ref, "#/$defs/")
			switch name {
			case "UUID":
				return id, nil
			case "Hash":
				return hash, nil
			case "ID", "Token", "Path":
				return "x", nil
			case "Time":
				return "2026-11-11T00:00:00Z", nil
			}
			if d, ok := doc.Defs[name]; ok {
				return fixture(d)
			}
			return nil, fmt.Errorf("unknown ref %s", name)
		}
		if len(s.OneOf) > 0 && s.Type != "object" {
			return fixture(s.OneOf[0])
		}
		if len(s.AnyOf) > 0 {
			return fixture(s.AnyOf[0])
		}
		switch s.Type {
		case "string":
			return "x", nil
		case "integer":
			if s.Minimum != nil {
				return *s.Minimum, nil
			}
			return 0, nil
		case "boolean":
			return false, nil
		case "object":
			m := map[string]any{}
			for _, k := range s.Required {
				v, err := fixture(s.Properties[k])
				if err != nil {
					return nil, err
				}
				m[k] = v
			}
			return m, nil
		case "array":
			a := make([]any, 0, s.MinItems)
			for i := 0; i < s.MinItems; i++ {
				v, err := fixture(s.Items)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			return a, nil
		case "null":
			return nil, nil
		}
		return nil, fmt.Errorf("unsupported schema")
	}
	names := []string{"StatusData", "LocateData", "PackData", "ValidateData", "RenameData", "GraphData", "SourceData", "MappingData", "FlowData", "BootstrapData", "PreviewData", "RecordData", "PreviewStatusData", "PreviewPermissionAdminData", "GroupAdminData", "RuntimeConfigData", "RecordListData", "RecordReadData", "ProfileAdminData", "PolicyAdminData"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			value, err := fixture(doc.Defs[name])
			if err != nil {
				t.Fatal(err)
			}
			m := value.(map[string]any)
			if name == "SourceData" {
				m["content"] = ""
			}
			if name == "RecordReadData" {
				m["ui_mapping"] = nil
			}
			if name == "RuntimeConfigData" {
				config, err := model.DecodeRuntimeConfigJSON([]byte(`{"schema_version":1}`), 16384, 4)
				if err != nil {
					t.Fatal(err)
				}
				m["config_digest"], err = model.RuntimeConfigDigest(config, 16384)
				if err != nil {
					t.Fatal(err)
				}
			}
			var schema jsonschema.Schema
			if err = json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			schema.Ref = "#/$defs/" + name
			compiled, err := schema.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = compiled.Validate(m); err != nil {
				t.Fatalf("schema fixture invalid: %v", err)
			}
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			v, err := model.DecodeResponseDataJSON(b, 32768, 4)
			if err != nil || v == nil {
				t.Fatalf("model rejected %s: %v", b, err)
			}
			m["unexpected_field"] = true
			if err = compiled.Validate(m); err == nil {
				t.Fatal("schema accepted unknown field")
			}
			b, _ = json.Marshal(m)
			if v, err = model.DecodeResponseDataJSON(b, 32768, 4); err == nil || v != nil {
				t.Fatal("model accepted unknown field")
			}
		})
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, op := range registry.Operations() {
		name := op.Request
		if seen[name] {
			continue
		}
		seen[name] = true
		t.Run("Request/"+name, func(t *testing.T) {
			value, err := fixture(doc.Defs[name])
			if err != nil {
				t.Fatal(err)
			}
			m := value.(map[string]any)
			switch name {
			case "LocateRequest":
				m["query"] = "item"
			case "ValidateRequest":
				m["pack_token"] = "token"
			case "PreviewPermissionAdminRequest":
				m["profile_id"] = "dev"
			case "GroupAdminRequest":
				m["group_id"] = id
			}
			var schema jsonschema.Schema
			if err = json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			schema.Ref = "#/$defs/" + name
			compiled, err := schema.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = compiled.Validate(m); err != nil {
				t.Fatalf("schema fixture invalid: %v", err)
			}
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			v, err := model.DecodeRequestJSON(name, b, 32768, 4)
			if err != nil || v == nil {
				t.Fatalf("model rejected %s: %v", b, err)
			}
			m["unexpected_field"] = true
			if err = compiled.Validate(m); err == nil {
				t.Fatal("schema accepted unknown field")
			}
			b, _ = json.Marshal(m)
			if v, err = model.DecodeRequestJSON(name, b, 32768, 4); err == nil || v != nil {
				t.Fatal("model accepted unknown field")
			}
		})
	}

}
