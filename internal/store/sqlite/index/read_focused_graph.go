package index

import (
	"context"
	"database/sql"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const FocusedContextDepth = 2

var focusedContextKinds = []string{
	"ROUTE_COMPONENT", "RENDERS_COMPONENT", "RENDERS_JSX", "BINDS_EVENT",
	"CHECKS_CONDITION", "BRANCH_TRUE", "BRANCH_FALSE",
	"CALLS_DIRECT", "CALLS_INTERFACE", "MAY_DISPATCH_TO", "UNRESOLVED_CALL",
	"READS", "WRITES", "REFERS_TO", "TEST_REF", "DOC_LINK",
}

func readFocusedGraph(ctx context.Context, tx *sql.Tx, r PublishedGraphRequest, graph *PublishedGraph,
	manifest []model.ManifestFile) ([]model.ManifestFile, error) {
	generation := r.Member.GenerationID
	request := NeighborhoodRequest{Root: r.Focus.SymbolID, Direction: "both", Kinds: focusedContextKinds,
		Depth: FocusedContextDepth, MaxNodes: 28, MaxEdges: 48}
	ids, relations, _, err := walkNeighborhood(ctx, tx, generation, request, false)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool)
	for _, path := range r.Focus.RulePaths {
		want[path] = true
	}
	packages := make(map[string]bool)
	for _, id := range ids {
		symbol, err := neighborhoodSymbol(ctx, tx, generation, id)
		if err != nil {
			return nil, err
		}
		if symbol.Path != nil {
			want[*symbol.Path] = true
		}
		if symbol.PackageKey != nil && !packages[*symbol.PackageKey] {
			key := *symbol.PackageKey
			var p model.PackageFact
			if err := tx.QueryRowContext(ctx, `SELECT package_key,import_path,module_key,variant,input_digest,semantic_status
				FROM packages WHERE generation_id=? AND package_key=? AND
				length(CAST(import_path AS BLOB))+length(CAST(module_key AS BLOB))+length(CAST(variant AS BLOB))+
				length(CAST(input_digest AS BLOB))+length(CAST(semantic_status AS BLOB))<=262144`, generation, key).
				Scan(&p.PackageKey, &p.ImportPath, &p.ModuleKey, &p.Variant, &p.InputDigest, &p.SemanticStatus); err != nil {
				return nil, err
			}
			packages[key] = true
			graph.Packages = append(graph.Packages, p)
		}
		graph.Symbols = append(graph.Symbols, symbol)
	}
	for _, relation := range relations {
		if !validRelationEvidence(relation) {
			return nil, ErrPublishIntegrity
		}
		want[relation.SitePath] = true
		graph.Relations = append(graph.Relations, relation)
	}
	graph.Coverage, _, err = readPublishedCoverage(ctx, tx, generation, graph.state, r.Scanner, r.MaxFactBytes)
	if err != nil {
		return nil, err
	}
	for _, gap := range graph.Coverage.Gaps {
		for _, ref := range gap.SourceRefs {
			want[ref.Path] = true
		}
	}
	entries := make([]model.ManifestFile, 0, len(want))
	for _, entry := range manifest {
		if want[entry.Path] && entry.Status == "captured" {
			entries = append(entries, entry)
		}
	}
	if len(graph.Symbols)+len(graph.Relations)+len(graph.Packages) > r.MaxFacts {
		return nil, ErrCaptureLimit
	}
	sort.Slice(graph.Packages, func(i, j int) bool { return graph.Packages[i].PackageKey < graph.Packages[j].PackageKey })
	sort.Slice(graph.Symbols, func(i, j int) bool { return graph.Symbols[i].SymbolID < graph.Symbols[j].SymbolID })
	sort.Slice(graph.Relations, func(i, j int) bool { return graph.Relations[i].RelationID < graph.Relations[j].RelationID })
	return entries, nil
}
