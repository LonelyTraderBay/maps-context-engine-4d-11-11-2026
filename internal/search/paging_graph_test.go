package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func graphFixture(t *testing.T, count int) ([]model.GraphNode, []model.GraphEdge) {
	t.Helper()
	id := "11111111-1111-4111-8111-111111111111"
	gen := "22222222-2222-4222-8222-222222222222"
	h := strings.Repeat("a", 64)
	identity := model.GraphIdentity{WorktreeID: id, ProfileID: h, GenerationID: gen}
	nodes := make([]model.GraphNode, count)
	edges := make([]model.GraphEdge, count)
	for i := range nodes {
		nodes[i] = model.GraphNode{NodeID: fmt.Sprintf("node-%04d", i), Ref: &model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: fmt.Sprintf("symbol-%04d", i)}, Kind: "function", Label: fmt.Sprintf("f%d", i), Plane: "observed_static", EvidenceRefs: []model.EvidenceRef{}, SourceRefs: []model.SourceRef{}}
		edges[i] = model.GraphEdge{EdgeID: fmt.Sprintf("edge-%04d", i), FromNodeID: fmt.Sprintf("node-%04d", i), ToNodeID: fmt.Sprintf("node-%04d", (i+1)%count), RelationKind: "CALLS_DIRECT", ResolutionStatus: "exact_in_supported_scope", EvidenceRefs: []model.EvidenceRef{}}
	}
	return nodes, edges
}

func TestGraphMaterializationPagesLargeCycleWithoutLoss(t *testing.T) {
	ctx, cancel, codec, binding, scanner, _, _, _ := mappingFixture(t, 0)
	defer cancel()
	binding.Purpose = "list"
	binding.OrderVersion = "graph-v1"
	nodes, edges := graphFixture(t, 3001)
	pager, err := NewGraphPager(ctx, codec, binding, mappingAllow, scanner, nodes, edges, true, 5000, 5000, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	seenNodes := map[string]bool{}
	seenEdges := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 200 {
			t.Fatal("graph cursor loop")
		}
		page, err := pager.Page(ctx, cursor, binding.PrincipalDigest, mappingAllow, 100, 200, 32768)
		if err != nil {
			t.Fatal(err)
		}
		if pages == 0 {
			retry, err := pager.Page(ctx, cursor, binding.PrincipalDigest, mappingAllow, 100, 200, 32768)
			firstRaw, _ := json.Marshal(page.Data)
			retryRaw, _ := json.Marshal(retry.Data)
			if err != nil || string(firstRaw) != string(retryRaw) {
				t.Fatal("graph replay changed the pinned page")
			}
		}
		if len(page.Data.Nodes) > 100 || len(page.Data.Edges) > 200 || page.SerializedItemBytes > 32768 {
			t.Fatal("page limit exceeded")
		}
		nodeBytes, _ := json.Marshal(page.Data.Nodes)
		edgeBytes, _ := json.Marshal(page.Data.Edges)
		if len(nodeBytes)+len(edgeBytes) != page.SerializedItemBytes {
			t.Fatal("graph item byte count wrong")
		}
		visible := map[string]bool{}
		for _, node := range page.Data.Nodes {
			if visible[node.NodeID] {
				t.Fatal("duplicate node in page")
			}
			visible[node.NodeID] = true
			seenNodes[node.NodeID] = true
		}
		for _, edge := range page.Data.Edges {
			if seenEdges[edge.EdgeID] || !visible[edge.FromNodeID] || !visible[edge.ToNodeID] {
				t.Fatal("edge duplicated or endpoint missing")
			}
			seenEdges[edge.EdgeID] = true
		}
		if page.PendingNodes > 0 && len(page.Data.Edges) > 0 {
			t.Fatal("edge phase started before nodes were delivered")
		}
		if page.NextCursor == nil {
			if page.PendingNodes != 0 || page.PendingEdges != 0 {
				t.Fatal("pending graph omitted")
			}
			break
		}
		if *page.NextCursor == cursor {
			t.Fatal("nonprogressing graph cursor")
		}
		cursor = *page.NextCursor
	}
	if len(seenNodes) != 3001 || len(seenEdges) != 3001 || !seenEdges["edge-3000"] {
		t.Fatal("cycle or graph facts lost")
	}
}

func TestGraphMaterializationRejectsScopeTamperAndIncomplete(t *testing.T) {
	ctx, cancel, codec, binding, scanner, _, _, _ := mappingFixture(t, 0)
	defer cancel()
	binding.Purpose = "list"
	binding.OrderVersion = "graph-v1"
	nodes, edges := graphFixture(t, 3)
	if _, err := NewGraphPager(ctx, codec, binding, mappingAllow, scanner, nodes, edges, false, 10, 10, 65536); !errors.Is(err, ErrGraphIncomplete) {
		t.Fatalf("incomplete traversal accepted: %v", err)
	}
	pager, err := NewGraphPager(ctx, codec, binding, mappingAllow, scanner, nodes, edges, true, 10, 10, 65536)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pager.Page(ctx, "", binding.PrincipalDigest, mappingAllow, 1, 10, 8192)
	if err != nil || page.NextCursor == nil {
		t.Fatalf("missing graph continuation: %v", err)
	}
	if _, err = pager.Page(ctx, *page.NextCursor, strings.Repeat("f", 64), mappingAllow, 1, 10, 8192); !errors.Is(err, ErrGraphPageInvalid) {
		t.Fatalf("wrong principal accepted: %v", err)
	}
	revoked := errors.New("revoked")
	if _, err = pager.Page(ctx, *page.NextCursor, binding.PrincipalDigest, func(context.Context, contextpack.CursorBinding) error { return revoked }, 1, 10, 8192); !errors.Is(err, revoked) {
		t.Fatalf("revoke ignored: %v", err)
	}
	if _, err = pager.Page(ctx, *page.NextCursor+"x", binding.PrincipalDigest, mappingAllow, 1, 10, 8192); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("tampered graph cursor accepted: %v", err)
	}
	if _, err = pager.Page(ctx, "", binding.PrincipalDigest, mappingAllow, 1, 10, 4); !errors.Is(err, ErrGraphPageLarge) {
		t.Fatalf("empty-progress page accepted: %v", err)
	}
	bad := append([]model.GraphEdge{}, edges...)
	bad[0].ToNodeID = "absent"
	if _, err = NewGraphPager(ctx, codec, binding, mappingAllow, scanner, nodes, bad, true, 10, 10, 65536); !errors.Is(err, ErrGraphPageInvalid) {
		t.Fatalf("missing endpoint accepted: %v", err)
	}
	other := append([]model.GraphNode{}, nodes...)
	other[0].Ref = &model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: "99999999-9999-4999-8999-999999999999", ProfileID: strings.Repeat("a", 64), GenerationID: "22222222-2222-4222-8222-222222222222"}, SymbolID: "foreign"}
	if _, err = NewGraphPager(ctx, codec, binding, mappingAllow, scanner, other, edges, true, 10, 10, 65536); !errors.Is(err, ErrGraphPageInvalid) {
		t.Fatalf("cross-scope node accepted: %v", err)
	}
	pager.now = func() time.Time { return time.Unix(pager.expires, 0) }
	if _, err = pager.Page(ctx, *page.NextCursor, binding.PrincipalDigest, mappingAllow, 1, 10, 8192); !errors.Is(err, ErrGraphPageExpired) {
		t.Fatalf("expired graph accepted: %v", err)
	}
}
