package model

import (
	"encoding/json"
	"fmt"
)

type GraphQueryRequest struct {
	Scope     *ScopeRequest `json:"scope,omitempty"`
	Root      Selector      `json:"root"`
	Depth     *int64        `json:"depth,omitempty"`
	NodeLimit *int64        `json:"node_limit,omitempty"`
	EdgeLimit *int64        `json:"edge_limit,omitempty"`
	Kinds     []string      `json:"kinds,omitempty"`
	Cursor    *string       `json:"cursor,omitempty"`
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
	f, err := optionalObject(data, maxBytes, "scope", "root", "depth", "node_limit", "edge_limit", "kinds", "cursor")
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
