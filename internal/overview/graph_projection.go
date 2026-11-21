package overview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// ProjectedGraph retains selection coverage and gaps beside its materialized
// pages. Transport must account for the complete outer response separately.
type ProjectedGraph struct {
	pager     *search.GraphPager
	selection *contextpack.Selection
	coverage  model.Coverage
	gaps      []model.Gap
}

type ProjectedGraphPage struct {
	Result   search.GraphPage
	Coverage model.Coverage
	Gaps     []model.Gap
}

func (p *ProjectedGraph) Page(ctx context.Context, cursor, principal string, current contextpack.CursorGuard, nodeLimit, edgeLimit, maxItemBytes int) (ProjectedGraphPage, error) {
	if p == nil || p.pager == nil || p.selection == nil {
		return ProjectedGraphPage{}, ErrInput
	}
	if err := p.selection.Check(ctx); err != nil {
		return ProjectedGraphPage{}, err
	}
	metadata, err := json.Marshal(struct {
		Coverage model.Coverage
		Gaps     []model.Gap
	}{p.coverage, p.gaps})
	if err != nil {
		return ProjectedGraphPage{}, ErrInput
	}
	var copied struct {
		Coverage model.Coverage
		Gaps     []model.Gap
	}
	if json.Unmarshal(metadata, &copied) != nil {
		return ProjectedGraphPage{}, ErrInput
	}
	result, err := p.pager.Page(ctx, cursor, principal, current, nodeLimit, edgeLimit, maxItemBytes)
	if err != nil {
		return ProjectedGraphPage{}, err
	}
	if err = p.selection.Check(ctx); err != nil {
		return ProjectedGraphPage{}, err
	}
	return ProjectedGraphPage{Result: result, Coverage: copied.Coverage, Gaps: copied.Gaps}, nil
}

func graphIdentityID(kind string, identity model.GraphIdentity, id string) string {
	raw, _ := json.Marshal(struct {
		Kind     string              `json:"kind"`
		Identity model.GraphIdentity `json:"identity"`
		ID       string              `json:"id"`
	}{kind, identity, id})
	sum := sha256.Sum256(append([]byte("mce-graph-projection-v1\n"), raw...))
	return kind + "-" + hex.EncodeToString(sum[:])
}

// CapturedSymbolNode keeps the canonical node identity common to the complete
// projection and the Explorer's bounded symbol catalog.
func CapturedSymbolNode(identity model.GraphIdentity, symbol model.SymbolFact, source *model.SourceRef) model.GraphNode {
	node := model.GraphNode{NodeID: graphIdentityID("symbol", identity, symbol.SymbolID),
		Ref:  &model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: symbol.SymbolID},
		Kind: symbol.Kind, Label: symbol.Name, Plane: "annotation",
		SourceRefs: []model.SourceRef{}, EvidenceRefs: []model.EvidenceRef{}}
	if source != nil {
		node.Plane = "observed_static"
		node.SourceRefs = append(node.SourceRefs, *source)
	}
	return node
}

// NewProjectedGraph walks the verified, pinned fact catalog once and gives the
// complete materialization to T025's signed pager. Incomplete traversal is an
// explicit error; no cursor restarts a partial walk against a newer snapshot.
// The caller owns bounded retention and real scope/fact/permission authorities.
func NewProjectedGraph(ctx context.Context, input contextpack.Input, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard, maxNodes, maxEdges, maxBytes int) (*ProjectedGraph, error) {
	if ctx == nil || codec == nil || guard == nil || input.Snapshot == nil || input.Scanner == nil || binding.Purpose != "list" || binding.ResultID != "" || binding.ResultDigest != "" || maxNodes < 1 || maxNodes > 100000 || maxEdges < 1 || maxEdges > 1000000 || maxBytes < 1024 || maxBytes > 16*1024*1024 || model.ComparePinnedScope(input.Scope, binding.Scope, 32768) != nil {
		return nil, ErrInput
	}
	preToken, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
	if err != nil {
		return nil, err
	}
	preClaims, err := codec.Verify(ctx, preToken, binding, guard)
	if err != nil {
		return nil, err
	}
	selection, err := contextpack.Select(ctx, input)
	if err != nil {
		return nil, err
	}
	report, err := selection.Read(ctx)
	if err != nil {
		return nil, err
	}
	if !report.EnumerationComplete {
		return nil, search.ErrGraphIncomplete
	}
	identity := model.GraphIdentity{WorktreeID: report.Target.WorktreeID, ProfileID: report.Target.ProfileID, GenerationID: report.Target.GenerationID}
	ordered, edges, err := ProjectFacts(ctx, input.Snapshot, report.Scope, input.ServiceID, identity, report.Nodes, report.References, maxNodes, maxEdges)
	if err != nil {
		return nil, err
	}
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := guard(ctx, b); err != nil {
			return err
		}
		return selection.Check(ctx)
	}
	pager, err := search.NewGraphPager(ctx, codec, binding, check, input.Scanner, ordered, edges, true, maxNodes, maxEdges, maxBytes)
	if err != nil {
		return nil, err
	}
	if err = selection.Check(ctx); err != nil {
		return nil, err
	}
	if err = codec.Recheck(ctx, preClaims, guard); err != nil {
		return nil, err
	}
	return &ProjectedGraph{pager: pager, selection: selection, coverage: report.Coverage, gaps: append([]model.Gap{}, report.Gaps...)}, nil
}

// ProjectFacts converts verified facts and their captured evidence into a
// renderer-neutral graph. Selection, access checks and pagination are owned
// by its callers.
func ProjectFacts(ctx context.Context, captured *snapshot.Snapshot, scope model.ScopeVector, serviceID *string, identity model.GraphIdentity,
	symbols []model.SymbolFact, references []contextpack.Reference, maxNodes, maxEdges int,
) ([]model.GraphNode, []model.GraphEdge, error) {
	if len(symbols) > maxNodes || len(references) > maxEdges {
		return nil, nil, search.ErrGraphPageLarge
	}
	nodes := map[string]model.GraphNode{}
	addSymbol := func(id string) string {
		nodeID := graphIdentityID("symbol", identity, id)
		if _, exists := nodes[nodeID]; !exists {
			nodes[nodeID] = model.GraphNode{NodeID: nodeID, Ref: &model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: id}, Kind: "boundary", Label: "", Plane: "annotation", EvidenceRefs: []model.EvidenceRef{}, SourceRefs: []model.SourceRef{}}
		}
		return nodeID
	}
	for _, symbol := range symbols {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var source *model.SourceRef
		if symbol.Path != nil && symbol.Range != nil {
			file, ok := captured.File(*symbol.Path)
			if !ok {
				return nil, nil, ErrInput
			}
			ref := model.SourceRef{WorktreeID: identity.WorktreeID, ProfileID: identity.ProfileID, GenerationID: identity.GenerationID, Path: *symbol.Path, ContentHash: file.ContentHash(), Range: *symbol.Range}
			if _, e := model.MatchPinnedSource(scope, ref, serviceID, 32768); e != nil {
				return nil, nil, ErrInput
			}
			source = &ref
		}
		node := CapturedSymbolNode(identity, symbol, source)
		nodes[node.NodeID] = node
	}
	edges := make([]model.GraphEdge, 0, len(references))
	for _, reference := range references {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		relation := reference.Relation
		from := addSymbol(relation.SourceSymbolID)
		var to, status string
		if relation.TargetSymbolID == nil {
			to = graphIdentityID("boundary", identity, relation.RelationID)
			hash := reference.Source.ContentHash
			nodes[to] = model.GraphNode{NodeID: to, Ref: &model.FileRef{Kind: "file", GraphIdentity: identity, Path: reference.Source.Path, ContentHash: &hash}, Kind: "unresolved_boundary", Label: "", Plane: "annotation", EvidenceRefs: []model.EvidenceRef{}, SourceRefs: []model.SourceRef{reference.Source}}
			status = "unresolved"
		} else {
			to = addSymbol(*relation.TargetSymbolID)
			switch {
			case relation.Kind == "MAY_DISPATCH_TO", relation.EvidenceKind == "type_possible", relation.EvidenceKind == "text_match":
				status = "candidate"
			default:
				status = "exact_in_supported_scope"
			}
		}
		evidence := model.EvidenceRef{EvidenceID: relation.RelationID, Origin: "static_analyzer", Kind: relation.EvidenceKind, SourceRefs: []model.SourceRef{reference.Source}}
		edges = append(edges, model.GraphEdge{EdgeID: graphIdentityID("relation", identity, relation.RelationID), FromNodeID: from, ToNodeID: to, RelationKind: relation.Kind, ResolutionStatus: status, EvidenceRefs: []model.EvidenceRef{evidence}})
	}
	if len(nodes) > maxNodes || len(edges) > maxEdges {
		return nil, nil, search.ErrGraphPageLarge
	}
	ordered := make([]model.GraphNode, 0, len(nodes))
	for _, node := range nodes {
		ordered = append(ordered, node)
	}
	return ordered, edges, nil
}
