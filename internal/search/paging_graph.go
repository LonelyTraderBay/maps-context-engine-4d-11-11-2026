package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

var (
	ErrGraphPageInvalid = errors.New("graph page invalid")
	ErrGraphPageLarge   = errors.New("graph item exceeds item budget")
	ErrGraphPageExpired = errors.New("graph materialization expired")
	ErrGraphIncomplete  = errors.New("graph traversal cannot be resumed")
)

type graphEdgeEntry struct {
	id   string
	from string
	to   string
	raw  []byte
}

// GraphPager holds a complete, bounded projection. Every edge page repeats its
// endpoint nodes so the sealed GraphData page remains independently valid;
// clients can deduplicate those nodes by their stable IDs across pages.
type GraphPager struct {
	nodes   map[string][]byte
	nodeIDs []string
	edges   []graphEdgeEntry
	codec   *contextpack.CursorCodec
	binding contextpack.CursorBinding
	guard   contextpack.CursorGuard
	anchor  string
	expires int64
	now     func() time.Time
}

type GraphPage struct {
	Data                model.GraphData
	PendingNodes        int64
	PendingEdges        int64
	NextCursor          *string
	SerializedItemBytes int
}

// NewGraphPager requires the producer to prove enumeration complete and to
// enforce source permissions. A missing frontier is an error, not a cursor
// that silently restarts against a different snapshot.
func NewGraphPager(ctx context.Context, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard, scanner *policy.Scanner, nodes []model.GraphNode, edges []model.GraphEdge, enumerationComplete bool, maxNodes, maxEdges, maxBytes int) (*GraphPager, error) {
	if ctx == nil || codec == nil || guard == nil || scanner == nil || binding.Purpose != "list" || binding.ResultID != "" || binding.ResultDigest != "" || nodes == nil || edges == nil || maxNodes < 1 || maxNodes > 100000 || maxEdges < 1 || maxEdges > 1000000 || maxBytes < 1024 || maxBytes > 16*1024*1024 || len(nodes) > maxNodes || len(edges) > maxEdges {
		return nil, ErrGraphPageInvalid
	}
	if !enumerationComplete {
		return nil, ErrGraphIncomplete
	}
	preToken, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
	if err != nil {
		return nil, err
	}
	preClaims, err := codec.Verify(ctx, preToken, binding, guard)
	if err != nil {
		return nil, err
	}
	p := &GraphPager{nodes: map[string][]byte{}, nodeIDs: []string{}, edges: []graphEdgeEntry{}, codec: codec, binding: binding, guard: guard, now: time.Now}
	used := 0
	for _, node := range nodes {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		raw, e := json.Marshal(node)
		if e != nil || len(raw) > maxBytes-used {
			return nil, ErrGraphPageLarge
		}
		value, e := model.DecodeGraphNodeJSON(raw, maxBytes)
		if e != nil {
			return nil, ErrGraphPageInvalid
		}
		if _, e = model.MatchPinnedMember(binding.Scope, value.Ref, nil); e != nil {
			return nil, ErrGraphPageInvalid
		}
		for _, ref := range value.SourceRefs {
			if _, e = model.MatchPinnedSource(binding.Scope, ref, nil, maxBytes); e != nil {
				return nil, ErrGraphPageInvalid
			}
		}
		for _, evidence := range value.EvidenceRefs {
			for _, ref := range evidence.SourceRefs {
				if _, e = model.MatchPinnedSource(binding.Scope, ref, nil, maxBytes); e != nil {
					return nil, ErrGraphPageInvalid
				}
			}
		}
		if e = scanner.CheckContent(raw); e != nil {
			return nil, e
		}
		if old, ok := p.nodes[value.NodeID]; ok {
			if !bytes.Equal(old, raw) {
				return nil, ErrGraphPageInvalid
			}
			continue
		}
		p.nodes[value.NodeID] = append([]byte(nil), raw...)
		p.nodeIDs = append(p.nodeIDs, value.NodeID)
		used += len(raw)
	}
	sort.Strings(p.nodeIDs)
	seen := map[string][]byte{}
	for _, edge := range edges {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		raw, e := json.Marshal(edge)
		if e != nil || len(raw) > maxBytes-used {
			return nil, ErrGraphPageLarge
		}
		value, e := model.DecodeGraphEdgeJSON(raw, maxBytes)
		if e != nil {
			return nil, ErrGraphPageInvalid
		}
		if _, ok := p.nodes[value.FromNodeID]; !ok {
			return nil, ErrGraphPageInvalid
		}
		if _, ok := p.nodes[value.ToNodeID]; !ok {
			return nil, ErrGraphPageInvalid
		}
		for _, evidence := range value.EvidenceRefs {
			for _, ref := range evidence.SourceRefs {
				if _, e = model.MatchPinnedSource(binding.Scope, ref, nil, maxBytes); e != nil {
					return nil, ErrGraphPageInvalid
				}
			}
		}
		if e = scanner.CheckContent(raw); e != nil {
			return nil, e
		}
		if old, ok := seen[value.EdgeID]; ok {
			if !bytes.Equal(old, raw) {
				return nil, ErrGraphPageInvalid
			}
			continue
		}
		seen[value.EdgeID] = append([]byte(nil), raw...)
		p.edges = append(p.edges, graphEdgeEntry{id: value.EdgeID, from: value.FromNodeID, to: value.ToNodeID, raw: append([]byte(nil), raw...)})
		used += len(raw)
	}
	sort.Slice(p.edges, func(i, j int) bool { return p.edges[i].id < p.edges[j].id })
	if err = codec.Recheck(ctx, preClaims, guard); err != nil {
		return nil, err
	}
	// Bind the immutable complete projection to both an instance UUID and its
	// normative digest, rather than deriving an ID from ad hoc concatenation.
	normalizedNodes := make([]model.GraphNode, 0, len(p.nodeIDs))
	for _, id := range p.nodeIDs {
		node, e := model.DecodeGraphNodeJSON(p.nodes[id], maxBytes)
		if e != nil {
			return nil, ErrGraphPageInvalid
		}
		normalizedNodes = append(normalizedNodes, node)
	}
	normalizedEdges := make([]model.GraphEdge, 0, len(p.edges))
	for _, entry := range p.edges {
		edge, e := model.DecodeGraphEdgeJSON(entry.raw, maxBytes)
		if e != nil {
			return nil, ErrGraphPageInvalid
		}
		normalizedEdges = append(normalizedEdges, edge)
	}
	digest, err := model.SubgraphDigest(binding.Scope, normalizedNodes, normalizedEdges, maxBytes)
	if err != nil {
		return nil, ErrGraphPageLarge
	}
	id, err := mappingID()
	if err != nil {
		return nil, err
	}
	p.binding.ResultID, p.binding.ResultDigest = id, digest
	p.anchor, err = codec.Issue(ctx, p.binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
	if err != nil {
		return nil, err
	}
	claims, err := codec.Verify(ctx, p.anchor, p.binding, guard)
	if err != nil {
		return nil, err
	}
	p.expires = claims.ExpiresAt
	return p, nil
}

// Page limits its node/edge arrays and their serialized bytes. The transport
// owner must additionally measure the entire envelope and delivery blocks.
func (p *GraphPager) Page(ctx context.Context, cursor, principal string, currentGuard contextpack.CursorGuard, nodeLimit, edgeLimit, maxItemBytes int) (GraphPage, error) {
	if p == nil || ctx == nil || currentGuard == nil || principal != p.binding.PrincipalDigest || nodeLimit < 1 || nodeLimit > 500 || edgeLimit < 1 || edgeLimit > 1500 || maxItemBytes < 4 || maxItemBytes > 131072 {
		return GraphPage{}, ErrGraphPageInvalid
	}
	authorize := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := p.guard(ctx, b); err != nil {
			return err
		}
		return currentGuard(ctx, b)
	}
	if p.now().Unix() >= p.expires {
		return GraphPage{}, ErrGraphPageExpired
	}
	anchor, err := p.codec.Verify(ctx, p.anchor, p.binding, authorize)
	if err != nil {
		return GraphPage{}, err
	}
	position := contextpack.SignedPosition{LastKey: []string{}}
	var old contextpack.SignedCursor
	if cursor != "" {
		old, err = p.codec.Verify(ctx, cursor, p.binding, authorize)
		if err != nil {
			return GraphPage{}, err
		}
		position = old.Position
	}
	if len(position.LastKey) != 0 || position.NextWarningGap != 0 || position.NextByteOffset != 0 || position.NextItem > int64(len(p.nodeIDs)) || position.NextCoverageGap > int64(len(p.edges)) || position.NextItem < int64(len(p.nodeIDs)) && position.NextCoverageGap != 0 {
		return GraphPage{}, ErrGraphPageInvalid
	}
	i, j := int(position.NextItem), int(position.NextCoverageGap)
	outNodes := []model.GraphNode{}
	outEdges := []model.GraphEdge{}
	used := 4
	if i < len(p.nodeIDs) {
		for i < len(p.nodeIDs) && len(outNodes) < nodeLimit {
			if err = ctx.Err(); err != nil {
				return GraphPage{}, err
			}
			raw := p.nodes[p.nodeIDs[i]]
			need := len(raw)
			if len(outNodes) > 0 {
				need++
			}
			if need > maxItemBytes-used {
				if len(outNodes) == 0 {
					return GraphPage{}, ErrGraphPageLarge
				}
				break
			}
			node, e := model.DecodeGraphNodeJSON(raw, maxItemBytes)
			if e != nil {
				return GraphPage{}, ErrGraphPageInvalid
			}
			outNodes = append(outNodes, node)
			used += need
			i++
		}
	} else {
		visible := map[string]bool{}
		for j < len(p.edges) && len(outEdges) < edgeLimit {
			if err = ctx.Err(); err != nil {
				return GraphPage{}, err
			}
			entry := p.edges[j]
			needed := []string{}
			if !visible[entry.from] {
				needed = append(needed, entry.from)
			}
			if !visible[entry.to] && entry.to != entry.from {
				needed = append(needed, entry.to)
			}
			if len(outNodes)+len(needed) > nodeLimit {
				if len(outEdges) == 0 {
					return GraphPage{}, ErrGraphPageLarge
				}
				break
			}
			more := len(entry.raw)
			if len(outEdges) > 0 {
				more++
			}
			for k, id := range needed {
				more += len(p.nodes[id])
				if len(outNodes)+k > 0 {
					more++
				}
			}
			if more > maxItemBytes-used {
				if len(outEdges) == 0 {
					return GraphPage{}, ErrGraphPageLarge
				}
				break
			}
			for _, id := range needed {
				node, e := model.DecodeGraphNodeJSON(p.nodes[id], maxItemBytes)
				if e != nil {
					return GraphPage{}, ErrGraphPageInvalid
				}
				outNodes = append(outNodes, node)
				visible[id] = true
			}
			edge, e := model.DecodeGraphEdgeJSON(entry.raw, maxItemBytes)
			if e != nil {
				return GraphPage{}, ErrGraphPageInvalid
			}
			outEdges = append(outEdges, edge)
			used += more
			j++
		}
		sort.Slice(outNodes, func(a, b int) bool { return outNodes[a].NodeID < outNodes[b].NodeID })
	}
	pendingNodes, pendingEdges := len(p.nodeIDs)-i, len(p.edges)-j
	var next *string
	if pendingNodes > 0 || pendingEdges > 0 {
		newPosition := contextpack.SignedPosition{NextItem: int64(i), NextCoverageGap: int64(j), LastKey: []string{}}
		if !contextpack.CursorAdvances(position, newPosition) {
			return GraphPage{}, ErrGraphPageInvalid
		}
		value, e := p.codec.Issue(ctx, p.binding, newPosition, authorize)
		if e != nil {
			return GraphPage{}, e
		}
		next = &value
	}
	digest, err := model.SubgraphDigest(p.binding.Scope, outNodes, outEdges, 16*1024*1024)
	if err != nil {
		return GraphPage{}, ErrGraphPageInvalid
	}
	data := model.GraphData{Operation: "graph_query", Nodes: outNodes, Edges: outEdges, SubgraphDigest: digest}
	raw, err := json.Marshal(data)
	if err != nil {
		return GraphPage{}, ErrGraphPageInvalid
	}
	if _, err = model.DecodeGraphDataJSON(raw, 1024*1024); err != nil {
		return GraphPage{}, ErrGraphPageInvalid
	}
	if err = p.codec.Recheck(ctx, anchor, authorize); err != nil {
		return GraphPage{}, err
	}
	if cursor != "" {
		if err = p.codec.Recheck(ctx, old, authorize); err != nil {
			return GraphPage{}, err
		}
	}
	if next != nil {
		if _, err = p.codec.Verify(ctx, *next, p.binding, authorize); err != nil {
			return GraphPage{}, err
		}
	}
	if p.now().Unix() >= p.expires {
		return GraphPage{}, ErrGraphPageExpired
	}
	return GraphPage{Data: data, PendingNodes: int64(pendingNodes), PendingEdges: int64(pendingEdges), NextCursor: next, SerializedItemBytes: used}, nil
}
