package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestSubgraphDigestOrderAndOracle(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	member := `{"service_id":null,"worktree_id":null,"profile_id":null,"status":"access_denied","reason_code":"%s"}`
	input := fmt.Sprintf(`{"kind":"federated","members":[],"vector_digest":"%s","group_id":"%s","group_revision":1,"catalog_incarnation":"%s","unavailable_members":[%s,%s]}`, h, id, id, fmt.Sprintf(member, "REASON_B"), fmt.Sprintf(member, "REASON_A"))
	scope, err := DecodeScopeVectorJSON([]byte(input), 16384)
	if err != nil {
		t.Fatal(err)
	}
	scope.VectorDigest, err = ComputeScopeVectorDigest(scope, 16384)
	if err != nil {
		t.Fatal(err)
	}
	got, err := SubgraphDigest(scope, []GraphNode{}, []GraphEdge{}, 16384)
	if err != nil || got != "1f4ada2cc89fd7dbac0677cb101c1a7d30d90166553bd0e4782fee264b827f3e" {
		t.Fatalf("digest %s %v", got, err)
	}
	reordered := scope
	reordered.UnavailableMembers = append([]MemberUnavailable{}, scope.UnavailableMembers...)
	reordered.UnavailableMembers[0], reordered.UnavailableMembers[1] = reordered.UnavailableMembers[1], reordered.UnavailableMembers[0]
	other, err := SubgraphDigest(reordered, []GraphNode{}, []GraphEdge{}, 16384)
	if err != nil || other != got || scope.UnavailableMembers[0].ReasonCode != "REASON_B" {
		t.Fatal("nested vector order changed digest or input")
	}
	ref := &SymbolRef{Kind: "symbol", GraphIdentity: GraphIdentity{WorktreeID: id, ProfileID: h, GenerationID: id}, SymbolID: "s"}
	n := GraphNode{NodeID: "b", Ref: ref, Kind: "function", Label: "f", Plane: "observed_static", EvidenceRefs: []EvidenceRef{}, SourceRefs: []SourceRef{}}
	n2 := n
	n2.NodeID = "a"
	a, err := SubgraphDigest(scope, []GraphNode{n, n2}, []GraphEdge{}, 16384)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SubgraphDigest(scope, []GraphNode{n2, n}, []GraphEdge{}, 16384)
	if err != nil || a != b {
		t.Fatal("node order changed digest")
	}
	many := make([]GraphNode, 501)
	for i := range many {
		many[i] = n
		many[i].NodeID = fmt.Sprintf("node_%03d", i)
	}
	if _, err := SubgraphDigest(scope, many, []GraphEdge{}, 1048576); err != nil {
		t.Fatalf("complete projection exceeds one page: %v", err)
	}
	badEdge := GraphEdge{EdgeID: "e", FromNodeID: "a", ToNodeID: "missing", RelationKind: "CALLS_DIRECT", ResolutionStatus: "exact_in_supported_scope", EvidenceRefs: []EvidenceRef{}}
	if _, err := SubgraphDigest(scope, []GraphNode{n2}, []GraphEdge{badEdge}, 16384); err == nil {
		t.Fatal("accepted missing endpoint")
	}
	if _, err := SubgraphDigest(scope, []GraphNode{n, n}, []GraphEdge{}, 16384); err == nil {
		t.Fatal("accepted duplicate nodes")
	}
}
