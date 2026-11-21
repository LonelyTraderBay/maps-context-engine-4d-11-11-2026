// Package traces projects approved span metadata into observed-only graphs.
package traces

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const (
	MaxArtifacts = 8
	MaxNodes     = 4096
	MaxRelations = 16384
)

var (
	ErrInput = errors.New("observed graph requires unique approved runtime trace artifacts")
	ErrLimit = errors.New("observed trace graph exceeds its resource limit")
)

type Ref struct {
	ArtifactID string `json:"artifact_id"`
	TraceID    string `json:"trace_id"`
	SpanID     string `json:"span_id"`
}

type ArtifactInfo struct {
	ArtifactID string               `json:"artifact_id"`
	Metadata   importtrace.Metadata `json:"metadata"`
}

type Node struct {
	Ref           Ref    `json:"ref"`
	Operation     string `json:"operation"`
	InteractionID string `json:"interaction_id"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at"`
	Status        string `json:"status"`
}

type Relation struct {
	Kind string `json:"kind"`
	From Ref    `json:"from"`
	To   Ref    `json:"to"`
}

type Gap struct {
	Code       string               `json:"code"`
	ArtifactID string               `json:"artifact_id"`
	At         *Ref                 `json:"at,omitempty"`
	Target     *importtrace.SpanKey `json:"target,omitempty"`
}

type Graph struct {
	Plane     string         `json:"plane"`
	Coverage  string         `json:"coverage"`
	Artifacts []ArtifactInfo `json:"artifacts"`
	Nodes     []Node         `json:"nodes"`
	Relations []Relation     `json:"relations"`
	Gaps      []Gap          `json:"gaps"`
}

type scopedKey struct {
	app, env string
	key      importtrace.SpanKey
}

type occurrence struct {
	ref      Ref
	app, env string
	span     importtrace.Span
}

// Project resolves only explicit relations within one application and
// environment namespace. Parent edges and cross-trace links remain distinct.
// Coverage remains an observed subset even when the collector labels sampling "all".
func Project(ctx context.Context, artifacts []importtrace.Artifact) (Graph, error) {
	if ctx == nil || len(artifacts) == 0 {
		return Graph{}, ErrInput
	}
	if len(artifacts) > MaxArtifacts {
		return Graph{}, ErrLimit
	}
	graph := Graph{Plane: "observed_runtime", Coverage: "observed_subset",
		Artifacts: []ArtifactInfo{}, Nodes: []Node{}, Relations: []Relation{}, Gaps: []Gap{}}
	seen := make(map[string]bool, len(artifacts))
	occurrences := make([]occurrence, 0)
	index := make(map[scopedKey][]Ref)
	relationBudget := 0
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return Graph{}, err
		}
		id := artifact.InputHash()
		spans := artifact.Spans()
		if !model.ValidDigest(id) || seen[id] || len(spans) == 0 {
			return Graph{}, ErrInput
		}
		if len(occurrences)+len(spans) > MaxNodes {
			return Graph{}, ErrLimit
		}
		seen[id] = true
		metadata := artifact.Metadata()
		graph.Artifacts = append(graph.Artifacts, ArtifactInfo{ArtifactID: id, Metadata: metadata})
		for _, span := range spans {
			relationBudget += len(span.Links)
			if span.ParentSpanID != "" {
				relationBudget++
			}
			if relationBudget > MaxRelations {
				return Graph{}, ErrLimit
			}
			ref := Ref{ArtifactID: id, TraceID: span.TraceID, SpanID: span.SpanID}
			occurrences = append(occurrences, occurrence{ref: ref, app: metadata.Application, env: metadata.Environment, span: span})
			key := scopedKey{app: metadata.Application, env: metadata.Environment, key: span.SpanKey}
			index[key] = append(index[key], ref)
		}
	}
	slices.SortFunc(graph.Artifacts, func(a, b ArtifactInfo) int { return cmp.Compare(a.ArtifactID, b.ArtifactID) })
	slices.SortFunc(occurrences, func(a, b occurrence) int { return compareRef(a.ref, b.ref) })
	for _, row := range graph.Artifacts {
		graph.Gaps = append(graph.Gaps, Gap{Code: "INSTRUMENTATION_COVERAGE_UNKNOWN", ArtifactID: row.ArtifactID})
		if row.Metadata.Sampling != "all" {
			graph.Gaps = append(graph.Gaps, Gap{Code: "SAMPLED_OR_UNSPECIFIED", ArtifactID: row.ArtifactID})
		}
	}
	parents := make(map[Ref]Ref)
	for _, row := range occurrences {
		if err := ctx.Err(); err != nil {
			return Graph{}, err
		}
		span := row.span
		graph.Nodes = append(graph.Nodes, Node{Ref: row.ref, Operation: span.Operation, InteractionID: span.InteractionID,
			StartedAt: span.StartedAt, EndedAt: span.EndedAt, Status: span.Status})
		if span.ParentSpanID == "" {
			continue
		}
		parent := importtrace.SpanKey{TraceID: span.TraceID, SpanID: span.ParentSpanID}
		if ref, ok := resolve(&graph, index, row, parent, "PARENT"); ok {
			parents[row.ref] = ref
		}
	}
	cycles := parentCycles(parents)
	for _, row := range occurrences {
		if parent, exists := parents[row.ref]; exists {
			if cycles[row.ref] {
				at := row.ref
				key := importtrace.SpanKey{TraceID: parent.TraceID, SpanID: parent.SpanID}
				graph.Gaps = append(graph.Gaps, Gap{Code: "PARENT_CYCLE", ArtifactID: at.ArtifactID, At: &at, Target: &key})
			} else {
				graph.Relations = append(graph.Relations, Relation{Kind: "observed_parent", From: parent, To: row.ref})
			}
		}
		slices.SortFunc(row.span.Links, func(a, b importtrace.SpanKey) int {
			if c := cmp.Compare(a.TraceID, b.TraceID); c != 0 {
				return c
			}
			return cmp.Compare(a.SpanID, b.SpanID)
		})
		for _, link := range row.span.Links {
			if ref, ok := resolve(&graph, index, row, link, "LINK"); ok {
				graph.Relations = append(graph.Relations, Relation{Kind: "observed_span_link", From: row.ref, To: ref})
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Graph{}, err
	}
	return graph, nil
}

func resolve(graph *Graph, index map[scopedKey][]Ref, row occurrence, target importtrace.SpanKey, relation string) (Ref, bool) {
	candidates := index[scopedKey{app: row.app, env: row.env, key: target}]
	if len(candidates) == 1 {
		return candidates[0], true
	}
	code := relation + "_NOT_CAPTURED"
	if len(candidates) > 1 {
		code = relation + "_AMBIGUOUS"
	}
	at := row.ref
	graph.Gaps = append(graph.Gaps, Gap{Code: code, ArtifactID: at.ArtifactID, At: &at, Target: &target})
	return Ref{}, false
}

func parentCycles(parents map[Ref]Ref) map[Ref]bool {
	visited := make(map[Ref]uint8, len(parents))
	cycles := make(map[Ref]bool)
	for ref := range parents {
		chain := make([]Ref, 0)
		for ref != (Ref{}) && visited[ref] == 0 {
			visited[ref] = 1
			chain = append(chain, ref)
			ref = parents[ref]
		}
		if visited[ref] == 1 {
			for i := slices.Index(chain, ref); i >= 0 && i < len(chain); i++ {
				cycles[chain[i]] = true
			}
		}
		for _, item := range chain {
			visited[item] = 2
		}
	}
	return cycles
}

func compareRef(a, b Ref) int {
	if c := cmp.Compare(a.ArtifactID, b.ArtifactID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.TraceID, b.TraceID); c != 0 {
		return c
	}
	return cmp.Compare(a.SpanID, b.SpanID)
}
