package contextpack

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// serializationBoundaries only interprets a captured Go struct field syntax.
// It never assumes that the field is actually serialized, or that a tag proves
// a stable contract in the presence of custom marshalers or reflection.
func serializationBoundaries(ctx context.Context, s *Selection, report Report) ([]model.Gap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var target model.SymbolFact
	for _, node := range report.Nodes {
		if node.SymbolID == report.Target.SymbolID {
			target = node
			break
		}
	}
	if target.Kind != "field" || !target.Exported || target.Path == nil || target.Range == nil || !strings.HasSuffix(*target.Path, ".go") {
		return nil, nil
	}
	file, ok := s.snapshot.File(*target.Path)
	if !ok {
		return nil, ErrInput
	}
	ref := source(s.member, file, *target.Range)
	gap := func(code, reason string) model.Gap {
		return model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{ref}}
	}
	base := []model.Gap{gap("SERIALIZATION_RUNTIME_NOT_PROVEN", "A captured struct field does not prove which serializers, custom marshalers or reflection use it.")}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, *target.Path, file.Bytes(), 0)
	if err != nil || parsed == nil {
		return append(base, gap("SERIALIZATION_BOUNDARY_UNKNOWN", "Field syntax could not be established from the captured file.")), nil
	}
	tokenFile := fset.File(parsed.Pos())
	if tokenFile == nil {
		return nil, ErrInput
	}
	var match *ast.Field
	count, depth, nodes := 0, 0, 0
	truncated := false
	ast.Inspect(parsed, func(n ast.Node) bool {
		if n == nil {
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
		structure, ok := n.(*ast.StructType)
		if !ok || structure.Fields == nil {
			return true
		}
		for _, field := range structure.Fields.List {
			names := field.Names
			if len(names) == 0 {
				if name := embeddedName(field.Type); name != nil {
					names = []*ast.Ident{name}
				}
			}
			for _, name := range names {
				if name != nil && name.Name == target.Name && ast.IsExported(name.Name) && int64(tokenFile.Offset(name.Pos())) == target.Range.StartByte && int64(tokenFile.Offset(name.End())) == target.Range.EndByte {
					count++
					match = field
				}
			}
		}
		return true
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if truncated {
		return append(base, gap("SERIALIZATION_BOUNDARY_UNKNOWN", "Field syntax exceeded the bounded inspection limits.")), nil
	}
	if count != 1 {
		return append(base, gap("SERIALIZATION_BOUNDARY_UNKNOWN", "The captured field declaration was not uniquely identified.")), nil
	}
	implicit := false
	if match.Tag == nil {
		implicit = true
	} else {
		tag, e := strconv.Unquote(match.Tag.Value)
		if e != nil {
			return append(base, gap("SERIALIZATION_BOUNDARY_UNKNOWN", "The captured field tag could not be interpreted.")), nil
		}
		value, exists := reflect.StructTag(tag).Lookup("json")
		if !exists {
			return append(base, gap("SERIALIZATION_BOUNDARY_UNKNOWN", "The JSON tag is absent or malformed; review the original declaration.")), nil
		}
		name, _, _ := strings.Cut(value, ",")
		implicit = name == ""
	}
	if implicit {
		base = append(base, gap("IMPLICIT_SERIALIZATION_NAME_CHANGE", "Renaming an exported field may change its default JSON name; verify the actual serialization contract."))
	}
	return base, nil
}

func embeddedName(expr ast.Expr) *ast.Ident {
	switch n := expr.(type) {
	case *ast.Ident:
		return n
	case *ast.StarExpr:
		return embeddedName(n.X)
	case *ast.SelectorExpr:
		return n.Sel
	case *ast.IndexExpr:
		return embeddedName(n.X)
	case *ast.IndexListExpr:
		return embeddedName(n.X)
	}
	return nil
}
