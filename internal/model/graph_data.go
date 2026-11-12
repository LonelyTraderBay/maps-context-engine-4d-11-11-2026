package model

import (
	"encoding/json"
	"fmt"
)

type GraphNode struct {
	NodeID          string        `json:"node_id"`
	Ref             GraphRef      `json:"ref"`
	Kind            string        `json:"kind"`
	Label           string        `json:"label"`
	Plane           string        `json:"plane"`
	EvidenceRefs    []EvidenceRef `json:"evidence_refs"`
	SourceRefs      []SourceRef   `json:"source_refs"`
	ExpandableCount *int64        `json:"expandable_count"`
}
type GraphEdge struct {
	EdgeID           string        `json:"edge_id"`
	FromNodeID       string        `json:"from_node_id"`
	ToNodeID         string        `json:"to_node_id"`
	RelationKind     string        `json:"relation_kind"`
	ResolutionStatus string        `json:"resolution_status"`
	EvidenceRefs     []EvidenceRef `json:"evidence_refs"`
	ExpandRef        *string       `json:"expand_ref"`
}
type GraphData struct {
	Operation      string      `json:"operation"`
	Nodes          []GraphNode `json:"nodes"`
	Edges          []GraphEdge `json:"edges"`
	SubgraphDigest string      `json:"subgraph_digest"`
}

func decodeSourceRefs(raw json.RawMessage, limit, maxBytes int) ([]SourceRef, error) {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	if list == nil || len(list) > limit {
		return nil, fmt.Errorf("invalid source ref array")
	}
	out := make([]SourceRef, 0, len(list))
	for _, item := range list {
		v, err := DecodeSourceRefJSON(item, maxBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func decodeEvidenceRefs(raw json.RawMessage, limit, maxBytes int) ([]EvidenceRef, error) {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	if list == nil || len(list) > limit {
		return nil, fmt.Errorf("invalid evidence array")
	}
	out := make([]EvidenceRef, 0, len(list))
	for _, item := range list {
		v, err := DecodeEvidenceRefJSON(item, maxBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func DecodeGraphNodeJSON(data []byte, maxBytes int) (GraphNode, error) {
	var n GraphNode
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return n, err
	}
	f, err := scopeObject(data, []string{"node_id", "ref", "kind", "label", "plane", "evidence_refs", "source_refs", "expandable_count"}, map[string]bool{"expandable_count": true})
	if err != nil {
		return n, err
	}
	n.Ref, err = DecodeGraphRefJSON(f["ref"], maxBytes)
	if err != nil {
		return n, err
	}
	delete(f, "ref")
	if _, err := decodeSourceRefs(f["source_refs"], 16, maxBytes); err != nil {
		return n, err
	}
	if _, err := decodeEvidenceRefs(f["evidence_refs"], 16, maxBytes); err != nil {
		return n, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return n, err
	}
	var body GraphNode
	if err := json.Unmarshal(raw, &body); err != nil {
		return n, err
	}
	body.Ref = n.Ref
	if !validID(body.NodeID) || !validID(body.Kind) || !stringAtMost(body.Label, 512) || !optionalRange(body.ExpandableCount, 0, maxExactInteger) || body.EvidenceRefs == nil || body.SourceRefs == nil {
		return GraphNode{}, fmt.Errorf("invalid graph node")
	}
	switch body.Plane {
	case "intended", "observed_static", "observed_runtime", "annotation":
	default:
		return GraphNode{}, fmt.Errorf("invalid graph plane")
	}
	return body, nil
}
func DecodeGraphEdgeJSON(data []byte, maxBytes int) (GraphEdge, error) {
	var e GraphEdge
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return e, err
	}
	f, err := scopeObject(data, []string{"edge_id", "from_node_id", "to_node_id", "relation_kind", "resolution_status", "evidence_refs", "expand_ref"}, map[string]bool{"expand_ref": true})
	if err != nil {
		return e, err
	}
	if _, err := decodeEvidenceRefs(f["evidence_refs"], 16, maxBytes); err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return GraphEdge{}, err
	}
	if !validID(e.EdgeID) || !validID(e.FromNodeID) || !validID(e.ToNodeID) || !validID(e.RelationKind) || e.EvidenceRefs == nil || e.ExpandRef != nil && !validToken(*e.ExpandRef) {
		return GraphEdge{}, fmt.Errorf("invalid graph edge")
	}
	switch e.ResolutionStatus {
	case "exact_in_supported_scope", "candidate", "unresolved":
	default:
		return GraphEdge{}, fmt.Errorf("invalid edge status")
	}
	return e, nil
}

// DecodeGraphDataJSON verifies page-local endpoint visibility. It does not
// validate the subgraph digest, provider evidence, capability or authorization.
func DecodeGraphDataJSON(data []byte, maxBytes int) (GraphData, error) {
	var g GraphData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return g, err
	}
	f, err := scopeObject(data, []string{"operation", "nodes", "edges", "subgraph_digest"}, nil)
	if err != nil {
		return g, err
	}
	var nodes, edges []json.RawMessage
	if err := json.Unmarshal(f["nodes"], &nodes); err != nil {
		return g, err
	}
	if err := json.Unmarshal(f["edges"], &edges); err != nil {
		return g, err
	}
	if nodes == nil || edges == nil || len(nodes) > 500 || len(edges) > 1500 {
		return g, fmt.Errorf("invalid graph page size")
	}
	if err := json.Unmarshal(f["operation"], &g.Operation); err != nil {
		return g, err
	}
	if err := json.Unmarshal(f["subgraph_digest"], &g.SubgraphDigest); err != nil {
		return g, err
	}
	if g.Operation != "graph_query" || !lowerHex(g.SubgraphDigest, 64) {
		return GraphData{}, fmt.Errorf("invalid graph data")
	}
	g.Nodes = make([]GraphNode, 0, len(nodes))
	g.Edges = make([]GraphEdge, 0, len(edges))
	ids := map[string]bool{}
	for _, raw := range nodes {
		n, err := DecodeGraphNodeJSON(raw, maxBytes)
		if err != nil {
			return GraphData{}, err
		}
		if ids[n.NodeID] {
			return GraphData{}, fmt.Errorf("duplicate node")
		}
		ids[n.NodeID] = true
		g.Nodes = append(g.Nodes, n)
	}
	edgeIDs := map[string]bool{}
	for _, raw := range edges {
		e, err := DecodeGraphEdgeJSON(raw, maxBytes)
		if err != nil {
			return GraphData{}, err
		}
		if edgeIDs[e.EdgeID] || !ids[e.FromNodeID] || !ids[e.ToNodeID] {
			return GraphData{}, fmt.Errorf("invalid graph edge endpoint")
		}
		edgeIDs[e.EdgeID] = true
		g.Edges = append(g.Edges, e)
	}
	return g, nil
}
