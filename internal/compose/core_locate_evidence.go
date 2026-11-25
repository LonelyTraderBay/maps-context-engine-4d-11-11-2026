package compose

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const locateExcerptLimit = 24
const locateEvidenceBudget = 20 << 10
const locateRouteEvidenceBudget = 8 << 10

var locateEvidenceRelations = []string{
	"ROUTE_COMPONENT", "RENDERS_COMPONENT", "RENDERS_JSX", "BINDS_EVENT",
	"CHECKS_CONDITION", "BRANCH_TRUE", "BRANCH_FALSE", "CALLS_DIRECT", "CALLS_INTERFACE",
	"READS", "WRITES", "REFERS_TO", "TEST_REF",
}

func (b *CoreBackend) locateEvidence(ctx context.Context, principal app.CorePrincipal, request model.LocateRequest,
	options app.CoreCallOptions, scope ScopeObservation, searchRequest search.Request) ([][]byte, error) {
	if request.Query == nil || request.Cursor != nil {
		return nil, app.ErrCoreInput
	}
	routeOnly := request.Focus == "route"
	goQuestion := request.Match == "question" && !routeOnly && scope.Binding.Profile != nil && len(request.Kinds) == 0 && request.Package == nil
	budget := int64(locateEvidenceBudget)
	if goQuestion {
		budget = 24 << 10
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, budget)
	if routeOnly {
		options.MaxOutputBytes = min(options.MaxOutputBytes, locateRouteEvidenceBudget)
	}
	member := scope.Vector.Members[0]
	searchRequest.ProfileID, searchRequest.GenerationID = member.ProfileID, member.GenerationID
	searchRequest.Mode, searchRequest.Query = "exact", *request.Query
	searchRequest.Limit = int(min(3, request.Limit))
	searchRequest.Kinds = request.Kinds
	if request.Path != nil {
		searchRequest.Path = *request.Path
	}
	if request.Package != nil {
		searchRequest.Package = *request.Package
	}
	checkPath := func(ctx context.Context, worktree, name, digest string, epoch int64) error {
		if worktree != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		return b.Authority.boundPath(ctx, principal, scope.Binding.Policy, name)
	}
	searchRequest.AuthorizePath = checkPath
	if goQuestion {
		blocks, err := b.locateGoQuestion(ctx, options, scope, searchRequest)
		if err != nil || blocks != nil {
			return blocks, err
		}
	}
	var result search.Result
	var err error
	if request.Match == "question" {
		if route := search.QuestionRoute(*request.Query); route != "" {
			searchRequest.Mode, searchRequest.Query = "question_route", route
			result, err = b.Reader.Index.Search(ctx, searchRequest)
			if err != nil {
				return nil, coreStoreError(err)
			}
		}
		if len(result.Symbols) == 0 {
			searchRequest.Mode, searchRequest.Query = "question", *request.Query
			result, err = b.Reader.Index.Search(ctx, searchRequest)
		}
	} else {
		result, err = b.Reader.Index.Search(ctx, searchRequest)
	}
	if err != nil {
		return nil, coreStoreError(err)
	}
	if result.WorktreeID != member.WorktreeID || result.ProfileID != member.ProfileID ||
		result.IndexIncarnation != member.IndexIncarnation || result.GenerationID != member.GenerationID ||
		result.SnapshotDigest != member.SnapshotDigest {
		return nil, ErrScopeChanged
	}
	response, err := locateResponse(options, scope.Vector, search.LocatePage{Result: result})
	if err != nil {
		return nil, err
	}
	response.Coverage.ScopeDescription = "Numbered excerpts from a bounded static neighborhood."
	appendGraphGap(&response, "BOUNDED_EVIDENCE", "Source excerpts cover selected indexed relations and declarations. Further results may exist.")
	if searchRequest.Mode == "question" {
		appendGraphGap(&response, "LEXICAL_CANDIDATES", "Question matching ranks likely symbols. Check source before relying on a match.")
	}
	if routeOnly {
		response.Coverage.ScopeDescription = "Route declaration and direct component rendering only."
	}
	if result.Truncated {
		appendGraphGap(&response, "CANDIDATES_LIMITED", "Narrow the lookup by file or symbol kind.")
	}
	data := response.Data.(model.LocateData)
	groups := make([][]model.LocatedExcerpt, 0, len(result.Symbols))
	rootFiles := make(map[string]bool)
	providerPartial := false
	n := index.NeighborhoodRequest{File: index.PublishedFileRequest{
		ProfileID: member.ProfileID, GenerationID: member.GenerationID, AuthorizeGeneration: searchRequest.AuthorizeGeneration,
		AuthorizePath: checkPath, Scanner: searchRequest.Scanner, MaxManifestBytes: searchRequest.MaxManifestBytes,
		MaxFileBytes: searchRequest.MaxFileBytes}, Direction: "both", Kinds: locateEvidenceRelations,
		Depth: int(min(3, b.limits.MaxGraphDepth)), MaxNodes: int(min(36, b.limits.MaxGraphNodes)),
		MaxEdges: int(min(48, b.limits.MaxGraphEdges)), MaxSourceBytes: min(8<<20, scope.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot),
		FollowJSXReads: !routeOnly}
	if routeOnly {
		n.Depth, n.MaxNodes, n.MaxEdges = 1, min(n.MaxNodes, 8), min(n.MaxEdges, 8)
		n.Kinds = []string{"ROUTE_COMPONENT", "RENDERS_COMPONENT"}
	}
	for _, hit := range result.Symbols {
		if hit.Source == nil || hit.Symbol.Kind == "file" {
			continue
		}
		n.Root = hit.Symbol.SymbolID
		view, err := b.Reader.Index.ReadNeighborhood(ctx, n)
		if err != nil {
			return nil, coreStoreError(err)
		}
		if view.Generation.Identity.Incarnation != member.IndexIncarnation || view.Generation.SnapshotDigest != member.SnapshotDigest {
			return nil, ErrScopeChanged
		}
		providerPartial = providerPartial || view.Coverage.Status != "supported_scope_complete"
		groups = append(groups, appendLocateExcerpts(member, view, n.Root, locateExcerptLimit/len(result.Symbols), routeOnly))
		rootFiles[hit.Source.Path] = true
	}
	if providerPartial {
		appendGraphGap(&response, "PROVIDER_COVERAGE_INCOMPLETE", "The published analyzer coverage is incomplete.")
	}
	seen := make(map[model.SourceRef]bool)
	appendExcerpt := func(excerpt model.LocatedExcerpt) {
		if !seen[excerpt.SourceRef] {
			seen[excerpt.SourceRef] = true
			data.Excerpts = append(data.Excerpts, excerpt)
		}
	}
	if len(rootFiles) > 1 {
		// Keep useful evidence from each distinct file when the name matches
		// several unrelated declarations.
		for i := range locateExcerptLimit {
			for _, group := range groups {
				if i < len(group) {
					appendExcerpt(group[i])
				}
			}
		}
	} else {
		// Within one file, keep each declaration's context together.
		for _, group := range groups {
			for _, excerpt := range group {
				appendExcerpt(excerpt)
			}
		}
	}
	for {
		response.Data = data
		blocks, err := encodeResponse(response)
		if !errors.Is(err, contextpack.ErrBudgetTooSmall) || len(data.Excerpts) == 0 {
			return blocks, err
		}
		data.Excerpts = data.Excerpts[:len(data.Excerpts)-1]
	}
}

func appendLocateExcerpts(member model.ScopeMember, view index.PublishedNeighborhood,
	root string, limit int, routeOnly bool) []model.LocatedExcerpt {
	out := make([]model.LocatedExcerpt, 0, limit)
	symbols := make(map[string]model.SymbolFact, len(view.Symbols))
	for _, symbol := range view.Symbols {
		symbols[symbol.SymbolID] = symbol
	}
	add := func(path string, span model.ByteRange, maxLines int) model.ByteRange {
		if len(out) >= limit {
			return model.ByteRange{}
		}
		file, ok := view.Files[path]
		if !ok {
			return model.ByteRange{}
		}
		bounds, line := numberedSourceLines(file.Bytes, span, maxLines)
		if line == 0 {
			return model.ByteRange{}
		}
		for _, prior := range out {
			if prior.SourceRef.Path == path && prior.SourceRef.Range.StartByte <= bounds.StartByte && prior.SourceRef.Range.EndByte >= bounds.EndByte {
				return bounds
			}
		}
		out = append(out, model.LocatedExcerpt{SourceRef: model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID,
			GenerationID: member.GenerationID, Path: path, ContentHash: file.ContentHash, Range: bounds},
			StartLine: line, Content: string(file.Bytes[bounds.StartByte:bounds.EndByte])})
		return bounds
	}
	addSymbol := func(id string, lines int) {
		if sym := symbols[id]; sym.Path != nil && sym.Range != nil {
			add(*sym.Path, *sym.Range, lines)
		}
	}
	if routeOnly {
		for _, edge := range view.Relations {
			if edge.Kind == "ROUTE_COMPONENT" || edge.Kind == "RENDERS_COMPONENT" {
				add(edge.SitePath, edge.Range, 8)
			}
		}
		// A declaration also identifies wrappers without a resolved render
		// relation. Keep it behind the explicit route and render sites.
		addSymbol(root, 8)
		return out
	}
	addSymbol(root, 26)
	for _, edge := range view.Relations {
		if edge.TargetSymbolID != nil && *edge.TargetSymbolID == root &&
			(edge.Kind == "BINDS_EVENT" || edge.Kind == "ROUTE_COMPONENT") {
			add(edge.SitePath, edge.Range, 1)
		}
	}
	// Keep declarations alongside the JSX that reads them so dynamic labels
	// can be tied to the correct event from the captured source alone.
	for _, edge := range index.RoutedJSXSites(root, view.Relations) {
		span := edge.Range
		span.EndByte = min(span.StartByte+index.RenderedJSXEvidenceBytes, int64(len(view.Files[edge.SitePath].Bytes)))
		visible := add(edge.SitePath, span, 8)
		for _, read := range view.JSXReads[edge.RelationID] {
			decl := read.Declaration
			if read.Use.StartByte >= visible.StartByte && read.Use.EndByte <= visible.EndByte &&
				decl.Path != nil && decl.Range != nil {
				add(*decl.Path, *decl.Range, 12)
			}
		}
	}
	for _, edge := range view.Relations {
		if edge.TargetSymbolID != nil && (edge.SourceSymbolID == root || *edge.TargetSymbolID == root) {
			other := edge.SourceSymbolID
			if other == root {
				other = *edge.TargetSymbolID
			}
			maxBody := int64(1800)
			if edge.Kind == "READS" || edge.Kind == "WRITES" {
				maxBody = 720
			}
			if sym := symbols[other]; sym.Range != nil && sym.Range.EndByte-sym.Range.StartByte <= maxBody {
				addSymbol(other, 24)
			}
		}
		add(edge.SitePath, edge.Range, 1)
	}
	return out
}
