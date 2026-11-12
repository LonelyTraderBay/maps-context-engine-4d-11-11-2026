package golang

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type syntaxFrame struct {
	node   ast.Node
	scopes int
}

type syntaxCollector struct {
	ctx                context.Context
	input              SyntaxInput
	fset               *token.FileSet
	source             []byte
	result             SyntaxResult
	frames             []syntaxFrame
	scopes             []string
	occurrenceScopes   []string
	fieldKinds         map[*ast.FieldList]string
	typeSwitchBindings map[*ast.CaseClause]*ast.Ident
	rangeBodies        map[*ast.BlockStmt]*ast.RangeStmt
	seenSymbols        map[string]bool
	nodes              int
	outputBytes        int
	err                error
}

func collectSyntax(ctx context.Context, input SyntaxInput, fset *token.FileSet, file *ast.File, source []byte, result SyntaxResult) (SyntaxResult, error) {
	c := &syntaxCollector{ctx: ctx, input: input, fset: fset, source: source, result: result, fieldKinds: map[*ast.FieldList]string{}, typeSwitchBindings: map[*ast.CaseClause]*ast.Ident{}, rangeBodies: map[*ast.BlockStmt]*ast.RangeStmt{}, seenSymbols: map[string]bool{}}
	root, err := identityHash("go-scope-v1", input.PackageKey, input.Variant, "", "package", "", 0, 0)
	if err != nil {
		return SyntaxResult{}, err
	}
	c.scopes = []string{root}
	occurrenceRoot, err := identityHash("go-occurrence-scope-v1", input.ImportPath, "", "", "package", result.SnapshotDigest, 0, 0)
	if err != nil {
		return SyntaxResult{}, err
	}
	c.occurrenceScopes = []string{occurrenceRoot}
	if !c.reserveOutput(512, input.Path, input.PackageKey, input.ImportPath, input.ModuleKey, input.Variant, root, result.ContentHash, result.SnapshotDigest) {
		return SyntaxResult{}, ErrSyntaxLimit
	}
	c.result.Scopes = append(c.result.Scopes, Scope{ID: root, Kind: "package"})
	if utf8.RuneCountInString(path.Base(input.Path)) > 512 {
		return SyntaxResult{}, ErrSyntaxLimit
	}
	c.emit("file", path.Base(input.Path), 0, int64(len(source)), "", false)
	c.emitIdent("package", file.Name, "", false)
	ast.Inspect(file, c.visit)
	if c.err != nil {
		return SyntaxResult{}, c.err
	}
	if err := ctx.Err(); err != nil {
		return SyntaxResult{}, err
	}
	raw, err := json.Marshal(c.result)
	if err != nil {
		return SyntaxResult{}, err
	}
	if len(raw) > input.MaxOutputBytes {
		return SyntaxResult{}, ErrSyntaxLimit
	}
	if err := ctx.Err(); err != nil {
		return SyntaxResult{}, err
	}
	return c.result, nil
}

func (c *syntaxCollector) visit(node ast.Node) bool {
	if node == nil {
		if len(c.frames) > 0 {
			frame := c.frames[len(c.frames)-1]
			c.frames = c.frames[:len(c.frames)-1]
			if frame.scopes > 0 {
				c.scopes = c.scopes[:len(c.scopes)-frame.scopes]
				c.occurrenceScopes = c.occurrenceScopes[:len(c.occurrenceScopes)-frame.scopes]
			}
		}
		return true
	}
	if c.err != nil {
		return false
	}
	c.nodes++
	if c.nodes > c.input.MaxNodes || len(c.frames)+1 > c.input.MaxDepth {
		c.err = ErrSyntaxLimit
		return false
	}
	if c.nodes%64 == 0 {
		if err := c.ctx.Err(); err != nil {
			c.err = err
			return false
		}
	}
	var parent ast.Node
	if len(c.frames) > 0 {
		parent = c.frames[len(c.frames)-1].node
	}
	openKind := ""
	var caseBinding *ast.Ident
	var receiverTypeParams []*ast.Ident
	switch n := node.(type) {
	case *ast.FuncDecl:
		kind := "function"
		if n.Recv != nil {
			kind = "method"
			c.fieldKinds[n.Recv] = "receiver"
			receiverTypeParams = receiverParameters(n.Recv)
		}
		c.emitIdent(kind, n.Name, c.signature(n.Type), n.Name != nil && n.Name.IsExported())
		c.functionFields(n.Type)
		openKind = "function"
	case *ast.FuncLit:
		start, err := c.offset(n.Type.Func)
		if err != nil {
			c.err = err
			break
		}
		c.emit("closure", "", start, start+4, c.signature(n.Type), false)
		c.functionFields(n.Type)
		openKind = "closure"
	case *ast.FuncType:
		c.functionFields(n)
		switch parent.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
		default:
			openKind = "function_type"
		}
	case *ast.TypeSpec:
		kind := "type"
		if _, ok := n.Type.(*ast.InterfaceType); ok {
			kind = "interface"
		}
		c.emitIdent(kind, n.Name, "", n.Name != nil && n.Name.IsExported())
		if n.TypeParams != nil {
			c.fieldKinds[n.TypeParams] = "type"
		}
		openKind = "type"
	case *ast.StructType:
		if n.Fields != nil {
			c.fieldKinds[n.Fields] = "field"
		}
		openKind = "struct"
	case *ast.InterfaceType:
		if n.Methods != nil {
			c.fieldKinds[n.Methods] = "method"
		}
		openKind = "interface"
	case *ast.Field:
		if fields, ok := parent.(*ast.FieldList); ok {
			kind := c.fieldKinds[fields]
			signature := ""
			if kind == "method" {
				signature = c.signature(n.Type)
			} else if kind == "field" && n.Type != nil {
				end := n.Type.End()
				if n.Tag != nil {
					end = n.Tag.End()
				}
				signature = c.signatureRange(n.Type.Pos(), end)
			}
			for _, name := range n.Names {
				c.emitIdent(kind, name, signature, name != nil && name.IsExported() && (kind == "field" || kind == "method"))
			}
			if kind == "field" && len(n.Names) == 0 {
				if name := embeddedFieldName(n.Type); name != nil {
					c.emitIdent(kind, name, signature, name.IsExported())
				}
			}
			if kind == "field" && n.Tag != nil {
				c.recordTags(n)
			}
		}
	case *ast.ValueSpec:
		if decl, ok := parent.(*ast.GenDecl); ok {
			kind := "variable"
			if decl.Tok == token.CONST {
				kind = "constant"
			}
			if decl.Tok == token.CONST || decl.Tok == token.VAR {
				for _, name := range n.Names {
					c.emitIdent(kind, name, "", name != nil && name.IsExported() && len(c.scopes) == 1)
				}
			}
		}
	case *ast.AssignStmt:
		_, isTypeSwitch := parent.(*ast.TypeSwitchStmt)
		if n.Tok == token.DEFINE && !isTypeSwitch {
			for _, expr := range n.Lhs {
				if id, ok := expr.(*ast.Ident); ok && id.Name != "_" {
					if id.Obj == nil {
						c.err = ErrInputMismatch
						break
					}
					if id.Obj.Decl == n {
						c.emitIdent("variable", id, "", false)
					}
				}
			}
		}
	case *ast.RangeStmt:
		if n.Body != nil {
			c.rangeBodies[n.Body] = n
		}
	case *ast.TypeSwitchStmt:
		openKind = "type_switch"
		if assign, ok := n.Assign.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE && len(assign.Lhs) == 1 {
			if id, ok := assign.Lhs[0].(*ast.Ident); ok {
				for _, clause := range n.Body.List {
					if item, ok := clause.(*ast.CaseClause); ok {
						c.typeSwitchBindings[item] = id
					}
				}
			}
		}
	case *ast.IfStmt:
		openKind = "if"
	case *ast.ForStmt:
		openKind = "for"
	case *ast.SwitchStmt:
		openKind = "switch"
	case *ast.SelectStmt:
		openKind = "select"
	case *ast.CaseClause:
		openKind = "case"
		caseBinding = c.typeSwitchBindings[n]
	case *ast.CommClause:
		openKind = "communication"
	case *ast.BlockStmt:
		if c.rangeBodies[n] == nil {
			switch parent.(type) {
			case *ast.FuncDecl, *ast.FuncLit:
			default:
				openKind = "block"
			}
		}
	case *ast.ImportSpec:
		c.recordImport(n)
	}
	if c.err != nil {
		return false
	}
	opened := 0
	if block, ok := node.(*ast.BlockStmt); ok && c.rangeBodies[block] != nil {
		rangeStmt := c.rangeBodies[block]
		c.openScope("range", block)
		if c.err != nil {
			return false
		}
		opened++
		if rangeStmt.Tok == token.DEFINE {
			for _, value := range []ast.Expr{rangeStmt.Key, rangeStmt.Value} {
				if id, ok := value.(*ast.Ident); ok {
					c.emitIdent("variable", id, "", false)
				}
			}
		}
		if c.err == nil {
			c.openScope("block", block)
			if c.err == nil {
				opened++
			}
		}
	}
	if openKind != "" {
		c.openScope(openKind, node)
		if c.err != nil {
			if opened > 0 {
				c.scopes = c.scopes[:len(c.scopes)-opened]
				c.occurrenceScopes = c.occurrenceScopes[:len(c.occurrenceScopes)-opened]
			}
			return false
		}
		opened++
		if caseBinding != nil {
			c.emitIdent("variable", caseBinding, "", false)
		}
		for _, id := range receiverTypeParams {
			c.emitIdent("type", id, "", false)
		}
	}
	if c.err != nil {
		if opened > 0 {
			c.scopes = c.scopes[:len(c.scopes)-opened]
			c.occurrenceScopes = c.occurrenceScopes[:len(c.occurrenceScopes)-opened]
		}
		return false
	}
	c.frames = append(c.frames, syntaxFrame{node: node, scopes: opened})
	return true
}

func (c *syntaxCollector) functionFields(function *ast.FuncType) {
	if function == nil {
		return
	}
	if function.Params != nil {
		c.fieldKinds[function.Params] = "parameter"
	}
	if function.Results != nil {
		c.fieldKinds[function.Results] = "result"
	}
	if function.TypeParams != nil {
		c.fieldKinds[function.TypeParams] = "type"
	}
}

func (c *syntaxCollector) offset(pos token.Pos) (int64, error) {
	file := c.fset.File(pos)
	if file == nil {
		return 0, ErrInputMismatch
	}
	offset := file.Offset(pos)
	if offset < 0 || offset > len(c.source) {
		return 0, ErrInputMismatch
	}
	return int64(offset), nil
}

func (c *syntaxCollector) signature(node ast.Node) string {
	if node == nil || c.err != nil {
		return ""
	}
	return c.signatureRange(node.Pos(), node.End())
}

func (c *syntaxCollector) signatureRange(first, last token.Pos) string {
	if c.err != nil {
		return ""
	}
	start, err := c.offset(first)
	if err != nil {
		c.err = err
		return ""
	}
	end, err := c.offset(last)
	if err != nil || end < start || end > int64(len(c.source)) {
		c.err = ErrInputMismatch
		return ""
	}
	if end-start > 131072 || end-start > int64((c.input.MaxOutputBytes-c.outputBytes)/6) || utf8.RuneCount(c.source[start:end]) > 32768 {
		c.err = ErrSyntaxLimit
		return ""
	}
	text := string(c.source[start:end])
	if err := c.input.Scanner.CheckContent([]byte(text)); err != nil {
		c.err = err
		return ""
	}
	return text
}

func (c *syntaxCollector) openScope(kind string, node ast.Node) {
	start, err := c.offset(node.Pos())
	if err != nil {
		c.err = err
		return
	}
	end, err := c.offset(node.End())
	if err != nil || end < start {
		c.err = ErrInputMismatch
		return
	}
	parent := c.scopes[len(c.scopes)-1]
	id, err := identityHash("go-scope-v1", c.input.PackageKey, c.input.Variant, c.input.Path, kind, parent, start, end)
	if err != nil {
		c.err = err
		return
	}
	if c.factCount() >= c.input.MaxFacts {
		c.err = ErrSyntaxLimit
		return
	}
	occurrenceParent := c.occurrenceScopes[len(c.occurrenceScopes)-1]
	occurrenceID, err := identityHash("go-occurrence-scope-v1", c.input.ImportPath, "", c.input.Path, kind, occurrenceParent, start, end)
	if err != nil {
		c.err = err
		return
	}
	if !c.reserveOutput(128, id, parent, kind, c.input.Path) {
		c.err = ErrSyntaxLimit
		return
	}
	c.result.Scopes = append(c.result.Scopes, Scope{ID: id, ParentID: parent, Kind: kind, Path: c.input.Path, StartByte: start, EndByte: end})
	c.scopes = append(c.scopes, id)
	c.occurrenceScopes = append(c.occurrenceScopes, occurrenceID)
}

func (c *syntaxCollector) emitIdent(kind string, id *ast.Ident, signature string, exported bool) {
	if id == nil || id.Name == "_" || kind == "" {
		return
	}
	start, err := c.offset(id.Pos())
	if err != nil {
		c.err = err
		return
	}
	end, err := c.offset(id.End())
	if err != nil {
		c.err = err
		return
	}
	c.emit(kind, id.Name, start, end, signature, exported)
}

func (c *syntaxCollector) emit(kind, name string, start, end int64, signature string, exported bool) {
	if c.err != nil {
		return
	}
	if c.factCount() >= c.input.MaxFacts || start < 0 || end < start || end > int64(len(c.source)) || utf8.RuneCountInString(name) > 512 {
		c.err = ErrSyntaxLimit
		return
	}
	owner := c.scopes[len(c.scopes)-1]
	id, err := identityHash("symbol-v1", c.input.PackageKey, c.input.Variant, c.input.Path, kind, owner, start, end)
	if err != nil {
		c.err = err
		return
	}
	if c.seenSymbols[id] {
		c.err = ErrInputMismatch
		return
	}
	occurrenceID, err := identityHash("go-occurrence-v1", c.input.ImportPath, "", c.input.Path, kind, c.occurrenceScopes[len(c.occurrenceScopes)-1], start, end)
	if err != nil {
		c.err = err
		return
	}
	qualified := c.input.ImportPath + "." + name
	if utf8.RuneCountInString(qualified) > 2048 {
		c.err = ErrSyntaxLimit
		return
	}
	if !c.reserveOutput(384, id, owner, occurrenceID, name, kind, qualified, signature, c.input.Path, c.input.PackageKey) {
		c.err = ErrSyntaxLimit
		return
	}
	filePath, packageKey := c.input.Path, c.input.PackageKey
	symbol := model.SymbolFact{SymbolID: id, Path: &filePath, PackageKey: &packageKey, Kind: kind, Name: name, QualifiedName: qualified, ScopeKey: owner, Range: &model.ByteRange{StartByte: start, EndByte: end}, Signature: signature, Exported: exported}
	raw, err := json.Marshal(symbol)
	if err != nil {
		c.err = err
		return
	}
	if _, err := model.DecodeSymbolFactJSON(raw, 1024*1024); err != nil {
		c.err = ErrInputMismatch
		return
	}
	c.seenSymbols[id] = true
	c.result.Symbols = append(c.result.Symbols, symbol)
	c.result.Occurrences = append(c.result.Occurrences, SyntaxOccurrence{SymbolID: id, OccurrenceID: occurrenceID})
}

func embeddedFieldName(expr ast.Expr) *ast.Ident {
	switch value := expr.(type) {
	case *ast.Ident:
		return value
	case *ast.StarExpr:
		return embeddedFieldName(value.X)
	case *ast.SelectorExpr:
		return value.Sel
	case *ast.IndexExpr:
		return embeddedFieldName(value.X)
	case *ast.IndexListExpr:
		return embeddedFieldName(value.X)
	case *ast.ParenExpr:
		return embeddedFieldName(value.X)
	default:
		return nil
	}
}

func receiverParameters(fields *ast.FieldList) []*ast.Ident {
	if fields == nil || len(fields.List) != 1 {
		return nil
	}
	expr := fields.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	var expressions []ast.Expr
	switch value := expr.(type) {
	case *ast.IndexExpr:
		expressions = []ast.Expr{value.Index}
	case *ast.IndexListExpr:
		expressions = value.Indices
	}
	var result []*ast.Ident
	for _, expression := range expressions {
		if id, ok := expression.(*ast.Ident); ok {
			result = append(result, id)
		}
	}
	return result
}

func (c *syntaxCollector) factCount() int {
	return len(c.result.Imports) + len(c.result.Tags) + len(c.result.Symbols) + len(c.result.Scopes) + len(c.result.Diagnostics)
}

// Six bytes per input byte conservatively bounds JSON escape expansion; the
// fixed overhead covers field names, numeric ranges and container separators.
func (c *syntaxCollector) reserveOutput(overhead int, values ...string) bool {
	remaining := c.input.MaxOutputBytes - c.outputBytes
	if overhead > remaining {
		return false
	}
	remaining -= overhead
	for _, value := range values {
		if len(value) > remaining/6 {
			return false
		}
		remaining -= len(value) * 6
	}
	c.outputBytes = c.input.MaxOutputBytes - remaining
	return true
}

func (c *syntaxCollector) recordImport(item *ast.ImportSpec) {
	if item.Path == nil {
		c.err = ErrInputMismatch
		return
	}
	value, err := strconv.Unquote(item.Path.Value)
	if err != nil || value == "" || utf8.RuneCountInString(value) > 2048 {
		c.err = ErrInputMismatch
		return
	}
	if err := c.input.Scanner.CheckContent([]byte(value)); err != nil {
		c.err = err
		return
	}
	start, err := c.offset(item.Path.Pos())
	if err != nil {
		c.err = err
		return
	}
	end, err := c.offset(item.Path.End())
	if err != nil {
		c.err = err
		return
	}
	if c.factCount() >= c.input.MaxFacts {
		c.err = ErrSyntaxLimit
		return
	}
	alias := ""
	if item.Name != nil {
		alias = item.Name.Name
	}
	if !c.reserveOutput(128, value, alias) {
		c.err = ErrSyntaxLimit
		return
	}
	c.result.Imports = append(c.result.Imports, SyntaxImport{ImportPath: value, Alias: alias, Range: model.ByteRange{StartByte: start, EndByte: end}})
}

func (c *syntaxCollector) recordTags(field *ast.Field) {
	start, err := c.offset(field.Tag.Pos())
	if err != nil {
		c.err = err
		return
	}
	end, err := c.offset(field.Tag.End())
	if err != nil || end < start || end > int64(len(c.source)) || end-start > 32768 {
		c.err = ErrSyntaxLimit
		return
	}
	literal := string(c.source[start:end])
	if err := c.input.Scanner.CheckContent([]byte(literal)); err != nil {
		c.err = err
		return
	}
	names := field.Names
	if len(names) == 0 {
		if name := embeddedFieldName(field.Type); name != nil {
			names = []*ast.Ident{name}
		}
	}
	for _, name := range names {
		if name == nil || name.Name == "_" {
			continue
		}
		nameStart, err := c.offset(name.Pos())
		if err != nil {
			c.err = err
			return
		}
		nameEnd, err := c.offset(name.End())
		if err != nil {
			c.err = err
			return
		}
		id, err := identityHash("symbol-v1", c.input.PackageKey, c.input.Variant, c.input.Path, "field", c.scopes[len(c.scopes)-1], nameStart, nameEnd)
		if err != nil || !c.seenSymbols[id] {
			c.err = ErrInputMismatch
			return
		}
		if c.factCount() >= c.input.MaxFacts {
			c.err = ErrSyntaxLimit
			return
		}
		if !c.reserveOutput(128, id, literal) {
			c.err = ErrSyntaxLimit
			return
		}
		c.result.Tags = append(c.result.Tags, SyntaxTag{SymbolID: id, Literal: literal, Range: model.ByteRange{StartByte: start, EndByte: end}})
	}
}
