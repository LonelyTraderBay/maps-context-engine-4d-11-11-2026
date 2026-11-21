package compose

import (
	"context"
	"errors"
	"fmt"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

// ObservedRunAuthority supplies evidence from the local interaction and build
// owner. All participants must be in its current authorized runtime scope.
// This boundary never accepts a binding, destination, or receipt from HTTP.
type ObservedRunAuthority func(context.Context, app.CorePrincipal, ScopeObservation, model.FlowQueryRequest) (ObservedRunInput, error)

type ObservedRunInput struct {
	Request          flowview.ObservedRequest
	RelatedArtifacts []string
	Resolve          func(context.Context, traces.Ref) (flowview.ObservedBinding, bool, error)
	Verify           func(context.Context, flowview.ObservedBinding) error
}

func (e *ExplorerReader) readObservedFlow(ctx context.Context, principal app.CorePrincipal, request model.FlowQueryRequest,
	options app.CoreCallOptions) ([][]byte, func(context.Context) error, error) {
	if e.observedFlow == nil {
		return nil, nil, app.ErrCoreUnsupported
	}
	if request.Scope == nil || request.Scope.PinnedVector == nil || request.ArtifactID == nil ||
		!model.ValidDigest(*request.ArtifactID) || request.Cursor != nil {
		return nil, nil, app.ErrCoreInput
	}
	steps, edges := int(e.limits.FlowMaxSteps), int(e.limits.FlowMaxEdges)
	if request.StepLimit != nil {
		steps = int(*request.StepLimit)
	}
	if request.EdgeLimit != nil {
		edges = int(*request.EdgeLimit)
	}
	if steps < 1 || steps > min(100, int(e.limits.FlowMaxSteps)) ||
		edges < 1 || edges > min(300, int(e.limits.FlowMaxEdges)) {
		return nil, nil, app.ErrCoreInput
	}
	select {
	case e.projection <- struct{}{}:
		defer func() { <-e.projection }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	scope, err := e.core.resolveRequestScope(ctx, principal, request.Scope)
	if err != nil {
		return nil, nil, err
	}
	member, err := model.MatchPinnedMember(scope.Vector, request.RootRef, nil)
	if err != nil || len(scope.Vector.Members) != 1 {
		return nil, nil, ErrAuthority
	}
	input, err := e.observedFlow(ctx, principal, scope, request)
	if err != nil {
		return nil, nil, err
	}
	if input.Request.RequestSpan.ArtifactID != *request.ArtifactID || input.Verify == nil ||
		input.Resolve == nil || input.Request.VerifyRequest == nil || len(input.RelatedArtifacts) >= traces.MaxArtifacts {
		return nil, nil, ErrAuthority
	}
	if err := input.Request.VerifyRequest(ctx, input.Request); err != nil {
		return nil, nil, err
	}
	ids := append([]string{*request.ArtifactID}, input.RelatedArtifacts...)
	artifacts := make([]importtrace.Artifact, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !model.ValidDigest(id) || seen[id] {
			return nil, nil, ErrAuthority
		}
		seen[id] = true
		trace, state, err := e.core.Reader.Records.ReadTrace(ctx, id, func(ctx context.Context, identity records.Identity) error {
			if identity != scope.Records.Identity {
				return ErrAuthority
			}
			return e.core.Reader.Access(ctx, principal, member.WorktreeID, member.ProfileID)
		})
		if errors.Is(err, records.ErrTraceUnavailable) {
			return nil, nil, app.ErrCoreUnsupported
		}
		if err != nil {
			return nil, nil, err
		}
		if state.RecordEpoch != member.RecordEpoch {
			return nil, nil, ErrScopeChanged
		}
		artifacts = append(artifacts, trace)
	}
	checkScope := func(ctx context.Context, vector model.ScopeVector) error {
		_, err := e.core.Reader.Recheck(ctx, principal, vector)
		return err
	}
	result, err := flowview.ProjectObserved(ctx, artifacts, flowview.ObservedOptions{
		Scope: scope.Vector, Root: request.RootRef, Request: input.Request,
		Steps: steps, Edges: edges, Resolve: input.Resolve, RecheckScope: checkScope,
		Verify: func(ctx context.Context, binding flowview.ObservedBinding) error {
			if err := e.core.Authority.path(ctx, principal, scope.Binding.Policy, binding.Source.Path); err != nil {
				return err
			}
			return input.Verify(ctx, binding)
		},
	})
	if errors.Is(err, flowview.ErrObservedCorrelation) || errors.Is(err, flowview.ErrUnverified) {
		return nil, nil, ErrAuthority
	}
	if errors.Is(err, traces.ErrLimit) || errors.Is(err, flowview.ErrLimit) {
		return nil, nil, app.ErrCoreLimit
	}
	if err != nil {
		return nil, nil, err
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, e.core.limits.MaxOutputBytes, scope.Binding.Policy.MaxOutputBytes)
	response := baseResponse(options, scope.Vector)
	response.Status = "partial"
	response.Coverage = model.Coverage{Status: "partial", Capabilities: []model.Capability{},
		ScopeDescription: "One correlated observed run from admitted runtime trace metadata.",
		Gaps: []model.Gap{{Code: "OBSERVED_INSTRUMENTATION_PARTIAL",
			Reason:     fmt.Sprintf("Instrumentation bounds this run. %d unbound spans and %d uncorrelated spans were excluded.", result.UnboundSpans, result.UncorrelatedSpans),
			SourceRefs: []model.SourceRef{}}}}
	blocks, err := encodeFlowResponse(response, result.Result)
	if err != nil {
		return nil, nil, err
	}
	return blocks, func(ctx context.Context) error {
		if err := input.Request.VerifyRequest(ctx, input.Request); err != nil {
			return err
		}
		return checkScope(ctx, scope.Vector)
	}, nil
}
