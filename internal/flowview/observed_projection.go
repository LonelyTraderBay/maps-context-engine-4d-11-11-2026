package flowview

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// ObservedBinding is resolved by the owner using approved build and
// instrumentation evidence. Operation names from the trace are never used
// as symbol identity or to infer SQL/transaction semantics.
type ObservedBinding struct {
	Span          traces.Ref
	Application   string
	Service       string
	Environment   string
	Revision      string
	BuildDigest   string
	ProfileID     string
	GraphRef      model.GraphRef
	Source        model.SourceRef
	Capture       *snapshot.Snapshot
	Role          string
	TransactionID string
}

type ObservedOptions struct {
	Scope        model.ScopeVector
	Root         model.GraphRef
	Request      ObservedRequest
	Steps, Edges int
	Resolve      func(context.Context, traces.Ref) (ObservedBinding, bool, error)
	Verify       func(context.Context, ObservedBinding) error
	RecheckScope func(context.Context, model.ScopeVector) error
}

type ObservedProjection struct {
	Result
	UnboundSpans      int
	UncorrelatedSpans int
}

type transactionKey struct {
	application, service, environment, revision, build, profile, trace, id string
}

type observedStep struct {
	node    traces.Node
	binding ObservedBinding
	step    model.FlowStep
}

// ProjectObserved uses the existing FlowData wire shape. Correlated spans with
// no verified source mapping count as unbound coverage. They cannot borrow a
// canonical graph reference from the selected static root.
func ProjectObserved(ctx context.Context, artifacts []importtrace.Artifact, opts ObservedOptions) (ObservedProjection, error) {
	if ctx == nil || opts.Resolve == nil || opts.Verify == nil || opts.RecheckScope == nil ||
		opts.Steps < 1 || opts.Steps > 100 || opts.Edges < 1 || opts.Edges > 300 ||
		model.VerifyScopeVectorDigest(opts.Scope, 32768) != nil {
		return ObservedProjection{}, ErrInput
	}
	if _, err := model.MatchPinnedMember(opts.Scope, opts.Root, nil); err != nil {
		return ObservedProjection{}, ErrInput
	}
	if err := opts.RecheckScope(ctx, opts.Scope); err != nil {
		return ObservedProjection{}, err
	}
	run, err := CorrelateObserved(ctx, opts.Request, artifacts)
	if err != nil {
		return ObservedProjection{}, err
	}
	out := ObservedProjection{UncorrelatedSpans: run.UncorrelatedSpans}
	data := model.FlowData{Operation: "flow_query", Mode: "observed", RootRef: opts.Root,
		Steps: []model.FlowStep{}, Edges: []model.FlowEdge{}}
	origins := make(map[string]importtrace.Metadata, len(run.Graph.Artifacts))
	for _, origin := range run.Graph.Artifacts {
		origins[origin.ArtifactID] = origin.Metadata
	}
	resolved := make([]observedStep, 0, opts.Steps)
	ids := make(map[traces.Ref]string)
	terminal := make(map[transactionKey][]observedStep)
	rootDigest, err := model.GraphRefDigest(opts.Root, 8192)
	if err != nil {
		return ObservedProjection{}, ErrInput
	}
	rootBound := false
	for _, node := range run.Graph.Nodes {
		if err := ctx.Err(); err != nil {
			return ObservedProjection{}, err
		}
		if len(resolved) == opts.Steps {
			out.Truncated = true
			break
		}
		binding, found, err := opts.Resolve(ctx, node.Ref)
		if err != nil {
			return ObservedProjection{}, err
		}
		if !found {
			out.UnboundSpans++
			continue
		}
		if !matchObservedBinding(ctx, opts.Scope, node.Ref, origins[node.Ref.ArtifactID], binding) {
			return ObservedProjection{}, ErrUnverified
		}
		if err := opts.Verify(ctx, binding); err != nil {
			return ObservedProjection{}, err
		}
		if node.Ref == run.RequestSpan {
			candidate, err := model.GraphRefDigest(binding.GraphRef, 8192)
			if err != nil || candidate != rootDigest {
				return ObservedProjection{}, ErrUnverified
			}
			rootBound = true
		}
		step := observedStep{node: node, binding: binding, step: makeObservedStep(node, binding)}
		resolved = append(resolved, step)
		ids[node.Ref] = step.step.StepID
		if binding.TransactionID != "" && (step.step.OutcomeStatus == "commit_observed" || step.step.OutcomeStatus == "rollback_observed") {
			key := observedTransaction(step)
			terminal[key] = append(terminal[key], step)
		}
	}
	if !rootBound || ids[run.RequestSpan] == "" {
		return ObservedProjection{}, ErrUnverified
	}
	appendEdge := func(kind, from, to string, evidence []model.EvidenceRef) {
		if len(data.Edges) == opts.Edges {
			out.Truncated = true
			return
		}
		data.Edges = append(data.Edges, model.FlowEdge{EdgeID: digest("observed-flow-edge-v1", kind, from, to),
			FromStepID: from, ToStepID: to, Kind: kind, EvidenceRefs: evidence})
	}
	for _, relation := range run.Graph.Relations {
		from, to := ids[relation.From], ids[relation.To]
		if from == "" || to == "" {
			continue
		}
		kind := "control"
		if relation.Kind == "observed_span_link" {
			kind = "async"
		}
		appendEdge(kind, from, to, []model.EvidenceRef{spanEvidence(relation.From), spanEvidence(relation.To)})
	}
	for _, row := range resolved {
		if row.binding.Role == "sql_statement" {
			ends := terminal[observedTransaction(row)]
			if row.binding.TransactionID == "" || len(ends) != 1 {
				row.step.Limitations = append(row.step.Limitations, observedOutcomeGap("OBSERVED_TRANSACTION_OUTCOME_UNKNOWN"))
			} else {
				end := ends[0]
				code := "OBSERVED_TRANSACTION_COMMIT"
				if end.step.OutcomeStatus == "rollback_observed" {
					code = "OBSERVED_TRANSACTION_ROLLBACK"
				}
				row.step.Limitations = append(row.step.Limitations, observedOutcomeGap(code))
				appendEdge("transaction_link", row.step.StepID, end.step.StepID,
					[]model.EvidenceRef{spanEvidence(row.node.Ref), spanEvidence(end.node.Ref)})
			}
		}
		data.Steps = append(data.Steps, row.step)
	}
	for _, row := range resolved {
		if err := opts.Verify(ctx, row.binding); err != nil {
			return ObservedProjection{}, err
		}
	}
	if err := opts.RecheckScope(ctx, opts.Scope); err != nil {
		return ObservedProjection{}, err
	}
	if err := opts.Request.VerifyRequest(ctx, opts.Request); err != nil {
		return ObservedProjection{}, err
	}
	if err := ctx.Err(); err != nil {
		return ObservedProjection{}, err
	}
	raw, err := json.Marshal(data)
	if err != nil || len(raw) > 1<<20 {
		return ObservedProjection{}, ErrLimit
	}
	data.ProjectionID = digest("observed-flow-projection-v1", opts.Scope.VectorDigest, opts.Request.InteractionID,
		opts.Request.RequestID, string(raw))
	out.Data = data
	out.Partial = true // An observed run asserts only the spans that were recorded.
	return out, nil
}

func matchObservedBinding(ctx context.Context, scope model.ScopeVector, ref traces.Ref, origin importtrace.Metadata, binding ObservedBinding) bool {
	if binding.Span != ref || binding.Capture == nil || binding.Application != origin.Application ||
		binding.Service != origin.Service || binding.Environment != origin.Environment ||
		origin.Revision == "" || binding.Revision != origin.Revision || !model.ValidDigest(binding.BuildDigest) ||
		binding.BuildDigest != origin.BuildDigest || binding.ProfileID != origin.ProfileID ||
		binding.Source.ProfileID != origin.ProfileID || (binding.TransactionID != "" && !model.ValidDigest(binding.TransactionID)) {
		return false
	}
	switch binding.Role {
	case "http_request", "http_handler", "runtime_span":
		if binding.TransactionID != "" {
			return false
		}
	case "sql_statement", "sql_commit", "sql_rollback":
	default:
		return false
	}
	var serviceID *string
	if scope.Kind != "single" {
		serviceID = &binding.Service
	}
	member, err := model.MatchPinnedMember(scope, binding.GraphRef, serviceID)
	if err != nil || member.WorktreeID != binding.Source.WorktreeID || member.ProfileID != binding.Source.ProfileID ||
		member.GenerationID != binding.Source.GenerationID {
		return false
	}
	return snapshot.ValidateCapturedSources(ctx, scope, []snapshot.ExpectedSource{{Ref: binding.Source, ServiceID: serviceID}},
		binding.Capture, 32768) == nil
}

func observedTransaction(row observedStep) transactionKey {
	b := row.binding
	return transactionKey{b.Application, b.Service, b.Environment, b.Revision, b.BuildDigest, b.ProfileID, row.node.Ref.TraceID, b.TransactionID}
}

func makeObservedStep(node traces.Node, binding ObservedBinding) model.FlowStep {
	status := "attempt_observed"
	if node.Status == "ok" {
		switch binding.Role {
		case "sql_statement":
			status = "statement_success_observed"
		case "sql_commit":
			if binding.TransactionID != "" {
				status = "commit_observed"
			}
		case "sql_rollback":
			if binding.TransactionID != "" {
				status = "rollback_observed"
			}
		}
	}
	return model.FlowStep{StepID: digest("observed-flow-step-v1", node.Ref.ArtifactID, node.Ref.TraceID, node.Ref.SpanID),
		CanonicalRefs: []model.GraphRef{binding.GraphRef}, SourceRefs: []model.SourceRef{binding.Source},
		StepKind: binding.Role, OrderSemantics: "causal_trace", PossibleOrObserved: "observed", OutcomeStatus: status,
		EvidenceRefs: []model.EvidenceRef{spanEvidence(node.Ref)}, Limitations: []model.Gap{}}
}

func spanEvidence(ref traces.Ref) model.EvidenceRef {
	return model.EvidenceRef{EvidenceID: "span-" + ref.TraceID + "-" + ref.SpanID, Origin: "runtime_observation",
		Kind: "runtime_observed", SourceRefs: []model.SourceRef{}, ArtifactID: &ref.ArtifactID}
}

func observedOutcomeGap(code string) model.Gap {
	reason := "The run lacks an unambiguous explicit transaction outcome."
	switch code {
	case "OBSERVED_TRANSACTION_COMMIT":
		reason = "A matching successful commit is recorded in this run."
	case "OBSERVED_TRANSACTION_ROLLBACK":
		reason = "A matching successful rollback is recorded in this run."
	}
	return model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{}}
}
