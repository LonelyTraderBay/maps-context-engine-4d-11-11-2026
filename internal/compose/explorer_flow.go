package compose

import (
	"context"
	"errors"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// staticFlowOwner computes only a projection of independently verified Go
// facts for the supplied published graph and exact retained scope.
type staticFlowOwner func(context.Context, ScopeObservation, *index.PublishedGraph, model.SymbolRef, flowview.Options) (flowview.Result, error)

func (e *ExplorerReader) readFlow(ctx context.Context, principal app.CorePrincipal, request model.FlowQueryRequest,
	options app.CoreCallOptions) ([][]byte, error) {
	if e.staticFlow == nil || request.ArtifactID != nil || request.Cursor != nil || request.Mode != nil && *request.Mode != "static" {
		return nil, app.ErrCoreUnsupported
	}
	root, ok := request.RootRef.(*model.SymbolRef)
	if !ok || root == nil {
		return nil, app.ErrCoreUnsupported
	}
	select {
	case e.projection <- struct{}{}:
		defer func() { <-e.projection }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	observed, err := e.core.resolveRequestScope(ctx, principal, request.Scope)
	if err != nil {
		return nil, err
	}
	member, err := model.MatchPinnedMember(observed.Vector, root, nil)
	if err != nil || len(observed.Vector.Members) != 1 {
		return nil, ErrAuthority
	}
	input, total, err := e.publishedRead(principal, observed)
	if err != nil {
		return nil, err
	}
	// Warm flow projects from a bounded, admitted source capture. Large
	// captures remain queryable through graph/source with their own budgets.
	graph, err := e.core.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{
		Member: member, VectorDigest: observed.Vector.VectorDigest, AuthorizeGeneration: input.AuthorizeGeneration,
		AuthorizePath: input.AuthorizePath, Scanner: input.Scanner, MaxManifestBytes: input.MaxManifestBytes,
		MaxFileBytes: min(input.MaxFileBytes, 64<<10), MaxSourceBytes: min(total, 2<<20), MaxFactBytes: packFactLimit, MaxFacts: 25000})
	if err != nil {
		return nil, err
	}
	steps, edges := int(e.limits.FlowMaxSteps), int(e.limits.FlowMaxEdges)
	if request.StepLimit != nil {
		steps = int(*request.StepLimit)
	}
	if request.EdgeLimit != nil {
		edges = int(*request.EdgeLimit)
	}
	if steps < 1 || edges < 1 || steps > min(100, int(e.limits.FlowMaxSteps)) ||
		edges > min(300, int(e.limits.FlowMaxEdges)) {
		return nil, app.ErrCoreInput
	}
	result, err := e.staticFlow(ctx, observed, graph, *root, flowview.Options{Steps: steps, Edges: edges, CallDepth: 2})
	if errors.Is(err, flowview.ErrUnverified) {
		return nil, app.ErrCoreUnsupported
	}
	if errors.Is(err, flowview.ErrLimit) {
		return nil, app.ErrCoreLimit
	}
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(result.Data.RootRef, request.RootRef) || result.Data.Mode != "static" ||
		len(result.Data.Steps) > steps || len(result.Data.Edges) > edges {
		return nil, app.ErrCoreBackend
	}
	if err := graph.VerifyCatalog(ctx, member); err != nil {
		return nil, err
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, observed.Binding.Policy.MaxOutputBytes, e.core.limits.MaxOutputBytes)
	response := baseResponse(options, observed.Vector)
	response.Coverage = model.Coverage{Status: "supported_scope_complete", Capabilities: []model.Capability{}, Gaps: []model.Gap{},
		ScopeDescription: "Bounded static flow from the captured Go profile."}
	if result.Partial {
		response.Status, response.Coverage.Status = "partial", "partial"
		response.Coverage.Gaps = append(response.Coverage.Gaps, model.Gap{Code: "FLOW_PROJECTION_PARTIAL",
			Reason: "Unevaluated branches, unsupported calls, or result bounds limit this static flow.", SourceRefs: []model.SourceRef{}})
	}
	return encodeFlowResponse(response, result)
}

func encodeFlowResponse(response model.Response, result flowview.Result) ([][]byte, error) {
	reason := "limit"
	data := result.Data
	fit := func(size int) ([][]byte, error) {
		page := response
		view := data
		view.Steps = data.Steps[:size]
		if size != len(data.Steps) {
			visible := make(map[string]bool, size)
			for _, step := range view.Steps {
				visible[step.StepID] = true
			}
			view.Edges = []model.FlowEdge{}
			for _, edge := range data.Edges {
				if visible[edge.FromStepID] && visible[edge.ToStepID] {
					view.Edges = append(view.Edges, edge)
				}
			}
		}
		page.Data = view
		page.Pagination.ReturnedCount = int64(len(view.Steps) + len(view.Edges))
		if size < len(data.Steps) || result.Truncated {
			remainingSteps := int64(len(data.Steps) - len(view.Steps))
			remainingEdges := int64(len(data.Edges) - len(view.Edges))
			page.Status, page.Coverage.Status = "partial", "partial"
			page.Pagination.EnumerationComplete, page.Pagination.Truncated = false, true
			page.Pagination.LimitReason = &reason
			page.Pagination.RequiredPending, page.Pagination.OptionalPending = &remainingSteps, &remainingEdges
			if result.Truncated {
				page.Pagination.RequiredPending, page.Pagination.OptionalPending = nil, nil
			}
		}
		return encodeResponse(page)
	}
	full, err := fit(len(data.Steps))
	if err == nil || !errors.Is(err, contextpack.ErrBudgetTooSmall) {
		return full, err
	}
	var best [][]byte
	for low, high := 1, len(data.Steps)-1; low <= high; {
		count := low + (high-low)/2
		page, err := fit(count)
		if err != nil {
			if !errors.Is(err, contextpack.ErrBudgetTooSmall) {
				return nil, err
			}
			high = count - 1
			continue
		}
		best, low = page, count+1
	}
	if best == nil {
		return nil, contextpack.ErrBudgetTooSmall
	}
	return best, nil
}
