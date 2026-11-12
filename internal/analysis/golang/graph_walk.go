package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const (
	graphRead uint8 = 1 << iota
	graphWrite
)

type graphFrame struct {
	node  ast.Node
	owner string
}

// walkFile parses only the immutable capture. A binding or call is accepted
// only when the typed load recorded the same identifier or expression range.
func (c *graphCollector) walkFile(path string) error {
	file := c.files[path]
	manifest := c.input.Typed.Loader.Preflight.Snapshot.Manifest()
	preflight := c.input.Typed.Loader.Preflight
	if err := preflight.AuthorizeScope(c.ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
		return loaderAuthorityError(c.ctx)
	}
	if err := preflight.AuthorizePath(c.ctx, manifest.WorktreeID, path, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
		return loaderAuthorityError(c.ctx)
	}
	source := file.Bytes()
	if err := preflight.Scanner.CheckContent(source); err != nil {
		return err
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, source, parser.AllErrors|parser.ParseComments)
	if err != nil || parsed == nil {
		return ErrInputMismatch
	}
	tf := fset.File(parsed.Pos())
	if tf == nil || tf.Size() != file.Len() {
		return ErrInputMismatch
	}
	frames := []graphFrame{}
	nodes := 0
	var walkErr error
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
		if nodes > c.input.Typed.MaxNodes || len(frames)+1 > c.input.Typed.MaxDepth {
			walkErr = ErrSyntaxLimit
			return false
		}
		if nodes%64 == 0 && c.ctx.Err() != nil {
			walkErr = c.ctx.Err()
			return false
		}
		owner := c.fileSymbols[path]
		var parent ast.Node
		if len(frames) != 0 {
			owner, parent = frames[len(frames)-1].owner, frames[len(frames)-1].node
		}
		var scopeKind string
		switch n := node.(type) {
		case *ast.FuncDecl:
			kind := "function"
			if n.Recv != nil {
				kind = "method"
			}
			if n.Name != nil && n.Name.Name == "_" {
				c.reasons["SCOPE_OWNER_UNMAPPED"] = true
			} else {
				owner, walkErr = c.graphIdentSymbol(path, tf, file, n.Name, kind)
			}
			scopeKind = "function"
		case *ast.FuncLit:
			owner, walkErr = c.graphClosureSymbol(path, tf, file, n)
			scopeKind = "closure"
		case *ast.TypeSpec:
			kind := "type"
			if _, ok := n.Type.(*ast.InterfaceType); ok {
				kind = "interface"
			}
			if n.Name != nil && n.Name.Name == "_" {
				c.reasons["SCOPE_OWNER_UNMAPPED"] = true
			} else {
				owner, walkErr = c.graphIdentSymbol(path, tf, file, n.Name, kind)
			}
			scopeKind = "type"
		case *ast.FuncType:
			if field, ok := parent.(*ast.Field); ok && len(field.Names) == 1 {
				site, siteErr := graphIdentSite(path, tf, file, field.Names[0])
				if siteErr != nil {
					walkErr = siteErr
				} else if id := c.symbolAt(site, "method"); id != "" {
					owner, scopeKind = id, "function_type"
				}
			}
		}
		if walkErr == nil && scopeKind != "" {
			var key typedRange
			key, walkErr = graphNodeRange(path, tf, file, node)
			if walkErr == nil {
				scope := c.scopeRanges[graphScopeRange{typedRange: key, kind: scopeKind}]
				if scope == "" || c.scopeOwners[scope] != "" && c.scopeOwners[scope] != owner {
					walkErr = ErrInputMismatch
				} else {
					c.scopeOwners[scope] = owner
				}
			}
		}
		if walkErr != nil {
			return false
		}
		switch n := node.(type) {
		case *ast.Ident:
			var site typedSite
			site, walkErr = graphIdentSite(path, tf, file, n)
			if walkErr == nil {
				if _, found := c.uses[site]; found {
					if c.useOwners[site] != "" {
						walkErr = ErrInputMismatch
					} else {
						c.useOwners[site] = owner
					}
				}
			}
		case *ast.CallExpr:
			var site typedRange
			site, walkErr = graphNodeRange(path, tf, file, n.Fun)
			if walkErr == nil {
				if _, found := c.calls[site]; !found || c.callOwners[site].owner != "" {
					walkErr = ErrInputMismatch
				} else {
					call := graphCall{owner: owner}
					var expr ast.Expr = n.Fun
					for {
						switch value := expr.(type) {
						case *ast.ParenExpr:
							expr = value.X
						case *ast.IndexExpr:
							expr = value.X
						case *ast.IndexListExpr:
							expr = value.X
						default:
							goto unwrapped
						}
					}
				unwrapped:
					switch value := expr.(type) {
					case *ast.Ident:
						var callee typedSite
						callee, walkErr = graphIdentSite(path, tf, file, value)
						call.callee = &callee
					case *ast.SelectorExpr:
						var callee typedSite
						callee, walkErr = graphIdentSite(path, tf, file, value.Sel)
						call.callee = &callee
					case *ast.FuncLit:
						call.literalTarget, walkErr = c.graphClosureSymbol(path, tf, file, value)
					}
					c.callOwners[site] = call
				}
			}
		case *ast.AssignStmt:
			flags := graphWrite
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
				flags |= graphRead
			}
			for _, lhs := range n.Lhs {
				if err := c.markAccess(path, tf, file, lhs, flags, false); err != nil {
					walkErr = err
					break
				}
			}
		case *ast.IncDecStmt:
			walkErr = c.markAccess(path, tf, file, n.X, graphRead|graphWrite, false)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				for _, lhs := range []ast.Expr{n.Key, n.Value} {
					if err := c.markAccess(path, tf, file, lhs, graphWrite, false); err != nil {
						walkErr = err
						break
					}
				}
			}
		case *ast.StarExpr:
			walkErr = c.markAccess(path, tf, file, n.X, 0, true)
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				walkErr = c.markAccess(path, tf, file, n.X, 0, true)
			}
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok {
				var site typedSite
				site, walkErr = graphIdentSite(path, tf, file, id)
				if walkErr == nil && c.uses[site].ObjectKind == "field" {
					delete(c.access, site)
				}
			}
		}
		if walkErr != nil {
			return false
		}
		frames = append(frames, graphFrame{node: node, owner: owner})
		return true
	})
	if walkErr != nil {
		return walkErr
	}
	return c.ctx.Err()
}

func graphNodeRange(path string, tf *token.File, file snapshot.File, node ast.Node) (typedRange, error) {
	if node == nil || !node.Pos().IsValid() || !node.End().IsValid() || node.Pos() < token.Pos(tf.Base()) || node.End() > token.Pos(tf.Base()+tf.Size()) {
		return typedRange{}, ErrInputMismatch
	}
	start, end := tf.Offset(node.Pos()), tf.Offset(node.End())
	if file.CheckRange(start, end) != nil {
		return typedRange{}, ErrInputMismatch
	}
	return typedRange{path: path, start: int64(start), end: int64(end)}, nil
}

func graphIdentSite(path string, tf *token.File, file snapshot.File, id *ast.Ident) (typedSite, error) {
	if id == nil {
		return typedSite{}, ErrInputMismatch
	}
	r, err := graphNodeRange(path, tf, file, id)
	if err != nil {
		return typedSite{}, err
	}
	data, err := file.Range(int(r.start), int(r.end))
	if err != nil || string(data) != id.Name {
		return typedSite{}, ErrInputMismatch
	}
	return typedSite{path: path, start: r.start, end: r.end, name: id.Name}, nil
}

func (c *graphCollector) symbolAt(site typedSite, kind string) string {
	match := ""
	for _, symbol := range c.symbolSites[site] {
		if symbol.Kind == kind {
			if match != "" {
				return ""
			}
			match = symbol.SymbolID
		}
	}
	return match
}

func (c *graphCollector) graphIdentSymbol(path string, tf *token.File, file snapshot.File, id *ast.Ident, kind string) (string, error) {
	site, err := graphIdentSite(path, tf, file, id)
	if err != nil {
		return "", err
	}
	result := c.symbolAt(site, kind)
	if result == "" {
		return "", ErrInputMismatch
	}
	return result, nil
}

func (c *graphCollector) graphClosureSymbol(path string, tf *token.File, file snapshot.File, literal *ast.FuncLit) (string, error) {
	if literal == nil || literal.Type == nil || !literal.Type.Func.IsValid() {
		return "", ErrInputMismatch
	}
	start := tf.Offset(literal.Type.Func)
	if file.CheckRange(start, start+4) != nil {
		return "", ErrInputMismatch
	}
	result := c.symbolAt(typedSite{path: path, start: int64(start), end: int64(start + 4)}, "closure")
	if result == "" {
		return "", ErrInputMismatch
	}
	return result, nil
}

func (c *graphCollector) markAccess(path string, tf *token.File, file snapshot.File, expr ast.Expr, flags uint8, alias bool) error {
	for {
		parenthesized, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = parenthesized.X
	}
	var id *ast.Ident
	switch value := expr.(type) {
	case *ast.Ident:
		id = value
	case *ast.SelectorExpr:
		// Taking the address of a field, or selecting one as an lvalue,
		// does not by itself read or write the containing binding.
		if alias || flags&graphWrite != 0 {
			if err := c.markAccess(path, tf, file, value.X, 0, true); err != nil {
				return err
			}
		}
		id = value.Sel
	default:
		return nil // index, pointer and other compound lvalues remain conservative
	}
	site, err := graphIdentSite(path, tf, file, id)
	if err != nil {
		return err
	}
	occurrence, exists := c.uses[site]
	if !exists || occurrence.Resolution != "captured" || occurrence.ObjectKind != "variable" && occurrence.ObjectKind != "field" {
		return nil
	}
	if alias || occurrence.ObjectKind == "field" && occurrence.Indirect {
		delete(c.access, site)
		c.reasons["POINTER_ALIAS_BOUNDARY"] = true
	} else {
		c.access[site] = flags
	}
	return nil
}

func (c *graphCollector) addContainment(paths []string) error {
	for _, path := range paths {
		for _, symbol := range c.syntax[path].Symbols {
			if symbol.Kind == "file" {
				continue
			}
			owner := c.fileSymbols[path]
			scope := symbol.ScopeKey
			for depth := 0; scope != ""; depth++ {
				if depth > c.input.Typed.MaxDepth {
					return ErrInputMismatch
				}
				if found := c.scopeOwners[scope]; found != "" {
					owner = found
					break
				}
				current, ok := c.scopes[scope]
				if !ok {
					return ErrInputMismatch
				}
				scope = current.ParentID
			}
			if owner == symbol.SymbolID {
				return ErrInputMismatch
			}
			target := symbol.SymbolID
			if err := c.emit(owner, &target, "CONTAINS", "syntax_exact", path, symbol.Range.StartByte, symbol.Range.EndByte, "", nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *graphCollector) addImports(paths []string) error {
	anchors := map[string]string{}
	for _, path := range paths {
		importPath := c.syntax[path].Package.ImportPath
		for _, symbol := range c.syntax[path].Symbols {
			if symbol.Kind == "package" && anchors[importPath] == "" {
				anchors[importPath] = symbol.SymbolID
			}
		}
	}
	for _, path := range paths {
		for _, item := range c.syntax[path].Imports {
			if target := anchors[item.ImportPath]; target != "" {
				if err := c.emit(c.fileSymbols[path], &target, "IMPORTS", "syntax_exact", path, item.Range.StartByte, item.Range.EndByte, "captured_package_anchor", nil); err != nil {
					return err
				}
			} else {
				label := item.ImportPath
				if err := c.emit(c.fileSymbols[path], nil, "UNRESOLVED_REF", "unresolved", path, item.Range.StartByte, item.Range.EndByte, "external_or_unselected_package", &label); err != nil {
					return err
				}
				c.reasons["UNRESOLVED_BINDING"] = true
			}
		}
	}
	return nil
}

func (c *graphCollector) addCall(site typedRange, call TypedCallSite) error {
	owner := c.callOwners[site]
	ownerKind := c.symbols[owner.owner].Kind
	if ownerKind != "function" && ownerKind != "method" && ownerKind != "closure" {
		label := "call"
		if owner.callee != nil {
			label = owner.callee.name
		}
		c.reasons["NON_CALLABLE_OWNER_BOUNDARY"] = true
		return c.emit(owner.owner, nil, "UNRESOLVED_CALL", "unresolved", site.path, site.start, site.end, "non_callable_owner", &label)
	}
	if call.Kind == "call" && owner.literalTarget != "" {
		return c.emit(owner.owner, &owner.literalTarget, "CALLS_DIRECT", "type_resolved", site.path, site.start, site.end, "", nil)
	}
	label, reason := "call", "dynamic_callee"
	if owner.callee != nil {
		label = owner.callee.name
		occurrence, found := c.uses[*owner.callee]
		if found {
			if call.Kind == "call" && occurrence.ObjectKind == "function" && occurrence.Resolution == "captured" && occurrence.SymbolID != nil {
				kind := "CALLS_DIRECT"
				if occurrence.Dispatch == "interface" {
					kind = "CALLS_INTERFACE"
				}
				if err := c.emit(owner.owner, occurrence.SymbolID, kind, "type_resolved", site.path, site.start, site.end, "", nil); err != nil {
					return err
				}
				if kind == "CALLS_INTERFACE" {
					ids := append([]string(nil), occurrence.Candidates...)
					sort.Strings(ids)
					for _, candidate := range ids {
						if err := c.emit(*occurrence.SymbolID, &candidate, "MAY_DISPATCH_TO", "type_possible", site.path, site.start, site.end, "same_load_candidate", nil); err != nil {
							return err
						}
					}
					c.reasons["INTERFACE_CANDIDATES_SCOPED"] = true
				}
				return nil
			}
			switch {
			case occurrence.ObjectKind == "variable" || occurrence.ObjectKind == "field":
				reason = "function_value"
			case occurrence.Resolution == "builtin":
				reason = "builtin_call"
			case occurrence.Resolution != "captured":
				reason = "external_or_unmapped_binding"
			}
		}
	}
	c.reasons["UNRESOLVED_CALL"] = true
	return c.emit(owner.owner, nil, "UNRESOLVED_CALL", "unresolved", site.path, site.start, site.end, reason, &label)
}
