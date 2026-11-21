package traces

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
)

func occurrenceFixture(trace, span, parent, operation string) importtrace.Span {
	return importtrace.Span{SpanKey: importtrace.SpanKey{TraceID: strings.Repeat(trace, 32), SpanID: strings.Repeat(span, 16)},
		ParentSpanID: strings.Repeat(parent, 16), Operation: operation, StartedAt: "2026-10-01T01:00:03Z",
		EndedAt: "2026-10-01T01:00:04Z", Status: "ok", Links: []importtrace.SpanKey{}}
}

func traceArtifact(t *testing.T, id, service, environment, sampling string, spans ...importtrace.Span) importtrace.Artifact {
	t.Helper()
	meta := importtrace.Metadata{SchemaVersion: 1, Application: "sample", Service: service,
		Environment: environment, Collector: "local", Instrumentation: "v1", Sampling: sampling,
		WindowStart: "2026-10-01T01:00:00Z", WindowEnd: "2026-10-01T01:00:10Z", SampleRatePPM: 100_000}
	if sampling == "all" {
		meta.SampleRatePPM = 1_000_000
	}
	marshal := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	rows := make([]json.RawMessage, 0, len(spans))
	for _, span := range spans {
		rows = append(rows, marshal(span))
	}
	result, err := importtrace.DecodeStored(strings.Repeat(id, 64), marshal(meta), rows)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestC071SampledAsyncTraceKeepsRetriesAndMissingInstrumentation(t *testing.T) {
	origin := occurrenceFixture("a", "1", "", "order")
	retryA := occurrenceFixture("a", "2", "1", "checkout.retry")
	retryB := occurrenceFixture("a", "3", "1", "checkout.retry")
	retryB.Status = "error"
	consumer := occurrenceFixture("b", "4", "", "queue.consume")
	retryA.Links = []importtrace.SpanKey{consumer.SpanKey}
	missing := importtrace.SpanKey{TraceID: consumer.TraceID, SpanID: strings.Repeat("6", 16)}
	retryB.Links = []importtrace.SpanKey{missing}
	orphan := occurrenceFixture("b", "5", "6", "write")
	api := traceArtifact(t, "a", "api", "test", "head", retryB, retryA, origin)
	worker := traceArtifact(t, "b", "worker", "test", "all", consumer, orphan)
	foreign := traceArtifact(t, "c", "worker", "development", "all", occurrenceFixture("b", "6", "", "write"))

	graph, err := Project(t.Context(), []importtrace.Artifact{worker, api, foreign})
	if err != nil || graph.Plane != "observed_runtime" || graph.Coverage != "observed_subset" || len(graph.Nodes) != 6 {
		t.Fatalf("observed graph: %+v %v", graph, err)
	}
	var pairs []string
	for _, edge := range graph.Relations {
		pairs = append(pairs, edge.Kind+":"+edge.From.SpanID[:1]+"->"+edge.To.SpanID[:1])
	}
	if !reflect.DeepEqual(pairs, []string{"observed_parent:1->2", "observed_span_link:2->4", "observed_parent:1->3"}) {
		t.Fatalf("explicit observed relations: %v", pairs)
	}
	counts := map[string]int{}
	for _, gap := range graph.Gaps {
		counts[gap.Code]++
	}
	if !reflect.DeepEqual(counts, map[string]int{"SAMPLED_OR_UNSPECIFIED": 1, "INSTRUMENTATION_COVERAGE_UNKNOWN": 3,
		"PARENT_NOT_CAPTURED": 1, "LINK_NOT_CAPTURED": 1}) {
		t.Fatalf("gaps for sampled async evidence: %v", counts)
	}
	again, err := Project(t.Context(), []importtrace.Artifact{api, foreign, worker})
	if err != nil || !reflect.DeepEqual(graph, again) {
		t.Fatal("artifact input order changed the projection")
	}
}

func TestProjectionRefusesAmbiguousParentAndSuppressesCycle(t *testing.T) {
	first := occurrenceFixture("a", "1", "2", "first")
	second := occurrenceFixture("a", "2", "1", "second")
	cyclic := traceArtifact(t, "a", "service", "test", "all", first, second)
	graph, err := Project(t.Context(), []importtrace.Artifact{cyclic})
	if err != nil || len(graph.Relations) != 0 || len(graph.Gaps) != 3 ||
		graph.Gaps[1].Code != "PARENT_CYCLE" || graph.Gaps[2].Code != "PARENT_CYCLE" {
		t.Fatalf("cycle must be visible without causal parent edges: %+v %v", graph, err)
	}
	second.ParentSpanID = ""
	sibling := traceArtifact(t, "b", "another", "test", "all", second)
	graph, err = Project(t.Context(), []importtrace.Artifact{cyclic, sibling})
	if err != nil || len(graph.Relations) != 1 || graph.Gaps[2].Code != "PARENT_AMBIGUOUS" {
		t.Fatalf("ambiguous target was inferred: %+v %v", graph, err)
	}
	if _, err := Project(t.Context(), []importtrace.Artifact{cyclic, cyclic}); !errors.Is(err, ErrInput) {
		t.Fatalf("duplicate artifact: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Project(ctx, []importtrace.Artifact{cyclic}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled graph: %v", err)
	}
}
