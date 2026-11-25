package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

type GraphQueryRequest struct {
	Scope        *ScopeRequest `json:"scope,omitempty"`
	Root         Selector      `json:"root"`
	Depth        *int64        `json:"depth,omitempty"`
	NodeLimit    *int64        `json:"node_limit,omitempty"`
	EdgeLimit    *int64        `json:"edge_limit,omitempty"`
	Kinds        []string      `json:"kinds,omitempty"`
	Cursor       *string       `json:"cursor,omitempty"`
	Directory    *string       `json:"directory,omitempty"`
	LocalFile    bool          `json:"local_file,omitempty"`
	Neighborhood string        `json:"neighborhood,omitempty"`
	SymbolQuery  string        `json:"symbol_query,omitempty"`
}
type FlowQueryRequest struct {
	Scope      *ScopeRequest `json:"scope,omitempty"`
	RootRef    GraphRef      `json:"root_ref"`
	Mode       *string       `json:"mode,omitempty"`
	ArtifactID *string       `json:"artifact_id,omitempty"`
	StepLimit  *int64        `json:"step_limit,omitempty"`
	EdgeLimit  *int64        `json:"edge_limit,omitempty"`
	Cursor     *string       `json:"cursor,omitempty"`
}

func optionalRange(value *int64, min, max int64) bool {
	return value == nil || (*value >= min && *value <= max)
}

// DecodeGraphQueryRequestJSON checks only wire and hard schema limits. Actual
// configured limits, pinned scope and authorization belong to the service.
func DecodeGraphQueryRequestJSON(data []byte, maxBytes int) (GraphQueryRequest, error) {
	var r GraphQueryRequest
	f, err := optionalObject(data, maxBytes, "scope", "root", "depth", "node_limit", "edge_limit", "kinds", "cursor", "directory", "local_file", "neighborhood", "symbol_query")
	if err != nil {
		return r, err
	}
	root, ok := f["root"]
	if !ok {
		return r, fmt.Errorf("missing graph root")
	}
	r.Root, err = DecodeSelectorJSON(root, maxBytes)
	if err != nil {
		return GraphQueryRequest{}, err
	}
	delete(f, "root")
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return GraphQueryRequest{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return GraphQueryRequest{}, err
	}
	var body GraphQueryRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return GraphQueryRequest{}, err
	}
	body.Root = r.Root
	body.Scope = r.Scope
	if _, present := f["symbol_query"]; present {
		_, workspace := body.Root.(*WorkspaceSelector)
		if !workspace || !boundedString(body.SymbolQuery, 256) || strings.TrimSpace(body.SymbolQuery) != body.SymbolQuery ||
			body.Directory != nil || body.LocalFile || body.Neighborhood != "" || body.Depth != nil || body.EdgeLimit != nil ||
			len(body.Kinds) != 0 || body.Cursor != nil {
			return GraphQueryRequest{}, fmt.Errorf("invalid symbol query")
		}
	}
	if _, present := f["neighborhood"]; present {
		if body.Neighborhood != "outgoing" && body.Neighborhood != "incoming" && body.Neighborhood != "both" {
			return GraphQueryRequest{}, fmt.Errorf("invalid neighborhood direction")
		}
		selected, ok := body.Root.(*RefSelector)
		if !ok || selected.ServiceID != nil || body.Directory != nil || body.LocalFile || body.Cursor != nil ||
			body.SymbolQuery != "" || !optionalRange(body.Depth, 1, 4) {
			return GraphQueryRequest{}, fmt.Errorf("invalid neighborhood request")
		}
		if _, ok := selected.Ref.(*SymbolRef); !ok {
			return GraphQueryRequest{}, fmt.Errorf("neighborhood needs a symbol reference")
		}
	}
	if body.Directory != nil {
		_, workspace := body.Root.(*WorkspaceSelector)
		if !workspace || body.LocalFile || body.Depth != nil || body.EdgeLimit != nil || len(body.Kinds) != 0 ||
			*body.Directory != "" && !validSourcePath(*body.Directory) {
			return GraphQueryRequest{}, fmt.Errorf("invalid directory request")
		}
	}
	if body.LocalFile {
		root, ok := body.Root.(*RefSelector)
		if !ok {
			return GraphQueryRequest{}, fmt.Errorf("local graph needs a file reference")
		}
		file, ok := root.Ref.(*FileRef)
		if !ok || file.ContentHash == nil || body.Depth != nil && *body.Depth != 1 || len(body.Kinds) != 0 {
			return GraphQueryRequest{}, fmt.Errorf("invalid local file graph request")
		}
	}
	if !optionalRange(body.Depth, 0, 8) || !optionalRange(body.NodeLimit, 1, 500) || !optionalRange(body.EdgeLimit, 1, 1500) || len(body.Kinds) > 32 || (body.Cursor != nil && !validToken(*body.Cursor)) {
		return GraphQueryRequest{}, fmt.Errorf("invalid graph request")
	}
	for _, kind := range body.Kinds {
		if !validID(kind) {
			return GraphQueryRequest{}, fmt.Errorf("invalid graph kind")
		}
	}
	return body, nil
}

// DecodeFlowQueryRequestJSON does not verify imported artifact provenance or
// expose an observed capability; those require the owning service and gate.
func DecodeFlowQueryRequestJSON(data []byte, maxBytes int) (FlowQueryRequest, error) {
	var r FlowQueryRequest
	f, err := optionalObject(data, maxBytes, "scope", "root_ref", "mode", "artifact_id", "step_limit", "edge_limit", "cursor")
	if err != nil {
		return r, err
	}
	root, ok := f["root_ref"]
	if !ok {
		return r, fmt.Errorf("missing flow root")
	}
	r.RootRef, err = DecodeGraphRefJSON(root, maxBytes)
	if err != nil {
		return FlowQueryRequest{}, err
	}
	delete(f, "root_ref")
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return FlowQueryRequest{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return FlowQueryRequest{}, err
	}
	var body FlowQueryRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return FlowQueryRequest{}, err
	}
	body.RootRef = r.RootRef
	body.Scope = r.Scope
	if body.Mode != nil && *body.Mode != "static" && *body.Mode != "observed" || body.Mode != nil && *body.Mode == "observed" && body.ArtifactID == nil || body.ArtifactID != nil && !lowerHex(*body.ArtifactID, 64) || !optionalRange(body.StepLimit, 1, 100) || !optionalRange(body.EdgeLimit, 1, 300) || body.Cursor != nil && !validToken(*body.Cursor) {
		return FlowQueryRequest{}, fmt.Errorf("invalid flow request")
	}
	return body, nil
}
