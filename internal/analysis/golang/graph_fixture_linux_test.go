//go:build linux

package golang

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// The expected sites below are derived directly from source text, not from
// the graph builder. These callbacks are synthetic authority proofs.
func runGraphDispatchFixture(t *testing.T, ctx context.Context, typed TypedInput, source string, candidates []string) {
	t.Helper()
	typed.MaxOutputBytes = 256 * 1024
	input := GraphInput{Typed: typed, MaxRelations: 4000, MaxOutputBytes: 256 * 1024}
	result, err := AnalyzeCapturedGraph(ctx, input)
	if err != nil {
		t.Fatalf("graph fixture: %v", err)
	}
	if result.Status != "partial" || !containsReason(result.Reasons, "GRAPH_COVERAGE_PARTIAL") || !containsReason(result.Reasons, "INTERFACE_CANDIDATES_SCOPED") || !containsReason(result.Reasons, "POINTER_ALIAS_BOUNDARY") {
		t.Fatalf("graph boundaries missing: %+v", result.Reasons)
	}
	symbolAt := func(kind, fragment, prefix string) string {
		t.Helper()
		pos := strings.Index(source, fragment)
		if pos < 0 {
			t.Fatalf("fixture fragment missing: %s", fragment)
		}
		pos += len(prefix)
		for _, syntax := range result.Typed.Syntax {
			for _, symbol := range syntax.Symbols {
				if symbol.Kind == kind && symbol.Range != nil && symbol.Range.StartByte == int64(pos) {
					return symbol.SymbolID
				}
			}
		}
		t.Fatalf("symbol missing: %s at %s", kind, fragment)
		return ""
	}
	use := symbolAt("function", "func Use(", "func ")
	interfaceMethod := symbolAt("method", "interface { Run(", "interface { ")
	concrete := symbolAt("method", "func (A) Run(", "func (A) ")
	direct := symbolAt("function", "func Direct(", "func ")
	x := symbolAt("variable", "x := r.Run", "")
	ptr := symbolAt("variable", "ptr := &x", "")
	state := symbolAt("variable", "s := State{}", "")
	field := symbolAt("field", "Count int", "")
	closure := symbolAt("closure", "immediate := func(", "immediate := ")
	find := func(kind, fragment, prefix, from, target string) []model.RelationFact {
		t.Helper()
		pos := strings.Index(source, fragment)
		if pos < 0 {
			t.Fatalf("fixture fragment missing: %s", fragment)
		}
		pos += len(prefix)
		var found []model.RelationFact
		for _, relation := range result.Relations {
			if relation.Kind != kind || relation.SitePath != "dispatch.go" || relation.Range.StartByte != int64(pos) || from != "" && relation.SourceSymbolID != from {
				continue
			}
			if target != "" && (relation.TargetSymbolID == nil || *relation.TargetSymbolID != target) {
				continue
			}
			found = append(found, relation)
		}
		return found
	}
	require := func(kind, fragment, prefix, from, target string) model.RelationFact {
		t.Helper()
		found := find(kind, fragment, prefix, from, target)
		if len(found) != 1 {
			t.Fatalf("%s at %s: got %d", kind, fragment, len(found))
		}
		return found[0]
	}
	for _, fragment := range []string{"r.Run(1)", "Runner.Run(r, 4)"} {
		require("CALLS_INTERFACE", fragment, "", use, interfaceMethod)
		for _, candidate := range candidates {
			relation := require("MAY_DISPATCH_TO", fragment, "", interfaceMethod, candidate)
			if relation.EvidenceKind != "type_possible" {
				t.Fatal("interface candidate became a proven runtime call")
			}
		}
		if len(find("CALLS_DIRECT", fragment, "", use, "")) != 0 {
			t.Fatal("interface call was labeled direct")
		}
	}
	require("CALLS_DIRECT", "A{}.Run(2)", "", use, concrete)
	first := require("CALLS_DIRECT", "Direct(5)", "", use, direct)
	second := require("CALLS_DIRECT", "Direct(6)", "", use, direct)
	if first.RelationID == second.RelationID {
		t.Fatal("distinct call sites collapsed")
	}
	for _, fragment := range []string{"Direct(9)", "Direct(10)"} {
		relation := require("UNRESOLVED_CALL", fragment, "", "", "")
		if relation.Reason != "non_callable_owner" || len(find("CALLS_DIRECT", fragment, "", "", direct)) != 0 {
			t.Fatal("call without a captured callable owner was reported as direct")
		}
	}
	require("CALLS_DIRECT", "immediate := func(v int) int { return v }(8)", "immediate := ", use, closure)
	for _, fragment := range []string{"fn(3)", "closure(7)"} {
		relation := require("UNRESOLVED_CALL", fragment, "", use, "")
		if relation.TargetSymbolID != nil || relation.Reason != "function_value" {
			t.Fatal("function-value call was guessed")
		}
	}
	for _, kind := range []string{"CALLS_DIRECT", "CALLS_INTERFACE", "UNRESOLVED_CALL"} {
		if len(find(kind, "int(x)", "", use, "")) != 0 {
			t.Fatal("type conversion became a call")
		}
	}
	require("READS", "x += q", "", use, x)
	require("WRITES", "x += q", "", use, x)
	require("REFERS_TO", "ptr := &x", "ptr := &", use, x)
	require("REFERS_TO", "*ptr = q", "*", use, ptr)
	require("WRITES", "s.Count = q", "s.", use, field)
	require("REFERS_TO", "s.Count = q", "", use, state)
	require("REFERS_TO", "addr := &s.Count", "addr := &s.", use, field)
	require("REFERS_TO", "addr := &s.Count", "addr := &", use, state)
	for _, kind := range []string{"READS", "WRITES"} {
		if len(find(kind, "ptr := &x", "ptr := &", use, x)) != 0 || len(find(kind, "*ptr = q", "*", use, ptr)) != 0 {
			t.Fatal("pointer alias inferred an access")
		}
		if len(find(kind, "s.Count = q", "", use, state)) != 0 || len(find(kind, "addr := &s.Count", "addr := &", use, state)) != 0 || len(find(kind, "addr := &s.Count", "addr := &s.", use, field)) != 0 {
			t.Fatal("field address or lvalue inferred an access to its owner")
		}
	}
	other, err := AnalyzeCapturedGraph(ctx, input)
	if err != nil || !reflect.DeepEqual(result.Relations, other.Relations) {
		t.Fatalf("graph order or identities are unstable: %v", err)
	}
	limited := input
	limited.MaxRelations = 1
	if got, err := AnalyzeCapturedGraph(ctx, limited); !errors.Is(err, ErrSyntaxLimit) || len(got.Relations) != 0 {
		t.Fatalf("relation limit returned partial facts: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := AnalyzeCapturedGraph(canceled, input); !errors.Is(err, context.Canceled) || len(got.Relations) != 0 {
		t.Fatalf("canceled graph returned facts: %v", err)
	}
	checks := 0
	revoked := input
	revoked.Typed.Loader.VerifyScratch = func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
		checks++
		return checks < 4, nil
	}
	if got, err := AnalyzeCapturedGraph(ctx, revoked); !errors.Is(err, ErrLoaderAuthority) || len(got.Relations) != 0 || checks != 4 {
		t.Fatalf("revocation after graph returned facts: checks=%d err=%v", checks, err)
	}
}

func runGraphTypedFixture(t *testing.T, ctx context.Context, typed TypedInput, source, otherSource, methodID, numericID string) {
	t.Helper()
	result, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: typed, MaxRelations: 6000, MaxOutputBytes: 2 * 1024 * 1024})
	if err != nil {
		t.Fatalf("UTF-8, CRLF and generic graph fixture: %v", err)
	}
	read, write, crossFileCall := false, false, false
	for _, relation := range result.Relations {
		if relation.TargetSymbolID == nil {
			continue
		}
		read = read || relation.Kind == "READS" && relation.SitePath == "typed.go" && relation.Range.StartByte == int64(strings.Index(source, "n += 1")) && *relation.TargetSymbolID == numericID
		write = write || relation.Kind == "WRITES" && relation.SitePath == "typed.go" && relation.Range.StartByte == int64(strings.Index(source, "n += 1")) && *relation.TargetSymbolID == numericID
		crossFileCall = crossFileCall || relation.Kind == "CALLS_DIRECT" && relation.SitePath == "other.go" && relation.Range.StartByte == int64(strings.Index(otherSource, "other.Method(1)")) && *relation.TargetSymbolID == methodID
	}
	if !read || !write || !crossFileCall {
		t.Fatalf("typed graph lost CRLF site or generic cross-file call: read=%v write=%v call=%v", read, write, crossFileCall)
	}
}
