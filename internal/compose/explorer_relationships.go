package compose

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

var functionalRelations = []string{"ROUTE_COMPONENT", "RENDERS_COMPONENT", "RENDERS_JSX", "BINDS_EVENT",
	"CHECKS_CONDITION", "BRANCH_TRUE", "BRANCH_FALSE", "CALLS_DIRECT", "CALLS_INTERFACE", "IMPORTS", "REFERS_TO"}

func (e *ExplorerReader) readRelationshipGraph(ctx context.Context, principal app.CorePrincipal,
	request model.GraphQueryRequest, options app.CoreCallOptions) ([][]byte, error) {
	observed, err := e.core.resolveRequestScope(ctx, principal, request.Scope)
	if err != nil {
		return nil, err
	}
	input, maxSource, err := e.publishedRead(principal, observed)
	if err != nil {
		return nil, err
	}
	member := observed.Vector.Members[0]
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	options.MaxOutputBytes = min(options.MaxOutputBytes, observed.Binding.Policy.MaxOutputBytes, e.core.limits.MaxOutputBytes)
	response := baseResponse(options, observed.Vector)
	nodes, edges := []model.GraphNode{}, []model.GraphEdge{}
	var rootID string
	if request.SymbolQuery != "" {
		limit := int(min(15, e.limits.GraphHardNodes))
		if request.NodeLimit != nil {
			limit = min(limit, int(*request.NodeLimit))
		}
		found, err := e.core.Reader.Index.Search(ctx, search.Request{
			ProfileID: member.ProfileID, GenerationID: member.GenerationID, Mode: "prefix", Query: request.SymbolQuery,
			Limit: limit, MaxOutputBytes: 1 << 20, MaxManifestBytes: input.MaxManifestBytes, MaxFileBytes: input.MaxFileBytes,
			AuthorizeGeneration: input.AuthorizeGeneration, AuthorizePath: input.AuthorizePath, Scanner: input.Scanner,
		})
		if err != nil {
			return nil, err
		}
		if found.WorktreeID != member.WorktreeID || found.ProfileID != member.ProfileID || found.GenerationID != member.GenerationID ||
			found.IndexIncarnation != member.IndexIncarnation || found.SnapshotDigest != member.SnapshotDigest {
			return nil, ErrScopeChanged
		}
		for _, hit := range found.Symbols {
			nodes = append(nodes, overview.CapturedSymbolNode(identity, hit.Symbol, hit.Source))
		}
		response.Coverage.ScopeDescription = "Prefix search in the pinned symbol index."
		if found.Truncated {
			appendGraphGap(&response, "SYMBOL_SEARCH_LIMIT", "Còn kết quả. Nhập thêm ký tự để thu hẹp tìm kiếm.")
		}
	} else {
		selected, ok := request.Root.(*model.RefSelector)
		if !ok {
			return nil, app.ErrCoreInput
		}
		ref, ok := selected.Ref.(*model.SymbolRef)
		if !ok {
			return nil, app.ErrCoreInput
		}
		if _, err := model.MatchPinnedMember(observed.Vector, ref, selected.ServiceID); err != nil {
			return nil, ErrAuthority
		}
		depth := min(2, e.core.limits.MaxGraphDepth)
		if request.Depth != nil {
			depth = *request.Depth
		}
		if depth < 1 || depth > min(4, e.core.limits.MaxGraphDepth) {
			return nil, app.ErrCoreInput
		}
		nodeLimit := min(28, e.core.limits.MaxGraphNodes, e.limits.GraphHardNodes)
		edgeLimit := min(36, e.core.limits.MaxGraphEdges, e.limits.GraphHardEdges)
		if request.NodeLimit != nil {
			nodeLimit = min(nodeLimit, *request.NodeLimit)
		}
		if request.EdgeLimit != nil {
			edgeLimit = min(edgeLimit, *request.EdgeLimit)
		}
		kinds := request.Kinds
		if len(kinds) == 0 {
			kinds = functionalRelations
		}
		view, err := e.core.Reader.Index.ReadNeighborhood(ctx, index.NeighborhoodRequest{File: input,
			Root: ref.SymbolID, Direction: request.Neighborhood, Kinds: kinds, Depth: int(depth), MaxNodes: int(nodeLimit),
			MaxEdges: int(edgeLimit), MaxSourceBytes: maxSource})
		if errors.Is(err, index.ErrPublishedFileUnavailable) {
			return nil, app.ErrValidationPrecondition
		}
		if err != nil {
			return nil, err
		}
		if view.Generation.Identity.Incarnation != member.IndexIncarnation || view.Generation.SnapshotDigest != member.SnapshotDigest {
			return nil, ErrScopeChanged
		}
		hashes := make(map[string]string, len(view.Files))
		for name, source := range view.Files {
			hashes[name] = source.ContentHash
		}
		refs := make([]contextpack.Reference, len(view.Relations))
		for i, rel := range view.Relations {
			span := rel.Range
			if rel.Kind == "READS" || rel.Kind == "WRITES" {
				span = variableUsageLine(view.Files[rel.SitePath].Bytes, span, int(options.MaxOutputBytes))
			}
			refs[i] = contextpack.Reference{Relation: rel, Source: model.SourceRef{WorktreeID: member.WorktreeID,
				ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: rel.SitePath,
				ContentHash: hashes[rel.SitePath], Range: span}}
		}
		nodes, edges, err = overview.ProjectCapturedFacts(ctx, observed.Vector, identity, hashes, view.Symbols, refs, int(nodeLimit), int(edgeLimit))
		if err != nil {
			return nil, err
		}
		response.Coverage = view.Coverage
		appendGraphGap(&response, "STATIC_RELATIONSHIPS", "Mũi tên thể hiện quan hệ xác định từ mã nguồn.")
		response.Coverage.ScopeDescription = "Bounded static neighborhood of a single captured symbol."
		if view.Limited {
			appendGraphGap(&response, "NEIGHBORHOOD_LIMIT", "Khung nhìn đã giới hạn số nhánh. Chọn một nút để mở tiếp.")
		}
		sourceLimited := false
		for i := range nodes {
			if root, ok := nodes[i].Ref.(*model.SymbolRef); ok && root.SymbolID == ref.SymbolID {
				rootID = nodes[i].NodeID
			}
			if len(nodes[i].SourceRefs) == 1 {
				file := view.Files[nodes[i].SourceRefs[0].Path]
				if splitNodeSource(&nodes[i], file.Bytes, int(options.MaxOutputBytes)) {
					sourceLimited = true
				}
			}
		}
		if rootID == "" {
			return nil, app.ErrCoreBackend
		}
		if sourceLimited {
			appendGraphGap(&response, "NEIGHBORHOOD_SOURCE_LIMIT", "Định nghĩa dài hiển thị theo các đoạn đầu trong phạm vi ngân sách.")
		}
		slices.SortFunc(nodes, func(a, b model.GraphNode) int { return strings.Compare(a.NodeID, b.NodeID) })
	}
	return fitRelationshipGraph(ctx, response, nodes, edges, rootID)
}

func variableUsageLine(source []byte, anchor model.ByteRange, budget int) model.ByteRange {
	start := bytes.LastIndexByte(source[:anchor.StartByte], '\n') + 1
	end := len(source)
	if next := bytes.IndexByte(source[anchor.EndByte:], '\n'); next >= 0 {
		end = int(anchor.EndByte) + next
	}
	if end-start > min(2048, max(4, (budget-4096)/6)) {
		return anchor
	}
	return model.ByteRange{StartByte: int64(start), EndByte: int64(end)}
}

func appendGraphGap(response *model.Response, code, reason string) {
	response.Status = "partial"
	response.Coverage.Status = "partial"
	if len(response.Coverage.Gaps) < 64 {
		response.Coverage.Gaps = append(response.Coverage.Gaps, model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{}})
	}
}

func fitRelationshipGraph(ctx context.Context, response model.Response, nodes []model.GraphNode,
	edges []model.GraphEdge, root string) ([][]byte, error) {
	pageNodes, pageEdges := nodes, edges
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		digest, err := model.SubgraphDigest(*response.Scope, pageNodes, pageEdges, maxProjectionBytes)
		if err != nil {
			return nil, err
		}
		response.Data = model.GraphData{Operation: "graph_query", Nodes: pageNodes, Edges: pageEdges, SubgraphDigest: digest}
		response.Pagination.ReturnedCount = int64(len(pageNodes) + len(pageEdges))
		blocks, err := encodeResponse(response)
		if err == nil || !errors.Is(err, contextpack.ErrBudgetTooSmall) {
			return blocks, err
		}
		if len(pageNodes) == len(nodes) && len(pageEdges) == len(edges) {
			appendGraphGap(&response, "GRAPH_OUTPUT_LIMIT", "Kết quả hiển thị trong ngân sách của phiên Explorer.")
		}
		if root == "" {
			if len(pageNodes) <= 1 {
				return nil, err
			}
			pageNodes = pageNodes[:len(pageNodes)-1]
			continue
		}
		if len(pageEdges) <= 1 {
			return nil, err
		}
		pageEdges = pageEdges[:len(pageEdges)-1]
		wanted := map[string]bool{root: true}
		for _, edge := range pageEdges {
			wanted[edge.FromNodeID], wanted[edge.ToNodeID] = true, true
		}
		pageNodes = make([]model.GraphNode, 0, len(wanted))
		for _, node := range nodes {
			if wanted[node.NodeID] {
				pageNodes = append(pageNodes, node)
			}
		}
	}
}
