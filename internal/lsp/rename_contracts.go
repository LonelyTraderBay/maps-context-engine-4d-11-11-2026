package lsp

import (
	"context"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// InspectRenameContracts conservatively checks captured Go syntax and returns
// boundaries for an already validated edit set. It does not prove that any
// serializer or reflection path is actually used, and never applies an edit.
func InspectRenameContracts(ctx context.Context, documents []CapturedDocument, edits []model.ProposedEdit, preserve bool) ([]model.Gap, error) {
	if ctx == nil || len(documents) == 0 || len(documents) > 128 || len(edits) == 0 || len(edits) > 10000 {
		return nil, ErrWorkspaceEdit
	}
	byPath := make(map[string]CapturedDocument, len(documents))
	var total int64
	for _, doc := range documents {
		if doc.File.Path() == "" || doc.Ref.Path != doc.File.Path() || doc.Ref.ContentHash != doc.File.ContentHash() || doc.File.Len() > 64*1024 {
			return nil, ErrWorkspaceEdit
		}
		total += int64(doc.File.Len())
		if total > 2*1024*1024 {
			return nil, ErrLSPAuthority
		}
		if _, duplicate := byPath[doc.Ref.Path]; duplicate {
			return nil, ErrWorkspaceEdit
		}
		byPath[doc.Ref.Path] = doc
	}
	perFile := make(map[string][]model.ProposedEdit)
	for _, edit := range edits {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, ok := byPath[edit.SourceRef.Path]
		ref := edit.SourceRef
		if !ok || ref.WorktreeID != doc.Ref.WorktreeID || ref.ProfileID != doc.Ref.ProfileID || ref.GenerationID != doc.Ref.GenerationID || ref.ContentHash != doc.Ref.ContentHash || ref.Range.StartByte < 0 || ref.Range.EndByte < ref.Range.StartByte || ref.Range.EndByte > int64(doc.File.Len()) || doc.File.CheckRange(int(ref.Range.StartByte), int(ref.Range.EndByte)) != nil {
			return nil, ErrWorkspaceEdit
		}
		if preserve && strings.ContainsAny(edit.NewText, "\"'`") {
			return nil, ErrWorkspaceEdit
		}
		perFile[ref.Path] = append(perFile[ref.Path], edit)
	}
	gaps := []model.Gap{{Code: "REFLECTION_RUNTIME_NOT_PROVEN", Reason: "Reflection and runtime string lookups require separate review.", SourceRefs: []model.SourceRef{}}}
	for path, fileEdits := range perFile {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc := byPath[path]
		source := doc.File.Bytes()
		if preserve && editsTouchLiteral(path, source, fileEdits) {
			return nil, ErrWorkspaceEdit
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, path, source, 0)
		if err != nil || parsed == nil {
			return nil, ErrLSPAuthority
		}
		tokens := fset.File(parsed.Pos())
		if tokens == nil {
			return nil, ErrLSPAuthority
		}
		byRange := make(map[model.ByteRange]model.ProposedEdit, len(fileEdits))
		for _, edit := range fileEdits {
			byRange[edit.SourceRef.Range] = edit
		}
		nodes, depth := 0, 0
		truncated := false
		ast.Inspect(parsed, func(node ast.Node) bool {
			if node == nil {
				depth--
				return true
			}
			depth++
			nodes++
			if depth > 512 || nodes > 100000 || ctx.Err() != nil {
				depth--
				truncated = true
				return false
			}
			structure, ok := node.(*ast.StructType)
			if !ok || structure.Fields == nil {
				return true
			}
			for _, field := range structure.Fields.List {
				names := field.Names
				if len(names) == 0 {
					if embedded := renameEmbeddedName(field.Type); embedded != nil {
						names = []*ast.Ident{embedded}
					}
				}
				for _, name := range names {
					if name == nil || !ast.IsExported(name.Name) {
						continue
					}
					bounds := model.ByteRange{StartByte: int64(tokens.Offset(name.Pos())), EndByte: int64(tokens.Offset(name.End()))}
					edit, exists := byRange[bounds]
					if !exists {
						continue
					}
					gap := func(code, reason string) {
						gaps = append(gaps, model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{edit.SourceRef}})
					}
					gap("SERIALIZATION_RUNTIME_NOT_PROVEN", "Captured syntax does not prove which serializers or custom marshalers use this field.")
					if field.Tag == nil {
						gap("IMPLICIT_SERIALIZATION_NAME_CHANGE", "Renaming this exported field may change its default JSON name.")
						continue
					}
					tag, err := strconv.Unquote(field.Tag.Value)
					if err != nil {
						gap("SERIALIZATION_BOUNDARY_UNKNOWN", "The captured field tag could not be interpreted.")
						continue
					}
					value, exists := reflect.StructTag(tag).Lookup("json")
					if !exists {
						gap("SERIALIZATION_BOUNDARY_UNKNOWN", "The JSON tag is absent or malformed; inspect the declaration.")
						gap("IMPLICIT_SERIALIZATION_NAME_CHANGE", "Renaming this exported field may change its default JSON name.")
						continue
					}
					tagName, _, _ := strings.Cut(value, ",")
					if !validJSONTagName(tagName) {
						gap("IMPLICIT_SERIALIZATION_NAME_CHANGE", "Renaming this exported field may change its default JSON name.")
					}
				}
			}
			return true
		})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if truncated || len(gaps) > 10000 {
			return nil, ErrLSPAuthority
		}
	}
	return gaps, nil
}

func validJSONTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) {
			return false
		}
	}
	return true
}

func editsTouchLiteral(path string, source []byte, edits []model.ProposedEdit) bool {
	fset := token.NewFileSet()
	file := fset.AddFile(path, -1, len(source))
	invalid := false
	var scan scanner.Scanner
	type span struct{ start, end int64 }
	literals := []span{}
	pending := int64(-1)
	scan.Init(file, source, func(token.Position, string) { invalid = true }, 0)
	for {
		position, kind, _ := scan.Scan()
		offset := int64(file.Offset(position))
		if pending >= 0 {
			if offset < pending {
				return true
			}
			// The scanner normalizes CRs in raw strings. Use the next token's
			// original offset as a conservative end, including intervening trivia.
			literals = append(literals, span{start: pending, end: offset})
			pending = -1
		}
		if kind == token.EOF {
			break
		}
		if kind == token.STRING || kind == token.CHAR || kind == token.INT || kind == token.FLOAT || kind == token.IMAG {
			pending = offset
		}
	}
	for _, edit := range edits {
		r := edit.SourceRef.Range
		i := sort.Search(len(literals), func(i int) bool { return literals[i].end > r.StartByte })
		if i < len(literals) && (r.StartByte < literals[i].end && r.EndByte > literals[i].start || r.StartByte == r.EndByte && r.StartByte > literals[i].start && r.StartByte < literals[i].end) {
			return true
		}
	}
	return invalid
}

func renameEmbeddedName(expr ast.Expr) *ast.Ident {
	for i := 0; i < 512; i++ {
		switch value := expr.(type) {
		case *ast.Ident:
			return value
		case *ast.StarExpr:
			expr = value.X
		case *ast.SelectorExpr:
			return value.Sel
		case *ast.IndexExpr:
			expr = value.X
		case *ast.IndexListExpr:
			expr = value.X
		default:
			return nil
		}
	}
	return nil
}
