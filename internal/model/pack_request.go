package model

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type PackRequest struct {
	Scope          *ScopeRequest `json:"scope,omitempty"`
	Selector       Selector      `json:"selector"`
	View           *string       `json:"view,omitempty"`
	TaskKind       string        `json:"task_kind"`
	Intent         *string       `json:"intent,omitempty"`
	Constraints    []string      `json:"constraints,omitempty"`
	FreshnessMode  string        `json:"freshness_mode"`
	MaxOutputBytes *int64        `json:"max_output_bytes,omitempty"`
}

// DecodePackRequestJSON establishes wire shape and documented defaults; the
// application still resolves scope, capabilities, target and freshness authority.
func DecodePackRequestJSON(data []byte, maxBytes int) (PackRequest, error) {
	var r PackRequest
	fields, err := optionalObject(data, maxBytes, "scope", "selector", "view", "task_kind", "intent", "constraints", "freshness_mode", "max_output_bytes")
	if err != nil {
		return r, err
	}
	rawSelector, ok := fields["selector"]
	if !ok {
		return r, fmt.Errorf("missing selector")
	}
	if _, ok := fields["task_kind"]; !ok {
		return r, fmt.Errorf("missing task kind")
	}
	r.Selector, err = DecodeSelectorJSON(rawSelector, maxBytes)
	if err != nil {
		return PackRequest{}, err
	}
	delete(fields, "selector")
	r.Scope, err = decodeOptionalScope(fields, maxBytes)
	if err != nil {
		return PackRequest{}, err
	}
	_, hasFreshness := fields["freshness_mode"]
	raw, err := json.Marshal(fields)
	if err != nil {
		return PackRequest{}, err
	}
	var body PackRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return PackRequest{}, err
	}
	body.Selector = r.Selector
	body.Scope = r.Scope
	switch body.TaskKind {
	case "explore", "plan":
		if !hasFreshness {
			body.FreshnessMode = "analysis_snapshot"
		}
	case "rename", "type_change", "behavior_change", "contract_change", "debug":
		if !hasFreshness {
			body.FreshnessMode = "checked_workspace"
		}
	default:
		return PackRequest{}, fmt.Errorf("invalid task kind")
	}
	if body.FreshnessMode != "analysis_snapshot" && body.FreshnessMode != "checked_workspace" {
		return PackRequest{}, fmt.Errorf("invalid freshness mode")
	}
	if body.View == nil {
		var view string
		switch body.Selector.selectorKind() {
		case "symbol", "source_position":
			view = "symbol"
		case "workspace", "module", "document_anchor":
			view = "overview"
		case "ref":
			ref := body.Selector.(*RefSelector)
			switch ref.Ref.(type) {
			case *SymbolRef:
				view = "symbol"
			case *ModuleRef:
				view = "overview"
			}
		}
		if view != "" {
			body.View = &view
		}
	}
	if body.View != nil {
		switch *body.View {
		case "overview", "symbol", "flow", "state", "contract", "change", "architecture", "ai_lineage":
		default:
			return PackRequest{}, fmt.Errorf("invalid view")
		}
	}
	if body.Intent != nil && (!utf8.ValidString(*body.Intent) || utf8.RuneCountInString(*body.Intent) > 2048) {
		return PackRequest{}, fmt.Errorf("invalid intent")
	}
	if len(body.Constraints) > 16 || !validOutputCap(body.MaxOutputBytes) {
		return PackRequest{}, fmt.Errorf("invalid constraints or budget")
	}
	for _, c := range body.Constraints {
		switch c {
		case "preserve_json_keys", "preserve_db_schema", "no_public_api_change", "preserve_message_schema", "no_source_mutation":
		default:
			return PackRequest{}, fmt.Errorf("unknown constraint")
		}
	}
	if body.TaskKind == "rename" {
		switch sel := body.Selector.(type) {
		case *SymbolSelector, *PositionSelector:
		case *RefSelector:
			if _, ok := sel.Ref.(*SymbolRef); !ok {
				return PackRequest{}, fmt.Errorf("rename requires symbol or position")
			}
		default:
			return PackRequest{}, fmt.Errorf("rename requires symbol or position")
		}
	}
	return body, nil
}
