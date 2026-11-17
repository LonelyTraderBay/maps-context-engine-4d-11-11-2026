package contracts

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MCPToolSchemas derives self-contained tool schemas from the sealed S43
// artifacts and the registered operation. It grants no runtime authority and
// does not replace semantic validation by DecodeRequest and DecodeResponse.
func (r *Registry) MCPToolSchemas(name string) (json.RawMessage, json.RawMessage, error) {
	op, err := r.Lookup(MCP, name)
	if err != nil {
		return nil, nil, err
	}
	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if json.Unmarshal(schemaJSON, &schema) != nil {
		return nil, nil, fmt.Errorf("invalid sealed schema")
	}
	decode := func(name string) (map[string]any, error) {
		var result map[string]any
		if data, ok := schema.Defs[name]; !ok || json.Unmarshal(data, &result) != nil || result == nil {
			return nil, fmt.Errorf("missing sealed schema definition")
		}
		return result, nil
	}
	input, err := decode(op.Request)
	if err != nil {
		return nil, nil, err
	}
	// Both alternatives in the sealed RenameRequest are objects. The MCP SDK
	// requires the tool schema itself to declare an object at its root.
	if op.Request == "RenameRequest" && input["type"] == nil {
		for _, name := range []string{"RenameInitialRequest", "RenameContinueRequest"} {
			alternative, err := decode(name)
			if err != nil || alternative["type"] != "object" {
				return nil, nil, fmt.Errorf("MCP rename alternative is not an object")
			}
		}
		input["type"] = "object"
	}
	if input["type"] != "object" {
		return nil, nil, fmt.Errorf("MCP request schema is not an object")
	}
	output, err := decode("Response")
	if err != nil {
		return nil, nil, err
	}
	props, ok := output["properties"].(map[string]any)
	if !ok || output["type"] != "object" || props["data"] == nil {
		return nil, nil, fmt.Errorf("invalid sealed response schema")
	}
	// The operation registry narrows Response.data to its registered result,
	// while errors and pre-index responses may carry null data.
	props["data"] = map[string]any{"anyOf": []any{
		map[string]any{"$ref": "#/$defs/" + op.Response},
		map[string]any{"type": "null"},
	}}
	build := func(root map[string]any) (json.RawMessage, error) {
		defs := map[string]any{}
		var visit func(any) error
		visit = func(value any) error {
			switch v := value.(type) {
			case map[string]any:
				if ref, exists := v["$ref"]; exists {
					text, ok := ref.(string)
					const prefix = "#/$defs/"
					if !ok || !strings.HasPrefix(text, prefix) || strings.Contains(text[len(prefix):], "/") {
						return fmt.Errorf("invalid sealed schema reference")
					}
					key := text[len(prefix):]
					if _, seen := defs[key]; !seen {
						definition, err := decode(key)
						if err != nil {
							return err
						}
						defs[key] = definition
						if err := visit(definition); err != nil {
							return err
						}
					}
				}
				for _, child := range v {
					if err := visit(child); err != nil {
						return err
					}
				}
			case []any:
				for _, child := range v {
					if err := visit(child); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := visit(root); err != nil {
			return nil, err
		}
		root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		root["$defs"] = defs
		return json.Marshal(root)
	}
	in, err := build(input)
	if err != nil {
		return nil, nil, err
	}
	out, err := build(output)
	return in, out, err
}
