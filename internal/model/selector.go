package model

import (
	"encoding/json"
	"fmt"
)

type Selector interface{ selectorKind() string }
type WorkspaceSelector struct {
	Kind string `json:"kind"`
}
type SymbolSelector struct {
	Kind     string `json:"kind"`
	SymbolID string `json:"symbol_id"`
}
type ModuleSelector struct {
	Kind     string `json:"kind"`
	ModuleID string `json:"module_id"`
}
type EntitySelector struct {
	Kind     string `json:"kind"`
	EntityID string `json:"entity_id"`
}
type DocumentSelector struct {
	Kind        string    `json:"kind"`
	Path        string    `json:"path"`
	ContentHash string    `json:"content_hash"`
	Range       ByteRange `json:"range"`
}

func (*WorkspaceSelector) selectorKind() string { return "workspace" }
func (*SymbolSelector) selectorKind() string    { return "symbol" }
func (*ModuleSelector) selectorKind() string    { return "module" }
func (*EntitySelector) selectorKind() string    { return "entity" }
func (*PositionSelector) selectorKind() string  { return "source_position" }
func (*DocumentSelector) selectorKind() string  { return "document_anchor" }
func (*RefSelector) selectorKind() string       { return "ref" }

// DecodeSelectorJSON validates the closed selector union without resolving it.
func DecodeSelectorJSON(data []byte, maxBytes int) (Selector, error) {
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return nil, err
	}
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Kind {
	case "source_position":
		v, err := DecodePositionSelectorJSON(data, maxBytes)
		if err != nil {
			return nil, err
		}
		return &v, nil
	case "ref":
		v, err := DecodeRefSelectorJSON(data, maxBytes)
		if err != nil {
			return nil, err
		}
		return &v, nil
	case "workspace":
		if _, err := scopeObject(data, []string{"kind"}, nil); err != nil {
			return nil, err
		}
		return &WorkspaceSelector{Kind: head.Kind}, nil
	case "symbol", "module", "entity":
		field := head.Kind + "_id"
		fields, err := scopeObject(data, []string{"kind", field}, nil)
		if err != nil {
			return nil, err
		}
		var id string
		if err := json.Unmarshal(fields[field], &id); err != nil || !validID(id) {
			return nil, fmt.Errorf("invalid selector ID")
		}
		switch head.Kind {
		case "symbol":
			return &SymbolSelector{Kind: head.Kind, SymbolID: id}, nil
		case "module":
			return &ModuleSelector{Kind: head.Kind, ModuleID: id}, nil
		default:
			return &EntitySelector{Kind: head.Kind, EntityID: id}, nil
		}
	case "document_anchor":
		fields, err := scopeObject(data, []string{"kind", "path", "content_hash", "range"}, nil)
		if err != nil {
			return nil, err
		}
		if _, err := scopeObject(fields["range"], []string{"start_byte", "end_byte"}, nil); err != nil {
			return nil, err
		}
		var r DocumentSelector
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		if !validSourcePath(r.Path) || !lowerHex(r.ContentHash, 64) || r.Range.StartByte < 0 || r.Range.EndByte < r.Range.StartByte || r.Range.EndByte > maxExactInteger {
			return nil, fmt.Errorf("invalid document selector")
		}
		return &r, nil
	default:
		return nil, fmt.Errorf("unknown selector kind")
	}
}
