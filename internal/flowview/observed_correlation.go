package flowview

import (
	"context"
	"errors"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrObservedCorrelation = errors.New("observed request correlation is unverified")

// ObservedRequest names the one outbound request selected by the interaction
// owner. VerifyRequest authenticates that evidence against the current session
// and permission. These fields alone do not authorize trace or source reads.
type ObservedRequest struct {
	InteractionID string
	RequestID     string
	TraceParent   string
	RequestSpan   traces.Ref
	BuildDigest   string
	VerifyRequest func(context.Context, ObservedRequest) error
}

type ObservedRun struct {
	RequestSpan       traces.Ref
	Graph             traces.Graph
	UncorrelatedSpans int
}

// CorrelateObserved starts from one exact request span. An admitted parent
// relation may carry its correlation to a child. A link is exposed only when
// both endpoints are already in the run. Timestamps do not select any span.
func CorrelateObserved(ctx context.Context, request ObservedRequest, artifacts []importtrace.Artifact) (ObservedRun, error) {
	if ctx == nil || request.VerifyRequest == nil || !model.ValidUUID(request.InteractionID) ||
		!model.ValidUUID(request.RequestID) || !model.ValidDigest(request.BuildDigest) ||
		!model.ValidDigest(request.RequestSpan.ArtifactID) || !matchRequestTraceParent(request) {
		return ObservedRun{}, ErrObservedCorrelation
	}
	if err := request.VerifyRequest(ctx, request); err != nil {
		return ObservedRun{}, err
	}
	graph, err := traces.Project(ctx, artifacts)
	if err != nil {
		return ObservedRun{}, err
	}
	nodes := make(map[traces.Ref]traces.Node, len(graph.Nodes))
	deployments := make(map[string]importtrace.Metadata, len(graph.Artifacts))
	for _, node := range graph.Nodes {
		nodes[node.Ref] = node
	}
	for _, artifact := range graph.Artifacts {
		deployments[artifact.ArtifactID] = artifact.Metadata
	}
	seed, exists := nodes[request.RequestSpan]
	deployment := deployments[request.RequestSpan.ArtifactID]
	if !exists || seed.InteractionID != request.InteractionID || deployment.BuildDigest != request.BuildDigest ||
		deployment.Revision == "" || deployment.ProfileID == "" {
		return ObservedRun{}, ErrObservedCorrelation
	}
	children := make(map[traces.Ref][]traces.Ref)
	for _, relation := range graph.Relations {
		if relation.Kind == "observed_parent" {
			children[relation.From] = append(children[relation.From], relation.To)
		}
	}
	selected := map[traces.Ref]bool{seed.Ref: true}
	queue := []traces.Ref{seed.Ref}
	for i := 0; i < len(queue); i++ {
		if err := ctx.Err(); err != nil {
			return ObservedRun{}, err
		}
		for _, child := range children[queue[i]] {
			interaction := nodes[child].InteractionID
			if selected[child] || (interaction != "" && interaction != request.InteractionID) {
				continue
			}
			selected[child] = true
			queue = append(queue, child)
		}
	}
	out := ObservedRun{RequestSpan: seed.Ref, Graph: traces.Graph{
		Plane: graph.Plane, Coverage: graph.Coverage, Artifacts: []traces.ArtifactInfo{},
		Nodes: []traces.Node{}, Relations: []traces.Relation{}, Gaps: []traces.Gap{},
	}}
	included := make(map[string]bool)
	for _, ref := range queue {
		out.Graph.Nodes = append(out.Graph.Nodes, nodes[ref])
		included[ref.ArtifactID] = true
	}
	for _, node := range graph.Nodes {
		if observed := deployments[node.Ref.ArtifactID]; !selected[node.Ref] && observed.Environment == deployment.Environment &&
			observed.Application == deployment.Application {
			out.UncorrelatedSpans++
		}
	}
	for _, artifact := range graph.Artifacts {
		if included[artifact.ArtifactID] {
			out.Graph.Artifacts = append(out.Graph.Artifacts, artifact)
		}
	}
	for _, edge := range graph.Relations {
		if selected[edge.From] && selected[edge.To] {
			out.Graph.Relations = append(out.Graph.Relations, edge)
		}
	}
	for _, gap := range graph.Gaps {
		if included[gap.ArtifactID] && (gap.At == nil || selected[*gap.At]) {
			out.Graph.Gaps = append(out.Graph.Gaps, gap)
		}
	}
	if err := request.VerifyRequest(ctx, request); err != nil {
		return ObservedRun{}, err
	}
	if err := ctx.Err(); err != nil {
		return ObservedRun{}, err
	}
	return out, nil
}

func matchRequestTraceParent(request ObservedRequest) bool {
	header := request.TraceParent
	if len(header) != 55 {
		return false
	}
	fields := strings.Split(header, "-")
	if len(fields) != 4 || fields[0] != "00" || len(fields[1]) != 32 || len(fields[2]) != 16 || len(fields[3]) != 2 ||
		fields[1] != request.RequestSpan.TraceID || fields[2] != request.RequestSpan.SpanID {
		return false
	}
	for _, field := range fields[1:] {
		for _, c := range field {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return false
			}
		}
	}
	return fields[1] != strings.Repeat("0", 32) && fields[2] != strings.Repeat("0", 16)
}
