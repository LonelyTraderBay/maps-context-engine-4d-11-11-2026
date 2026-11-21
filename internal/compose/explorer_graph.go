package compose

import (
	"context"
	"errors"
	"path"
	"slices"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const (
	explorerGraphOrder = "explorer-graph-v1"
	maxExplorerGraphs  = 4
	maxProjectionBytes = 4 << 20
)

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
	nodeLimit := e.limits.GraphDefaultNodes
	edgeLimit := e.limits.GraphDefaultEdges
	if request.NodeLimit != nil {
		nodeLimit = *request.NodeLimit
	}
	if request.EdgeLimit != nil {
		edgeLimit = *request.EdgeLimit
	}
	depth := min(int64(2), e.core.limits.MaxGraphDepth)
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
		if binding.PrincipalDigest != p.Digest || binding.BindingDigest != p.BindingDigest || binding.OrderVersion != explorerGraphOrder || binding.RequestDigest != digest {
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
		binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: p.Digest, BindingDigest: p.BindingDigest,
			Scope: observed.Vector, RequestDigest: digest, OrderVersion: explorerGraphOrder}
		entry, err = e.projectGraph(ctx, p, observed, request, binding, guard)
		if err != nil {
			return nil, err
		}
	} else {
		claims, err := e.codec.VerifyPrincipal(ctx, cursor, "list", p.Digest, guard)
		if err != nil {
			return nil, err
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
		response.Pagination.EnumerationComplete = page.NextCursor == nil
		response.Status = "ok"
		if page.NextCursor != nil || response.Coverage.Status == "partial" {
			response.Status = "partial"
		}
		if page.NextCursor != nil {
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
	input, maxSource, err := e.publishedRead(p, observed)
	if err != nil {
		return explorerGraph{}, err
	}
	graph, err := e.core.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: observed.Vector.Members[0],
		VectorDigest: observed.Vector.VectorDigest, AuthorizeGeneration: input.AuthorizeGeneration,
		AuthorizePath: input.AuthorizePath, Scanner: input.Scanner, MaxManifestBytes: input.MaxManifestBytes,
		MaxFileBytes: input.MaxFileBytes, MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000})
	if err != nil {
		return explorerGraph{}, err
	}
	maxNodes, maxEdges := int(e.core.limits.MaxGraphNodes), int(e.core.limits.MaxGraphEdges)
	nodes, edges, err := explorerGraphFacts(ctx, graph, observed.Vector, request, maxNodes, maxEdges)
	if errors.Is(err, search.ErrGraphPageLarge) {
		return explorerGraph{}, app.ErrCoreLimit
	}
	if err != nil {
		return explorerGraph{}, err
	}
	pager, err := search.NewGraphPager(ctx, e.codec, binding, guard, input.Scanner, nodes, edges, true, maxNodes, maxEdges, maxProjectionBytes)
	if errors.Is(err, search.ErrGraphPageLarge) {
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
			id, err := model.GraphRefDigest(ref, 8192)
			if err != nil {
				return nil, nil, err
			}
			source := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
				Path: ref.Path, ContentHash: file.ContentHash(), Range: model.ByteRange{EndByte: int64(file.Len())}}
			fileRoot = ref
			fileNode = model.GraphNode{NodeID: id, Ref: ref, Kind: "file", Label: path.Base(ref.Path),
				Plane: "observed_static", SourceRefs: []model.SourceRef{source}, EvidenceRefs: []model.EvidenceRef{}}
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
