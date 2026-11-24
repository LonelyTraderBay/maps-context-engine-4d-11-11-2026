package golang

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// SyntaxContainment reports lexical ownership from the captured AST, even
// when the analyzer has no verified type or build selection information.
func SyntaxContainment(ctx context.Context, syntax SyntaxResult) ([]model.RelationFact, error) {
	if len(syntax.Symbols) == 0 {
		return nil, ctx.Err() // No facts from a file that failed to parse.
	}
	file := syntax.Symbols[0]
	if file.Kind != "file" {
		return nil, ErrInputMismatch
	}
	owners := make(map[string]string, len(syntax.Scopes))
	for _, scope := range syntax.Scopes {
		owner := scope.OwnerSymbolID
		if owner == "" {
			owner = file.SymbolID
			if scope.ParentID != "" {
				owner = owners[scope.ParentID]
				if owner == "" {
					return nil, ErrInputMismatch
				}
			}
		}
		owners[scope.ID] = owner
	}
	relations := make([]model.RelationFact, 0, len(syntax.Symbols)-1)
	for _, symbol := range syntax.Symbols[1:] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		owner, target := owners[symbol.ScopeKey], symbol.SymbolID
		if owner == "" || owner == target || symbol.Range == nil {
			return nil, ErrInputMismatch
		}
		site := *symbol.Range
		id, err := graphRelationID(owner, &target, "CONTAINS", "syntax_exact", syntax.Path, site.StartByte, site.EndByte, "", nil)
		if err != nil {
			return nil, err
		}
		relations = append(relations, model.RelationFact{
			RelationID: id, SourceSymbolID: owner, TargetSymbolID: &target, Kind: "CONTAINS",
			EvidenceKind: "syntax_exact", SitePath: syntax.Path, Range: site,
		})
	}
	return relations, ctx.Err()
}
