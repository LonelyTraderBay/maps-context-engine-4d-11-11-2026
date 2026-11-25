package index

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const RenderedJSXEvidenceBytes = 1800

// RoutedJSXSites identifies event-bearing elements of the route component
// and its directly rendered components in the selected neighborhood.
func RoutedJSXSites(root string, relations []model.RelationFact) []model.RelationFact {
	rendered := map[string]bool{root: true}
	routed := false
	for _, edge := range relations {
		if edge.TargetSymbolID == nil {
			continue
		}
		routed = routed || edge.Kind == "ROUTE_COMPONENT" && *edge.TargetSymbolID == root
		if edge.Kind == "RENDERS_COMPONENT" && edge.SourceSymbolID == root {
			rendered[*edge.TargetSymbolID] = true
		}
	}
	if !routed {
		return nil
	}
	var sites []model.RelationFact
	for _, edge := range relations {
		if edge.Kind == "RENDERS_JSX" && rendered[edge.SourceSymbolID] {
			sites = append(sites, edge)
		}
	}
	return sites
}

func readRouteJSXVariables(ctx context.Context, tx *sql.Tx, generation, root string, view PublishedNeighborhood,
	checkRange func(string, model.ByteRange) error, verifySymbol func(model.SymbolFact) error) (map[string][]JSXVariableRead, error) {
	sites := RoutedJSXSites(root, view.Relations)
	if len(sites) == 0 {
		return nil, nil
	}
	symbols := make(map[string]model.SymbolFact, len(view.Symbols))
	for _, symbol := range view.Symbols {
		symbols[symbol.SymbolID] = symbol
	}
	found := make(map[string][]JSXVariableRead)
	for _, site := range sites[:min(8, len(sites))] {
		if site.TargetSymbolID == nil {
			continue
		}
		element, ok := symbols[*site.TargetSymbolID]
		if !ok || element.Kind != "jsx_site" || element.Path == nil || *element.Path != site.SitePath || element.Range == nil {
			continue
		}
		file := view.Files[site.SitePath]
		end := min(site.Range.StartByte+RenderedJSXEvidenceBytes, int64(len(file.Bytes)))
		if element.Range.EndByte >= end {
			continue
		}
		reads, err := jsxVariableReads(ctx, tx, generation, site.SourceSymbolID, site.SitePath, element.Range.EndByte, end)
		if err != nil {
			return nil, err
		}
		for _, read := range reads {
			if err := checkRange(site.SitePath, read.use); err != nil {
				return nil, err
			}
			declaration, ok := symbols[read.target]
			if !ok {
				declaration, err = neighborhoodSymbol(ctx, tx, generation, read.target)
				if err != nil {
					return nil, err
				}
				if err := verifySymbol(declaration); err != nil {
					return nil, err
				}
				symbols[read.target] = declaration
			}
			found[site.RelationID] = append(found[site.RelationID], JSXVariableRead{Use: read.use, Declaration: declaration})
		}
	}
	return found, nil
}

type indexedJSXRead struct {
	use    model.ByteRange
	target string
}

func jsxVariableReads(ctx context.Context, tx *sql.Tx, generation, owner, path string, start, end int64) (_ []indexedJSXRead, resultErr error) {
	// Keep the lookup on the indexed owner and kind. Read only same-file
	// variable declarations small enough to include with the JSX.
	rows, err := tx.QueryContext(ctx, `SELECT r.start_byte,r.end_byte,r.target_symbol_id
		FROM relations r JOIN symbols s ON s.generation_id=r.generation_id AND s.symbol_id=r.target_symbol_id
		WHERE r.generation_id=? AND r.source_symbol_id=? AND r.kind='READS' AND r.evidence_kind='type_resolved'
		AND r.site_path=? AND r.start_byte>=? AND r.end_byte<=?
		AND s.path=r.site_path AND s.kind='variable' AND s.end_byte-s.start_byte BETWEEN 1 AND 640
		ORDER BY r.start_byte,r.end_byte,r.target_symbol_id LIMIT 16`, generation, owner, path, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	var found []indexedJSXRead
	seen := make(map[string]bool)
	for rows.Next() && len(found) < 4 {
		var read indexedJSXRead
		if err := rows.Scan(&read.use.StartByte, &read.use.EndByte, &read.target); err != nil {
			return nil, err
		}
		if !seen[read.target] {
			seen[read.target] = true
			found = append(found, read)
		}
	}
	return found, rows.Err()
}
