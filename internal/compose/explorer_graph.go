package compose

import (
	"context"
	"errors"
	"path"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const (
	explorerGraphOrder   = "explorer-graph-v1"
	explorerCatalogOrder = "explorer-symbol-catalog-v1"
	maxExplorerGraphs    = 4
	maxProjectionBytes   = 4 << 20
)

var errExplorerCatalog = errors.New("workspace graph requires bounded symbol pages")

type explorerGraph struct {
	pager    *search.GraphPager
	scope    model.ScopeVector
	coverage model.Coverage
	expires  time.Time
}

type explorerGraphs struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]explorerGraph
}

func (g *explorerGraphs) get(id string, now time.Time) (explorerGraph, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.entries[id]
	if ok && !now.Before(entry.expires) {
		delete(g.entries, id)
		return explorerGraph{}, false
	}
	return entry, ok
}

func (g *explorerGraphs) put(id string, entry explorerGraph, now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for key, old := range g.entries {
		if !now.Before(old.expires) {
			delete(g.entries, key)
		}
	}
	if len(g.entries) >= maxExplorerGraphs {
		// Old results may expire from this process cache under bounded pressure.
		var first string
		var expiry time.Time
		for key, old := range g.entries {
			if first == "" || old.expires.Before(expiry) {
				first, expiry = key, old.expires
			}
		}
		delete(g.entries, first)
	}
	if !now.Before(entry.expires) {
		return contextpack.ErrCursorExpired
	}
	g.entries[id] = entry
	return nil
}

func (e *ExplorerReader) readGraph(ctx context.Context, p app.CorePrincipal, request model.GraphQueryRequest, options app.CoreCallOptions) ([][]byte, error) {
	if request.Directory != nil {
		return e.readDirectory(ctx, p, request, options)
	}
	if request.Neighborhood != "" || request.SymbolQuery != "" {
		return e.readRelationshipGraph(ctx, p, request, options)
	}
	nodeLimit := e.limits.GraphDefaultNodes
	edgeLimit := e.limits.GraphDefaultEdges
	if request.NodeLimit != nil {
		nodeLimit = *request.NodeLimit
	}
	if request.EdgeLimit != nil {
		edgeLimit = *request.EdgeLimit
	}
	depth := min(int64(2), e.core.limits.MaxGraphDepth)
	if request.LocalFile {
		depth = 1
	}
	if request.Depth != nil {
		depth = *request.Depth
	}
	if nodeLimit > e.limits.GraphHardNodes || edgeLimit > e.limits.GraphHardEdges || depth > e.core.limits.MaxGraphDepth {
		return nil, app.ErrCoreInput
	}
	cursor := ""
	if request.Cursor != nil {
		cursor = *request.Cursor
	}
	request.Cursor, request.NodeLimit, request.EdgeLimit, request.Depth = nil, nil, nil, &depth
	request.Kinds = slices.Clone(request.Kinds)
	slices.Sort(request.Kinds)
	for _, kind := range request.Kinds {
		if !model.ValidRelationKind(kind) {
			return nil, app.ErrCoreUnsupported
		}
	}
	request.Kinds = slices.Compact(request.Kinds)
	digest, err := model.RequestDigest("graph_query", p.ID, "GraphQueryRequest", request, 65536, 1)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	guard := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if binding.PrincipalDigest != p.Digest || binding.BindingDigest != p.BindingDigest || binding.RequestDigest != digest ||
			(binding.OrderVersion != explorerGraphOrder && binding.OrderVersion != explorerCatalogOrder) {
			return contextpack.ErrCursorInvalid
		}
		_, err := e.core.Reader.Recheck(ctx, p, binding.Scope)
		return err
	}
	select {
	case e.projection <- struct{}{}:
		defer func() { <-e.projection }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var entry explorerGraph
	var cap int64
	if cursor == "" {
		observed, err := e.core.resolveRequestScope(ctx, p, request.Scope)
		if err != nil {
			return nil, err
		}
		cap = observed.Binding.Policy.MaxOutputBytes
		if selected, ok := request.Root.(*model.RefSelector); ok && depth == 0 {
			if ref, ok := selected.Ref.(*model.FileRef); ok {
				options.MaxOutputBytes = min(options.MaxOutputBytes, cap, e.core.limits.MaxOutputBytes)
				return e.readFileNode(ctx, p, observed, selected, ref, options)
			}
		}
		binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: p.Digest, BindingDigest: p.BindingDigest,
			Scope: observed.Vector, RequestDigest: digest, OrderVersion: explorerGraphOrder}
		entry, err = e.projectGraph(ctx, p, observed, request, binding, guard)
		if errors.Is(err, errExplorerCatalog) {
			binding.OrderVersion = explorerCatalogOrder
			return e.readSymbolCatalog(ctx, p, observed, binding, guard, nil, nodeLimit, options)
		}
		if err != nil {
			return nil, err
		}
	} else {
		claims, err := e.codec.VerifyPrincipal(ctx, cursor, "list", p.Digest, guard)
		if err != nil {
			return nil, err
		}
		if claims.Binding.OrderVersion == explorerCatalogOrder {
			observed, err := e.core.Reader.Recheck(ctx, p, claims.Binding.Scope)
			if err != nil {
				return nil, err
			}
			return e.readSymbolCatalog(ctx, p, observed, claims.Binding, guard, &claims, nodeLimit, options)
		}
		var ok bool
		entry, ok = e.graphs.get(claims.Binding.ResultID, time.Now())
		if !ok {
			return nil, contextpack.ErrCursorExpired
		}
		observed, err := e.core.Reader.Recheck(ctx, p, claims.Binding.Scope)
		if err != nil {
			return nil, err
		}
		cap = observed.Binding.Policy.MaxOutputBytes
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, cap, e.core.limits.MaxOutputBytes)
	response := baseResponse(options, entry.scope)
	response.Coverage = entry.coverage
	// Reserve space for the scope, signed cursor and any coverage metadata.
	// The complete serialized envelope is measured again before delivery.
	meta, err := encodeResponse(response)
	if err != nil {
		return nil, err
	}
	available := int(options.MaxOutputBytes) - len(meta[0]) - 2048
	if available < 4 {
		return nil, contextpack.ErrBudgetTooSmall
	}
	for available >= 4 {
		page, err := entry.pager.Page(ctx, cursor, p.Digest, guard, int(nodeLimit), int(edgeLimit), min(available, 131072))
		if errors.Is(err, search.ErrGraphPageLarge) {
			return nil, contextpack.ErrItemTooLarge
		}
		if errors.Is(err, search.ErrGraphPageExpired) {
			return nil, contextpack.ErrCursorExpired
		}
		if err != nil {
			return nil, err
		}
		response.Data = page.Data
		response.Pagination.ReturnedCount = int64(len(page.Data.Nodes) + len(page.Data.Edges))
		response.Pagination.RequiredPending = &page.PendingNodes
		response.Pagination.OptionalPending = &page.PendingEdges
		response.Pagination.NextCursor = page.NextCursor
		response.Pagination.Truncated = page.NextCursor != nil
		response.Pagination.EnumerationComplete = !response.Pagination.Truncated
		response.Status = "ok"
		if response.Pagination.Truncated || response.Coverage.Status == "partial" {
			response.Status = "partial"
		}
		if response.Pagination.Truncated {
			reason := "limit"
			response.Pagination.LimitReason = &reason
		} else {
			response.Pagination.LimitReason = nil
		}
		blocks, err := encodeResponse(response)
		if err != nil {
			available /= 2
			continue
		}
		if cursor == "" && page.NextCursor != nil {
			claims, err := e.codec.VerifyPrincipal(ctx, *page.NextCursor, "list", p.Digest, guard)
			if err != nil {
				return nil, err
			}
			if err := e.graphs.put(claims.Binding.ResultID, entry, time.Now()); err != nil {
				return nil, err
			}
		}
		return blocks, nil
	}
	return nil, contextpack.ErrBudgetTooSmall
}

func (e *ExplorerReader) projectGraph(ctx context.Context, p app.CorePrincipal, observed ScopeObservation,
	request model.GraphQueryRequest, binding contextpack.CursorBinding, guard contextpack.CursorGuard) (explorerGraph, error) {
	if request.LocalFile {
		return e.projectLocalFile(ctx, p, observed, request, binding, guard)
	}
	input, maxSource, err := e.publishedRead(p, observed)
	if err != nil {
		return explorerGraph{}, err
	}
	member := observed.Vector.Members[0]
	maxNodes, maxEdges := int(e.core.limits.MaxGraphNodes), int(e.core.limits.MaxGraphEdges)
	_, workspace := request.Root.(*model.WorkspaceSelector)
	maxSymbols := 0
	if workspace {
		maxSymbols = maxNodes
	}
	graph, err := e.core.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member,
		VectorDigest: observed.Vector.VectorDigest, AuthorizeGeneration: input.AuthorizeGeneration,
		AuthorizePath: input.AuthorizePath, Scanner: input.Scanner, MaxManifestBytes: input.MaxManifestBytes,
		MaxFileBytes: input.MaxFileBytes, MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000,
		MaxSymbols: maxSymbols})
	catalogFallback := workspace && len(request.Kinds) == 0
	if catalogFallback && errors.Is(err, index.ErrCaptureLimit) {
		return explorerGraph{}, errExplorerCatalog
	}
	if err != nil {
		return explorerGraph{}, err
	}
	nodes, edges, err := explorerGraphFacts(ctx, graph, observed.Vector, request, maxNodes, maxEdges)
	if catalogFallback && (errors.Is(err, search.ErrGraphPageLarge) || errors.Is(err, app.ErrCoreLimit)) {
		return explorerGraph{}, errExplorerCatalog
	}
	if errors.Is(err, search.ErrGraphPageLarge) {
		return explorerGraph{}, app.ErrCoreLimit
	}
	if err != nil {
		return explorerGraph{}, err
	}
	pager, err := search.NewGraphPager(ctx, e.codec, binding, guard, input.Scanner, nodes, edges, true, maxNodes, maxEdges, maxProjectionBytes)
	if errors.Is(err, search.ErrGraphPageLarge) {
		if catalogFallback {
			return explorerGraph{}, errExplorerCatalog
		}
		return explorerGraph{}, app.ErrCoreLimit
	}
	if err != nil {
		return explorerGraph{}, err
	}
	if err := graph.VerifyCatalog(ctx, observed.Vector.Members[0]); err != nil {
		return explorerGraph{}, err
	}
	return explorerGraph{pager: pager, scope: observed.Vector, coverage: graph.Coverage, expires: time.Now().Add(e.graphs.ttl)}, nil
}

func (e *ExplorerReader) projectLocalFile(ctx context.Context, principal app.CorePrincipal, observed ScopeObservation,
	request model.GraphQueryRequest, binding contextpack.CursorBinding, guard contextpack.CursorGuard) (explorerGraph, error) {
	root := request.Root.(*model.RefSelector)
	ref := root.Ref.(*model.FileRef)
	member, err := model.MatchPinnedMember(observed.Vector, ref, root.ServiceID)
	if err != nil {
		return explorerGraph{}, ErrAuthority
	}
	input, _, err := e.publishedRead(principal, observed)
	if err != nil {
		return explorerGraph{}, err
	}
	maxNodes := min(int(e.core.limits.MaxGraphNodes), 129)
	maxEdges := min(int(e.core.limits.MaxGraphEdges), 300)
	if maxNodes < 2 || maxEdges < 1 {
		return explorerGraph{}, app.ErrCoreLimit
	}
	input.Path = ref.Path
	local, err := e.core.Reader.Index.ReadPublishedFileSymbols(ctx, index.PublishedFileSymbolsRequest{
		File: input, MaxSymbols: maxNodes - 1, MaxFactsBytes: 1 << 20, MaxRelations: maxEdges,
	})
	if err != nil {
		return explorerGraph{}, err
	}
	if local.File.IndexIncarnation != member.IndexIncarnation || local.File.SnapshotDigest != member.SnapshotDigest ||
		local.File.PolicyDigest != member.PolicyDigest || local.File.PolicyEpoch != member.PolicyEpoch {
		return explorerGraph{}, ErrScopeChanged
	}
	if ref.ContentHash == nil || *ref.ContentHash != local.File.ContentHash {
		return explorerGraph{}, app.ErrValidationPrecondition
	}
	fileNode, err := explorerFileNode(ref, int64(len(local.File.Bytes)))
	if err != nil {
		return explorerGraph{}, err
	}
	references := make([]contextpack.Reference, 0, len(local.Relations))
	for _, relation := range local.Relations {
		source := fileNode.SourceRefs[0]
		source.Range = relation.Range
		references = append(references, contextpack.Reference{Relation: relation, Source: source})
	}
	nodes, edges, err := overview.ProjectFileFacts(ctx, observed.Vector, ref.GraphIdentity, ref.Path, local.File.ContentHash,
		local.Symbols, references, maxNodes-1, maxEdges)
	if err != nil {
		return explorerGraph{}, err
	}
	aliases := map[string]bool{}
	nodes = slices.DeleteFunc(nodes, func(node model.GraphNode) bool {
		if node.Kind != "file" {
			return false
		}
		aliases[node.NodeID] = true
		return true
	})
	for i := range edges {
		if aliases[edges[i].FromNodeID] {
			edges[i].FromNodeID = fileNode.NodeID
		}
		if aliases[edges[i].ToNodeID] {
			edges[i].ToNodeID = fileNode.NodeID
		}
	}
	gaps := []model.Gap{{Code: "FILE_LOCAL_SCOPE", Reason: "Chỉ hiển thị quan hệ đã ghi giữa các ký hiệu trong tệp.", SourceRefs: []model.SourceRef{}}}
	if local.Truncated {
		gaps = append(gaps, model.Gap{Code: "FILE_GRAPH_LIMIT", Reason: "Tệp có thêm ký hiệu hoặc cạnh ngoài giới hạn của bản đồ này.", SourceRefs: []model.SourceRef{}})
	}
	budget := min(observed.Binding.Policy.MaxOutputBytes, e.core.limits.MaxOutputBytes, 131072)
	if splitNodeSource(&fileNode, local.File.Bytes, int(budget)) {
		gaps = append(gaps, model.Gap{Code: "FILE_SOURCE_PREVIEW", Reason: "Mã nguồn tệp lớn được xem theo các đoạn đầu. Chọn ký hiệu để đọc vùng tương ứng.", SourceRefs: []model.SourceRef{}})
	}
	nodes = append(nodes, fileNode)
	pager, err := search.NewGraphPager(ctx, e.codec, binding, guard, input.Scanner, nodes, edges, true, maxNodes, maxEdges, maxProjectionBytes)
	if err != nil {
		return explorerGraph{}, err
	}
	pager.PrioritizeNode(fileNode.NodeID)
	coverage := model.Coverage{Status: "partial", Capabilities: []model.Capability{}, ScopeDescription: "Local graph for one captured file.", Gaps: gaps}
	return explorerGraph{pager: pager, scope: observed.Vector, coverage: coverage, expires: time.Now().Add(e.graphs.ttl)}, nil
}

func splitNodeSource(node *model.GraphNode, source []byte, budget int) bool {
	// Reserve room for scope and JSON escaping. A source request reads one slice.
	size := max(4, min(8192, (budget-4096)/6))
	template := node.SourceRefs[0]
	start, limit := int(template.Range.StartByte), int(template.Range.EndByte)
	if limit-start <= size {
		return false
	}
	node.SourceRefs = nil
	for start < limit && len(node.SourceRefs) < 16 {
		end := min(start+size, limit)
		for end < limit && !utf8.RuneStart(source[end]) {
			end--
		}
		ref := template
		ref.Range = model.ByteRange{StartByte: int64(start), EndByte: int64(end)}
		node.SourceRefs = append(node.SourceRefs, ref)
		start = end
	}
	return start < limit
}

// readFileNode serves an exact depth-zero FileRef from captured bytes. A
// single node has no continuation and needs no provider graph materialization.
func (e *ExplorerReader) readFileNode(ctx context.Context, p app.CorePrincipal, observed ScopeObservation,
	selected *model.RefSelector, ref *model.FileRef, options app.CoreCallOptions) ([][]byte, error) {
	member, err := model.MatchPinnedMember(observed.Vector, ref, selected.ServiceID)
	if err != nil {
		return nil, ErrAuthority
	}
	input, maxSource, err := e.publishedRead(p, observed)
	if err != nil {
		return nil, err
	}
	input.Path = ref.Path
	input.MaxFileBytes = int(min(int64(input.MaxFileBytes), maxSource))
	file, err := e.core.Reader.Index.ReadPublishedFile(ctx, input)
	if errors.Is(err, index.ErrSnapshotUnavailable) {
		return nil, ErrScopeSnapshotExpired
	}
	if errors.Is(err, index.ErrPublishedFileUnavailable) {
		return nil, app.ErrValidationPrecondition
	}
	if err != nil {
		return nil, err
	}
	if file.IndexIncarnation != member.IndexIncarnation || file.SnapshotDigest != member.SnapshotDigest ||
		file.PolicyDigest != member.PolicyDigest || file.PolicyEpoch != member.PolicyEpoch {
		return nil, ErrScopeChanged
	}
	if ref.ContentHash == nil || *ref.ContentHash != file.ContentHash {
		return nil, app.ErrValidationPrecondition
	}
	node, err := explorerFileNode(ref, int64(len(file.Bytes)))
	if err != nil {
		return nil, err
	}
	nodes, edges := []model.GraphNode{node}, []model.GraphEdge{}
	digest, err := model.SubgraphDigest(observed.Vector, nodes, edges, maxProjectionBytes)
	if err != nil {
		return nil, err
	}
	response := baseResponse(options, observed.Vector)
	response.Coverage.ScopeDescription = "Exact captured file node. Provider graph coverage was not inspected."
	response.Data = model.GraphData{Operation: "graph_query", Nodes: nodes, Edges: edges, SubgraphDigest: digest}
	response.Pagination.ReturnedCount = 1
	blocks, err := encodeResponse(response)
	if err != nil {
		return nil, contextpack.ErrItemTooLarge
	}
	return blocks, nil
}

func explorerFileNode(ref *model.FileRef, size int64) (model.GraphNode, error) {
	id, err := model.GraphRefDigest(ref, 8192)
	if err != nil {
		return model.GraphNode{}, err
	}
	source := model.SourceRef{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID,
		Path: ref.Path, ContentHash: *ref.ContentHash, Range: model.ByteRange{EndByte: size}}
	return model.GraphNode{NodeID: id, Ref: ref, Kind: "file", Label: path.Base(ref.Path),
		Plane: "observed_static", SourceRefs: []model.SourceRef{source}, EvidenceRefs: []model.EvidenceRef{}}, nil
}

func explorerGraphFacts(ctx context.Context, graph *index.PublishedGraph, scope model.ScopeVector,
	request model.GraphQueryRequest, maxNodes, maxEdges int,
) ([]model.GraphNode, []model.GraphEdge, error) {
	member := scope.Members[0]
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	full := false
	rootID := ""
	var fileRoot *model.FileRef
	var fileNode model.GraphNode
	switch root := request.Root.(type) {
	case *model.WorkspaceSelector:
		full = true
	case *model.SymbolSelector:
		rootID = root.SymbolID
	case *model.RefSelector:
		if _, err := model.MatchPinnedMember(scope, root.Ref, root.ServiceID); err != nil {
			return nil, nil, ErrAuthority
		}
		switch ref := root.Ref.(type) {
		case *model.SymbolRef:
			rootID = ref.SymbolID
		case *model.FileRef:
			file, ok := graph.Snapshot.File(ref.Path)
			if !ok || ref.ContentHash == nil || *ref.ContentHash != file.ContentHash() {
				return nil, nil, app.ErrValidationPrecondition
			}
			node, err := explorerFileNode(ref, int64(file.Len()))
			if err != nil {
				return nil, nil, err
			}
			fileRoot, fileNode = ref, node
			if *request.Depth == 0 {
				return []model.GraphNode{fileNode}, []model.GraphEdge{}, ctx.Err()
			}
		default:
			return nil, nil, app.ErrCoreUnsupported
		}
	default:
		return nil, nil, app.ErrCoreUnsupported
	}
	selected := make(map[string]bool)
	nodes := graph.Symbols
	if !full {
		seeds := make([]string, 0)
		for _, item := range graph.Symbols {
			if item.SymbolID == rootID || fileRoot != nil && item.Path != nil && *item.Path == fileRoot.Path {
				seeds = append(seeds, item.SymbolID)
				selected[item.SymbolID] = true
			}
		}
		if len(seeds) == 0 && fileRoot == nil {
			return nil, nil, app.ErrCoreInput
		}
		if err := expandExplorerScope(ctx, graph.Relations, request.Kinds, *request.Depth, seeds, selected, maxNodes); err != nil {
			return nil, nil, err
		}
		nodes = make([]model.SymbolFact, 0, len(selected))
		for _, item := range graph.Symbols {
			if selected[item.SymbolID] {
				nodes = append(nodes, item)
			}
		}
	}
	refs := make([]contextpack.Reference, 0, min(len(graph.Relations), maxEdges))
	for _, item := range graph.Relations {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if len(request.Kinds) != 0 && !slices.Contains(request.Kinds, item.Kind) {
			continue
		}
		if !full && (!selected[item.SourceSymbolID] || item.TargetSymbolID != nil && !selected[*item.TargetSymbolID] ||
			item.TargetSymbolID == nil && *request.Depth == 0) {
			continue
		}
		if len(refs) >= maxEdges {
			return nil, nil, app.ErrCoreLimit
		}
		file, ok := graph.Snapshot.File(item.SitePath)
		if !ok {
			return nil, nil, app.ErrCoreBackend
		}
		ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
			Path: item.SitePath, ContentHash: file.ContentHash(), Range: item.Range}
		refs = append(refs, contextpack.Reference{Relation: item, Source: ref})
	}
	if fileRoot != nil {
		maxNodes--
	}
	projected, edges, err := overview.ProjectFacts(ctx, graph.Snapshot, scope, nil, identity, nodes, refs, maxNodes, maxEdges)
	if err == nil && fileRoot != nil {
		projected = append(projected, fileNode)
	}
	return projected, edges, err
}

func expandExplorerScope(ctx context.Context, relations []model.RelationFact, kinds []string, depth int64,
	seeds []string, selected map[string]bool, maxNodes int) error {
	if len(selected) > maxNodes {
		return app.ErrCoreLimit
	}
	if depth == 0 {
		return ctx.Err()
	}
	neighbors := make(map[string][]string)
	for _, item := range relations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.TargetSymbolID == nil || len(kinds) != 0 && !slices.Contains(kinds, item.Kind) {
			continue
		}
		target := *item.TargetSymbolID
		neighbors[item.SourceSymbolID] = append(neighbors[item.SourceSymbolID], target)
		neighbors[target] = append(neighbors[target], item.SourceSymbolID)
	}
	for i := int64(0); i < depth && len(seeds) != 0; i++ {
		next := []string{}
		for _, id := range seeds {
			for _, adjacent := range neighbors[id] {
				if selected[adjacent] {
					continue
				}
				if len(selected) == maxNodes {
					return app.ErrCoreLimit
				}
				selected[adjacent] = true
				next = append(next, adjacent)
			}
		}
		seeds = next
	}
	return ctx.Err()
}
