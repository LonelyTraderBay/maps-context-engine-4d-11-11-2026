package flowview

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
)

func TestObservedCorrelationKeepsConcurrentRequestsSeparate(t *testing.T) {
	const clickA = "11111111-1111-4111-8111-111111111111"
	const clickB = "22222222-2222-4222-8222-222222222222"
	traceID := strings.Repeat("a", 32)
	spanID := func(digit string) string { return strings.Repeat(digit, 16) }
	span := func(id, parent, interaction string) importtrace.Span {
		return importtrace.Span{
			SpanKey:      importtrace.SpanKey{TraceID: traceID, SpanID: spanID(id)},
			ParentSpanID: parent, InteractionID: interaction, Operation: "sample",
			StartedAt: "2026-10-01T01:00:03Z", EndedAt: "2026-10-01T01:00:04Z", Status: "ok",
			Links: []importtrace.SpanKey{},
		}
	}
	root := span("1", "", clickA)
	first := span("2", spanID("1"), "")
	retry := span("3", spanID("1"), "")
	concurrent := span("4", "", clickB)
	conflictingChild := span("5", spanID("1"), clickB)
	background := span("6", "", "")
	uncorrelated := span("7", "", clickA)
	first.Links = []importtrace.SpanKey{background.SpanKey}
	build := strings.Repeat("b", 64)
	meta := importtrace.Metadata{
		SchemaVersion: 1, Application: "stock", Service: "frontend", Environment: "test",
		Collector: "local", Instrumentation: "v1", WindowStart: "2026-10-01T01:00:00Z",
		WindowEnd: "2026-10-01T01:00:10Z", SampleRatePPM: 1000000, Sampling: "all",
		Revision: strings.Repeat("c", 40), BuildDigest: build, ProfileID: strings.Repeat("d", 64),
	}
	marshal := func(value any) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	id := strings.Repeat("e", 64)
	artifact, err := importtrace.DecodeStored(id, marshal(meta), []json.RawMessage{
		marshal(concurrent), marshal(root), marshal(first), marshal(retry), marshal(conflictingChild),
		marshal(background), marshal(uncorrelated),
	})
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	req := ObservedRequest{
		InteractionID: clickA, RequestID: "33333333-3333-4333-8333-333333333333",
		TraceParent: "00-" + traceID + "-" + spanID("1") + "-01", BuildDigest: build,
		RequestSpan:   traces.Ref{ArtifactID: id, TraceID: traceID, SpanID: spanID("1")},
		VerifyRequest: func(ctx context.Context, _ ObservedRequest) error { checks++; return ctx.Err() },
	}
	run, err := CorrelateObserved(t.Context(), req, []importtrace.Artifact{artifact})
	if err != nil || checks != 2 || run.UncorrelatedSpans != 4 || len(run.Graph.Nodes) != 3 || len(run.Graph.Relations) != 2 {
		t.Fatalf("explicit correlation crossed requests: %+v checks=%d %v", run, checks, err)
	}
	for i, node := range run.Graph.Nodes {
		if node.Ref.SpanID != spanID(string(rune('1'+i))) {
			t.Fatalf("wrong correlated occurrence: %s", node.Ref.SpanID)
		}
	}
	for _, edge := range run.Graph.Relations {
		if edge.Kind != "observed_parent" || edge.From != req.RequestSpan {
			t.Fatalf("timestamps, labels or unbound links changed the observed run: %+v", edge)
		}
	}
	wrongBuild := req
	wrongBuild.BuildDigest = strings.Repeat("f", 64)
	if _, err := CorrelateObserved(t.Context(), wrongBuild, []importtrace.Artifact{artifact}); !errors.Is(err, ErrObservedCorrelation) {
		t.Fatalf("wrong build: %v", err)
	}
	wrongHeader := req
	wrongHeader.TraceParent = "00-" + traceID + "-" + spanID("4") + "-01"
	if _, err := CorrelateObserved(t.Context(), wrongHeader, []importtrace.Artifact{artifact}); !errors.Is(err, ErrObservedCorrelation) {
		t.Fatalf("wrong traceparent: %v", err)
	}
	revoke := errors.New("revoked fixture request")
	req.VerifyRequest = func(context.Context, ObservedRequest) error {
		checks++
		if checks%2 == 0 {
			return revoke
		}
		return nil
	}
	checks = 0
	if result, err := CorrelateObserved(t.Context(), req, []importtrace.Artifact{artifact}); !errors.Is(err, revoke) || len(result.Graph.Nodes) != 0 {
		t.Fatalf("revoked request emitted observations: %+v %v", result, err)
	}
}
