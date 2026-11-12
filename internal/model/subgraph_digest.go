package model

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SubgraphDigest hashes a complete validated graph projection. The caller must
// establish scope authorization, sanitization and enumeration completeness.
func SubgraphDigest(scope ScopeVector, nodes []GraphNode, edges []GraphEdge, maxBytes int) (string, error) {
	if maxBytes <= 0 || nodes == nil || edges == nil || len(nodes) > maxBytes || len(edges) > maxBytes {
		return "", fmt.Errorf("invalid graph budget")
	}
	normalizedScope, err := NormalizeScopeVector(scope, maxBytes)
	if err != nil {
		return "", err
	}
	scope = normalizedScope
	validNodes := make([]GraphNode, 0, len(nodes))
	ids := map[string]bool{}
	for _, node := range nodes {
		raw, err := json.Marshal(node)
		if err != nil {
			return "", err
		}
		v, err := DecodeGraphNodeJSON(raw, maxBytes)
		if err != nil {
			return "", err
		}
		if ids[v.NodeID] {
			return "", fmt.Errorf("duplicate node")
		}
		ids[v.NodeID] = true
		validNodes = append(validNodes, v)
	}
	validEdges := make([]GraphEdge, 0, len(edges))
	edgeIDs := map[string]bool{}
	for _, edge := range edges {
		raw, err := json.Marshal(edge)
		if err != nil {
			return "", err
		}
		v, err := DecodeGraphEdgeJSON(raw, maxBytes)
		if err != nil {
			return "", err
		}
		if edgeIDs[v.EdgeID] || !ids[v.FromNodeID] || !ids[v.ToNodeID] {
			return "", fmt.Errorf("invalid edge endpoint")
		}
		edgeIDs[v.EdgeID] = true
		validEdges = append(validEdges, v)
	}
	sort.Slice(validNodes, func(i, j int) bool { return validNodes[i].NodeID < validNodes[j].NodeID })
	sort.Slice(validEdges, func(i, j int) bool { return validEdges[i].EdgeID < validEdges[j].EdgeID })
	value := struct {
		Scope ScopeVector `json:"scope"`
		Nodes []GraphNode `json:"nodes"`
		Edges []GraphEdge `json:"edges"`
	}{scope, validNodes, validEdges}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	v, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("subgraph-v1", v, maxBytes)
	return digest, err
}
