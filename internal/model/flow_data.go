package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type FlowStep struct {
	StepID             string        `json:"step_id"`
	CanonicalRefs      []GraphRef    `json:"canonical_refs"`
	SourceRefs         []SourceRef   `json:"source_refs"`
	StepKind           string        `json:"step_kind"`
	OrderSemantics     string        `json:"order_semantics"`
	PossibleOrObserved string        `json:"possible_or_observed"`
	OutcomeStatus      string        `json:"outcome_status"`
	EvidenceRefs       []EvidenceRef `json:"evidence_refs"`
	Limitations        []Gap         `json:"limitations"`
}
type FlowEdge struct {
	EdgeID             string        `json:"edge_id"`
	FromStepID         string        `json:"from_step_id"`
	ToStepID           string        `json:"to_step_id"`
	Kind               string        `json:"kind"`
	ConditionSourceRef *SourceRef    `json:"condition_source_ref"`
	EvidenceRefs       []EvidenceRef `json:"evidence_refs"`
}
type FlowData struct {
	Operation    string     `json:"operation"`
	ProjectionID string     `json:"projection_id"`
	RootRef      GraphRef   `json:"root_ref"`
	Mode         string     `json:"mode"`
	Steps        []FlowStep `json:"steps"`
	Edges        []FlowEdge `json:"edges"`
}

func decodeGraphRefs(raw json.RawMessage, min, limit, maxBytes int) ([]GraphRef, error) {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	if list == nil || len(list) < min || len(list) > limit {
		return nil, fmt.Errorf("invalid graph refs")
	}
	result := make([]GraphRef, 0, len(list))
	for _, item := range list {
		r, err := DecodeGraphRefJSON(item, maxBytes)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}
func decodeGaps(raw json.RawMessage, limit, maxBytes int) ([]Gap, error) {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	if list == nil || len(list) > limit {
		return nil, fmt.Errorf("invalid gap array")
	}
	result := make([]Gap, 0, len(list))
	for _, item := range list {
		g, err := DecodeGapJSON(item, maxBytes)
		if err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, nil
}
func DecodeFlowStepJSON(data []byte, maxBytes int) (FlowStep, error) {
	var s FlowStep
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return s, err
	}
	f, err := scopeObject(data, []string{"step_id", "canonical_refs", "source_refs", "step_kind", "order_semantics", "possible_or_observed", "outcome_status", "evidence_refs", "limitations"}, nil)
	if err != nil {
		return s, err
	}
	s.CanonicalRefs, err = decodeGraphRefs(f["canonical_refs"], 1, 16, maxBytes)
	if err != nil {
		return s, err
	}
	delete(f, "canonical_refs")
	if _, err := decodeSourceRefs(f["source_refs"], 16, maxBytes); err != nil {
		return s, err
	}
	if _, err := decodeEvidenceRefs(f["evidence_refs"], 16, maxBytes); err != nil {
		return s, err
	}
	if _, err := decodeGaps(f["limitations"], 16, maxBytes); err != nil {
		return s, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return s, err
	}
	var body FlowStep
	if err := json.Unmarshal(raw, &body); err != nil {
		return s, err
	}
	body.CanonicalRefs = s.CanonicalRefs
	if !validID(body.StepID) || !validID(body.StepKind) || body.SourceRefs == nil || body.EvidenceRefs == nil || body.Limitations == nil {
		return FlowStep{}, fmt.Errorf("invalid flow step")
	}
	switch body.OrderSemantics {
	case "source_sequence", "control_flow_supported", "call_relationship_only", "causal_trace", "unknown":
	default:
		return FlowStep{}, fmt.Errorf("invalid order semantics")
	}
	switch body.PossibleOrObserved {
	case "statically_possible", "observed":
	default:
		return FlowStep{}, fmt.Errorf("invalid observation status")
	}
	switch body.OutcomeStatus {
	case "declared_operation", "statically_possible", "attempt_observed", "statement_success_observed", "commit_observed", "rollback_observed", "outcome_unknown":
	default:
		return FlowStep{}, fmt.Errorf("invalid outcome status")
	}
	return body, nil
}
func DecodeFlowEdgeJSON(data []byte, maxBytes int) (FlowEdge, error) {
	var e FlowEdge
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return e, err
	}
	f, err := scopeObject(data, []string{"edge_id", "from_step_id", "to_step_id", "kind", "condition_source_ref", "evidence_refs"}, map[string]bool{"condition_source_ref": true})
	if err != nil {
		return e, err
	}
	if !bytes.Equal(bytes.TrimSpace(f["condition_source_ref"]), []byte("null")) {
		if _, err := DecodeSourceRefJSON(f["condition_source_ref"], maxBytes); err != nil {
			return e, err
		}
	}
	if _, err := decodeEvidenceRefs(f["evidence_refs"], 16, maxBytes); err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return FlowEdge{}, err
	}
	if !validID(e.EdgeID) || !validID(e.FromStepID) || !validID(e.ToStepID) || e.EvidenceRefs == nil {
		return FlowEdge{}, fmt.Errorf("invalid flow edge")
	}
	switch e.Kind {
	case "call", "control", "return", "async", "contract", "transaction_link", "candidate":
	default:
		return FlowEdge{}, fmt.Errorf("invalid flow edge kind")
	}
	return e, nil
}

// DecodeFlowDataJSON checks visible endpoint integrity and keeps static results
// distinct from observed outcomes. It does not prove actual execution or source.
func DecodeFlowDataJSON(data []byte, maxBytes int) (FlowData, error) {
	var d FlowData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return d, err
	}
	f, err := scopeObject(data, []string{"operation", "projection_id", "root_ref", "mode", "steps", "edges"}, nil)
	if err != nil {
		return d, err
	}
	d.RootRef, err = DecodeGraphRefJSON(f["root_ref"], maxBytes)
	if err != nil {
		return d, err
	}
	delete(f, "root_ref")
	var steps, edges []json.RawMessage
	if err := json.Unmarshal(f["steps"], &steps); err != nil {
		return d, err
	}
	if err := json.Unmarshal(f["edges"], &edges); err != nil {
		return d, err
	}
	if steps == nil || edges == nil || len(steps) > 100 || len(edges) > 300 {
		return d, fmt.Errorf("invalid flow page size")
	}
	delete(f, "steps")
	delete(f, "edges")
	raw, err := json.Marshal(f)
	if err != nil {
		return d, err
	}
	var body FlowData
	if err := json.Unmarshal(raw, &body); err != nil {
		return d, err
	}
	body.RootRef = d.RootRef
	if body.Operation != "flow_query" || !lowerHex(body.ProjectionID, 64) || body.Mode != "static" && body.Mode != "observed" {
		return FlowData{}, fmt.Errorf("invalid flow data")
	}
	ids := map[string]bool{}
	body.Steps = make([]FlowStep, 0, len(steps))
	for _, raw := range steps {
		s, err := DecodeFlowStepJSON(raw, maxBytes)
		if err != nil {
			return FlowData{}, err
		}
		if ids[s.StepID] {
			return FlowData{}, fmt.Errorf("duplicate flow step")
		}
		if body.Mode == "static" && (s.PossibleOrObserved == "observed" || s.OrderSemantics == "causal_trace" || s.OutcomeStatus == "attempt_observed" || s.OutcomeStatus == "statement_success_observed" || s.OutcomeStatus == "commit_observed" || s.OutcomeStatus == "rollback_observed") {
			return FlowData{}, fmt.Errorf("observed outcome in static flow")
		}
		ids[s.StepID] = true
		body.Steps = append(body.Steps, s)
	}
	seen := map[string]bool{}
	body.Edges = make([]FlowEdge, 0, len(edges))
	for _, raw := range edges {
		e, err := DecodeFlowEdgeJSON(raw, maxBytes)
		if err != nil {
			return FlowData{}, err
		}
		if seen[e.EdgeID] || !ids[e.FromStepID] || !ids[e.ToStepID] {
			return FlowData{}, fmt.Errorf("invalid flow edge endpoint")
		}
		seen[e.EdgeID] = true
		body.Edges = append(body.Edges, e)
	}
	return body, nil
}
