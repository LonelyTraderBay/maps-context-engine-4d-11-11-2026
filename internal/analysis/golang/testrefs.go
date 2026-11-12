package golang

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// Avoid inventing bindings when imported names could shadow a declaration.
func syntaxTestImportsEligible(syntax SyntaxResult) bool {
	functions := make(map[string]bool)
	for _, symbol := range syntax.Symbols {
		if symbol.Kind == "function" {
			functions[symbol.Name] = true
		}
	}
	for _, imported := range syntax.Imports {
		if imported.Alias == "" || imported.Alias == "." {
			return false // imported package names or dot-import members are unknown
		}
		if imported.Alias == "_" {
			continue
		}
		if functions[imported.Alias] {
			return false
		}
	}
	return true
}

// syntaxTestReferences is deliberately narrower than Go test-variant type
// checking: it only links a function declaration and a syntactically resolved
// use in the same explicitly requested test file. It does not select a build
// variant, resolve imports or cross-file references, execute tests, or claim
// behavioral coverage. Parser object pointers never leave this function.
func syntaxTestReferences(ctx context.Context, preflight LoaderPreflightInput, syntax SyntaxResult, maxNodes, maxDepth, maxRelations int) ([]model.RelationFact, error) {
	if ctx == nil || preflight.Snapshot == nil || preflight.Profile == nil || !preflight.Profile.TestsEnabled || preflight.AuthorizeScope == nil || preflight.AuthorizePath == nil ||
		!strings.HasSuffix(syntax.Path, "_test.go") || syntax.Package.Variant != "internal_test" && syntax.Package.Variant != "external_test" || maxNodes < 1 || maxDepth < 1 || maxRelations < 1 {
		return nil, ErrLoaderInput
	}
	if len(syntax.Diagnostics) != 0 {
		return []model.RelationFact{}, nil
	}
	if !syntaxTestImportsEligible(syntax) {
		return []model.RelationFact{}, nil
	}
	manifest := preflight.Snapshot.Manifest()
	if err := preflight.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	if err := preflight.AuthorizePath(ctx, manifest.WorktreeID, syntax.Path, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	file, ok := preflight.Snapshot.File(syntax.Path)
	if !ok || file.ContentHash() != syntax.ContentHash || syntax.SnapshotDigest != manifest.SnapshotDigest {
		return nil, ErrInputMismatch
	}
	bytes := file.Bytes()
	if err := preflight.Scanner.CheckContent(bytes); err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, syntax.Path, bytes, parser.ParseComments)
	if err != nil || parsed == nil {
		return nil, ErrInputMismatch
	}
	tf := fset.File(parsed.Pos())
	if tf == nil || tf.Size() != file.Len() {
		return nil, ErrInputMismatch
	}
	sites := map[typedSite][]model.SymbolFact{}
	for _, symbol := range syntax.Symbols {
		if symbol.Path == nil || *symbol.Path != syntax.Path || symbol.Range == nil || file.CheckRange(int(symbol.Range.StartByte), int(symbol.Range.EndByte)) != nil {
			return nil, ErrInputMismatch
		}
		key := typedSite{path: syntax.Path, start: symbol.Range.StartByte, end: symbol.Range.EndByte, name: symbol.Name}
		sites[key] = append(sites[key], symbol)
	}
	declarations := map[*ast.Object]model.SymbolFact{}
	functionIDs := map[*ast.FuncDecl]string{}
	nodes := 0
	var walkErr error
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil || walkErr != nil {
			return walkErr == nil
		}
		nodes++
		if nodes > maxNodes {
			walkErr = ErrSyntaxLimit
			return false
		}
		if nodes%64 == 0 && ctx.Err() != nil {
			walkErr = ctx.Err()
			return false
		}
		decl, ok := node.(*ast.FuncDecl)
		if !ok || decl.Name == nil || decl.Name.Name == "_" {
			return true
		}
		site, err := graphIdentSite(syntax.Path, tf, file, decl.Name)
		if err != nil {
			walkErr = err
			return false
		}
		kind := "function"
		if decl.Recv != nil {
			kind = "method"
		}
		var match model.SymbolFact
		for _, symbol := range sites[site] {
			if symbol.Kind == kind {
				if match.SymbolID != "" {
					walkErr = ErrInputMismatch
					return false
				}
				match = symbol
			}
		}
		if match.SymbolID == "" {
			walkErr = ErrInputMismatch
			return false
		}
		functionIDs[decl] = match.SymbolID
		if decl.Recv == nil && decl.Name.Obj != nil && decl.Name.Obj.Kind == ast.Fun && decl.Name.Obj.Decl == decl {
			declarations[decl.Name.Obj] = match
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	frames := []string{}
	result := []model.RelationFact{}
	seen := map[string]bool{}
	nodes = 0
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil {
			if len(frames) > 0 {
				frames = frames[:len(frames)-1]
			}
			return true
		}
		if walkErr != nil {
			return false
		}
		nodes++
		if nodes > maxNodes || len(frames)+1 > maxDepth {
			walkErr = ErrSyntaxLimit
			return false
		}
		if nodes%64 == 0 && ctx.Err() != nil {
			walkErr = ctx.Err()
			return false
		}
		owner := ""
		if len(frames) > 0 {
			owner = frames[len(frames)-1]
		}
		if decl, ok := node.(*ast.FuncDecl); ok {
			owner = functionIDs[decl]
		}
		if ident, ok := node.(*ast.Ident); ok && owner != "" && ident.Obj != nil {
			if target, exists := declarations[ident.Obj]; exists {
				var site typedSite
				site, walkErr = graphIdentSite(syntax.Path, tf, file, ident)
				if walkErr != nil {
					return false
				}
				if target.Range.StartByte != site.start || target.Range.EndByte != site.end {
					var id string
					id, walkErr = graphRelationID(owner, &target.SymbolID, "TEST_REF", "syntax_exact", syntax.Path, site.start, site.end, "same_file_lexical_binding", nil)
					if walkErr != nil {
						return false
					}
					if !seen[id] {
						if len(result) >= maxRelations {
							walkErr = ErrSyntaxLimit
							return false
						}
						targetID := target.SymbolID
						item := model.RelationFact{RelationID: id, SourceSymbolID: owner, TargetSymbolID: &targetID, Kind: "TEST_REF", EvidenceKind: "syntax_exact", SitePath: syntax.Path, Range: model.ByteRange{StartByte: site.start, EndByte: site.end}, Reason: "same_file_lexical_binding"}
						raw, err := json.Marshal(item)
						if err != nil {
							walkErr = ErrInputMismatch
							return false
						}
						if _, err := model.DecodeRelationFactJSON(raw, 65536); err != nil {
							walkErr = ErrInputMismatch
							return false
						}
						seen[id] = true
						result = append(result, item)
					}
				}
			}
		}
		frames = append(frames, owner)
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Range.StartByte != result[j].Range.StartByte {
			return result[i].Range.StartByte < result[j].Range.StartByte
		}
		return result[i].RelationID < result[j].RelationID
	})
	if err := preflight.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	if err := preflight.AuthorizePath(ctx, manifest.WorktreeID, syntax.Path, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
