package overview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

func graphProjectionFixture(t *testing.T, count int) (context.Context, contextpack.Input, contextpack.CursorBinding, *contextpack.CursorCodec) {
	t.Helper()
	ctx, in := overviewFixture(t, count)
	manifest := in.Snapshot.Manifest()
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	packages := []model.PackageFact{}
	for _, item := range in.Packages {
		packages = append(packages, item.Fact)
	}
	input := contextpack.Input{Scope: scope, Target: model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation}, SymbolID: "hub"}, Snapshot: in.Snapshot, Packages: packages, Symbols: in.Symbols, Relations: in.Relations, Coverage: model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}, Scanner: in.Scanner, AuthorizeScope: func(context.Context, model.ScopeVector, model.ScopeMember) error { return nil }, AuthorizePath: func(context.Context, model.ScopeMember, string) error { return nil }, VerifyFacts: func(context.Context, model.ScopeMember, string) error { return nil }, MaxManifestBytes: 65536, MaxFactBytes: 16 * 1024 * 1024, MaxSourceBytes: 1024 * 1024, MaxFacts: 10000, MaxNodes: 100, MaxEdges: 5000, MaxDepth: 2, MaxOutputBytes: 8 * 1024 * 1024}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "projection-v1", Scope: scope}
	var key [32]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, input, binding, codec
}

func TestProjectedGraphEnumeratesAndPagesPinnedFacts(t *testing.T) {
	ctx, input, binding, codec := graphProjectionFixture(t, 3001)
	allow := func(context.Context, contextpack.CursorBinding) error { return nil }
	graph, err := NewProjectedGraph(ctx, input, codec, binding, allow, 5000, 5000, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{}
	for _, rel := range input.Relations {
		expected[rel.RelationID] = true
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	var first string
	for {
		page, err := graph.Page(ctx, cursor, binding.PrincipalDigest, allow, 10, 100, 8192)
		if err != nil {
			t.Fatal(err)
		}
		if page.Coverage.Status != "partial" || len(page.Gaps) == 0 {
			t.Fatal("page lost coverage or gaps")
		}
		for _, edge := range page.Result.Data.Edges {
			if len(edge.EvidenceRefs) != 1 {
				t.Fatal("lost edge evidence")
			}
			id := edge.EvidenceRefs[0].EvidenceID
			if !expected[id] || seen[id] {
				t.Fatalf("lost identity or duplicate: %s", id)
			}
			seen[id] = true
		}
		pages++
		if page.Result.NextCursor == nil {
			break
		}
		cursor = *page.Result.NextCursor
		if first == "" {
			first = cursor
		}
		if pages > 5000 {
			t.Fatal("cursor did not terminate")
		}
	}
	if len(seen) != len(expected) || pages < 2 {
		t.Fatalf("lost graph facts: %d/%d", len(seen), len(expected))
	}
	denied := func(context.Context, contextpack.CursorBinding) error { return errors.New("revoked") }
	if page, err := graph.Page(ctx, first, binding.PrincipalDigest, denied, 10, 10, 8192); err == nil || len(page.Result.Data.Edges) != 0 {
		t.Fatal("revoked graph leaked")
	}
}

func TestProjectedGraphRejectsIncompleteAndKeepsUnresolvedBoundary(t *testing.T) {
	ctx, input, binding, codec := graphProjectionFixture(t, 4)
	allow := func(context.Context, contextpack.CursorBinding) error { return nil }
	limited := input
	limited.MaxEdges = 2
	if _, err := NewProjectedGraph(ctx, limited, codec, binding, allow, 100, 100, 1048576); !errors.Is(err, search.ErrGraphIncomplete) {
		t.Fatalf("incomplete traversal: %v", err)
	}
	rel := input.Relations[0]
	rel.RelationID = "unresolved-example"
	rel.Kind = "UNRESOLVED_CALL"
	rel.EvidenceKind = "unresolved"
	rel.TargetSymbolID = nil
	rel.Reason = "unknown"
	label := "dynamic"
	rel.UnresolvedLabel = &label
	input.Relations = append(input.Relations, rel)
	candidate := input.Relations[0]
	candidate.RelationID = "candidate-example"
	candidate.Kind = "MAY_DISPATCH_TO"
	candidate.EvidenceKind = "type_resolved"
	input.Relations = append(input.Relations, candidate)
	graph, err := NewProjectedGraph(ctx, input, codec, binding, allow, 100, 100, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	cursor := ""
	foundBoundary := false
	foundEdge := false
	foundCandidate := false
	for {
		page, err := graph.Page(ctx, cursor, binding.PrincipalDigest, allow, 20, 20, 65536)
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range page.Result.Data.Nodes {
			if node.Kind == "unresolved_boundary" {
				if node.Plane != "annotation" {
					t.Fatal("invented unresolved implementation")
				}
				if _, ok := node.Ref.(*model.FileRef); !ok || len(node.SourceRefs) != 1 {
					t.Fatal("boundary is not tied to its captured source")
				}
				foundBoundary = true
			}
		}
		for _, edge := range page.Result.Data.Edges {
			if len(edge.EvidenceRefs) == 1 && edge.EvidenceRefs[0].EvidenceID == "unresolved-example" {
				if edge.ResolutionStatus != "unresolved" {
					t.Fatal("unresolved became exact")
				}
				foundEdge = true
			}
			if len(edge.EvidenceRefs) == 1 && edge.EvidenceRefs[0].EvidenceID == "candidate-example" {
				if edge.ResolutionStatus != "candidate" {
					t.Fatal("possible dispatch became exact")
				}
				foundCandidate = true
			}
		}
		if page.Result.NextCursor == nil {
			break
		}
		cursor = *page.Result.NextCursor
	}
	if !foundBoundary || !foundEdge || !foundCandidate {
		t.Fatal("unresolved boundary was dropped")
	}
}

func TestProjectedGraphChecksPrincipalBeforeFactTraversal(t *testing.T) {
	ctx, input, binding, codec := graphProjectionFixture(t, 4)
	checks := 0
	input.AuthorizeScope = func(context.Context, model.ScopeVector, model.ScopeMember) error { checks++; return nil }
	denied := func(context.Context, contextpack.CursorBinding) error { return errors.New("denied") }
	if graph, err := NewProjectedGraph(ctx, input, codec, binding, denied, 100, 100, 1048576); err == nil || graph != nil || checks != 0 {
		t.Fatal("facts traversed before principal authorization")
	}
}

func TestProjectedGraphRechecksPinnedFactAuthorityOnPages(t *testing.T) {
	ctx, input, binding, codec := graphProjectionFixture(t, 4)
	allowed := true
	input.AuthorizeScope = func(context.Context, model.ScopeVector, model.ScopeMember) error {
		if !allowed {
			return errors.New("revoked scope")
		}
		return nil
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	graph, err := NewProjectedGraph(ctx, input, codec, binding, guard, 100, 100, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	page, err := graph.Page(ctx, "", binding.PrincipalDigest, guard, 10, 10, 8192)
	if err != nil || page.Result.NextCursor == nil {
		t.Fatal("missing cursor")
	}
	allowed = false
	if page, err = graph.Page(ctx, *page.Result.NextCursor, binding.PrincipalDigest, guard, 10, 10, 8192); err == nil || len(page.Result.Data.Edges) != 0 || len(page.Result.Data.Nodes) != 0 {
		t.Fatal("revoked fact scope leaked")
	}
}
