package model

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type RenameRequest interface{ renameRequest() }
type RenameInitialRequest struct {
	Scope             *ScopeRequest    `json:"scope,omitempty"`
	Selector          PositionSelector `json:"selector"`
	NewName           string           `json:"new_name"`
	PreserveContracts bool             `json:"preserve_contracts"`
	MaxOutputBytes    *int64           `json:"max_output_bytes,omitempty"`
}
type RenameContinueRequest struct {
	ResultToken    string `json:"result_token"`
	Cursor         string `json:"cursor"`
	MaxOutputBytes *int64 `json:"max_output_bytes,omitempty"`
}

func (*RenameInitialRequest) renameRequest()  {}
func (*RenameContinueRequest) renameRequest() {}
func validOutputCap(n *int64) bool            { return n == nil || (*n >= 4096 && *n <= 131072) }

// DecodeRenameRequestJSON preserves the distinction between initial work and
// continuation. It neither runs an LSP nor verifies result tokens or freshness.
func DecodeRenameRequestJSON(data []byte, maxBytes int) (RenameRequest, error) {
	fields, err := optionalObject(data, maxBytes, "scope", "selector", "new_name", "preserve_contracts", "max_output_bytes", "result_token", "cursor")
	if err != nil {
		return nil, err
	}
	_, initial := fields["selector"]
	_, continuation := fields["result_token"]
	if initial == continuation {
		return nil, fmt.Errorf("expected exactly one rename mode")
	}
	if continuation {
		for key := range fields {
			switch key {
			case "result_token", "cursor", "max_output_bytes":
			default:
				return nil, fmt.Errorf("unexpected continuation field")
			}
		}
		if _, ok := fields["cursor"]; !ok {
			return nil, fmt.Errorf("missing cursor")
		}
		var r RenameContinueRequest
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		if !validToken(r.ResultToken) || !validToken(r.Cursor) || !validOutputCap(r.MaxOutputBytes) {
			return nil, fmt.Errorf("invalid continuation")
		}
		return &r, nil
	}
	for key := range fields {
		switch key {
		case "scope", "selector", "new_name", "preserve_contracts", "max_output_bytes":
		default:
			return nil, fmt.Errorf("unexpected initial field")
		}
	}
	if _, ok := fields["new_name"]; !ok {
		return nil, fmt.Errorf("missing rename name")
	}
	selector, err := DecodePositionSelectorJSON(fields["selector"], maxBytes)
	if err != nil {
		return nil, err
	}
	delete(fields, "selector")
	scope, err := decodeOptionalScope(fields, maxBytes)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	r := RenameInitialRequest{PreserveContracts: true}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	r.Scope = scope
	r.Selector = selector
	if !utf8.ValidString(r.NewName) || utf8.RuneCountInString(r.NewName) < 1 || utf8.RuneCountInString(r.NewName) > 256 || !validOutputCap(r.MaxOutputBytes) {
		return nil, fmt.Errorf("invalid rename request")
	}
	return &r, nil
}
