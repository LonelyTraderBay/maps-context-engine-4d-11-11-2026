package contextpack

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type declarationKey struct{ start, end int64 }

type sourceQuota struct {
	maxFile, maxTotal, used int64
	seen                    map[string]bool
}

func (q *sourceQuota) charge(file snapshot.File) error {
	if q == nil || file.Path() == "" {
		return ErrInput
	}
	if q.seen[file.Path()] {
		return nil
	}
	length := int64(file.Len())
	if length > q.maxFile || length > q.maxTotal-q.used {
		return ErrLimit
	}
	q.used += length
	q.seen[file.Path()] = true
	return nil
}

// immediateDeclarations expands proven neighboring Go declarations from the
// same captured bytes. Unsupported or truncated parsing remains unknown.
func immediateDeclarations(ctx context.Context, s *Selection, report Report, quota *sourceQuota) ([]SourceRange, bool, error) {
	wanted := map[string]bool{}
	for _, ref := range report.References {
		r := ref.Relation
		switch r.Kind {
		case "REFERS_TO", "READS", "WRITES", "CALLS_DIRECT", "CALLS_INTERFACE", "MAY_DISPATCH_TO", "TEST_REF":
		default:
			continue
		}
		if r.EvidenceKind == "text_match" || r.TargetSymbolID == nil {
			continue
		}
		if r.SourceSymbolID == report.Target.SymbolID {
			wanted[*r.TargetSymbolID] = true
		}
		if *r.TargetSymbolID == report.Target.SymbolID {
			wanted[r.SourceSymbolID] = true
		}
	}
	delete(wanted, report.Target.SymbolID)
	byID := map[string]model.SymbolFact{}
	for _, node := range report.Nodes {
		byID[node.SymbolID] = node
	}
	indices := map[string]map[declarationKey][]model.ByteRange{}
	failed := map[string]bool{}
	unknown := false
	out := []SourceRange{}
	for id := range wanted {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		node, ok := byID[id]
		if !ok || node.Path == nil || node.Range == nil || !strings.HasSuffix(*node.Path, ".go") {
			unknown = true
			continue
		}
		filename := *node.Path
		file, ok := s.snapshot.File(filename)
		if !ok {
			return nil, false, ErrInput
		}
		if err := quota.charge(file); err != nil {
			return nil, false, err
		}
		if indices[filename] == nil && !failed[filename] {
			if file.Len() > 2*1024*1024 {
				failed[filename] = true
			} else {
				index, complete, err := indexDeclarations(ctx, filename, file.Bytes())
				if err != nil {
					return nil, false, err
				}
				if !complete {
					failed[filename] = true
				} else {
					indices[filename] = index
				}
			}
		}
		if failed[filename] {
			unknown = true
			continue
		}
		matches := indices[filename][declarationKey{node.Range.StartByte, node.Range.EndByte}]
		if len(matches) != 1 {
			unknown = true
			continue
		}
		if err := file.CheckRange(int(matches[0].StartByte), int(matches[0].EndByte)); err != nil {
			return nil, false, ErrInput
		}
		out = append(out, SourceRange{Source: source(s.member, file, matches[0]), Priority: 1, Reasons: []string{"immediate_declaration"}})
	}
	return out, unknown, nil
}

func indexDeclarations(ctx context.Context, path string, bytes []byte) (map[declarationKey][]model.ByteRange, bool, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, bytes, 0)
	if err != nil || parsed == nil {
		return nil, false, nil
	}
	tf := fset.File(parsed.Pos())
	if tf == nil {
		return nil, false, ErrInput
	}
	index := map[declarationKey][]model.ByteRange{}
	depth, nodes := 0, 0
	truncated := false
	add := func(name *ast.Ident, start, end token.Pos) {
		if name == nil || !name.Pos().IsValid() || !name.End().IsValid() || !start.IsValid() || !end.IsValid() {
			return
		}
		key := declarationKey{int64(tf.Offset(name.Pos())), int64(tf.Offset(name.End()))}
		span := model.ByteRange{StartByte: int64(tf.Offset(start)), EndByte: int64(tf.Offset(end))}
		index[key] = append(index[key], span)
	}
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
		switch n := node.(type) {
		case *ast.FuncDecl:
			add(n.Name, n.Pos(), n.End())
		case *ast.FuncLit:
			if n.Type != nil && n.Type.Func.IsValid() {
				start := int64(tf.Offset(n.Type.Func))
				index[declarationKey{start, start + 4}] = append(index[declarationKey{start, start + 4}], model.ByteRange{StartByte: int64(tf.Offset(n.Pos())), EndByte: int64(tf.Offset(n.End()))})
			}
		case *ast.TypeSpec:
			add(n.Name, n.Pos(), n.End())
		case *ast.ValueSpec:
			for _, name := range n.Names {
				add(name, n.Pos(), n.End())
			}
		case *ast.Field:
			for _, name := range n.Names {
				add(name, n.Pos(), n.End())
			}
			if len(n.Names) == 0 {
				add(embeddedName(n.Type), n.Pos(), n.End())
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, expr := range n.Lhs {
					if name, ok := expr.(*ast.Ident); ok {
						add(name, n.Pos(), n.End())
					}
				}
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, expr := range []ast.Expr{n.Key, n.Value} {
					if name, ok := expr.(*ast.Ident); ok {
						add(name, n.Pos(), n.End())
					}
				}
			}
		}
		return true
	})
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return index, !truncated, nil
}

// candidateContents reads only preselected or internally expanded ranges.
func candidateContents(ctx context.Context, s *Selection, ranges []SourceRange, maxBytes int, quota *sourceQuota) ([]SourceContent, error) {
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	result := make([]SourceContent, 0, len(ranges))
	used := 0
	for _, r := range ranges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, ok := s.snapshot.File(r.Source.Path)
		if !ok || file.ContentHash() != r.Source.ContentHash {
			return nil, ErrInput
		}
		if err := quota.charge(file); err != nil {
			return nil, err
		}
		if err := s.scanner.CheckContent(file.Bytes()); err != nil {
			return nil, err
		}
		data, err := file.Range(int(r.Source.Range.StartByte), int(r.Source.Range.EndByte))
		if err != nil {
			return nil, ErrInput
		}
		if len(data) > maxBytes-used {
			return nil, ErrLimit
		}
		used += len(data)
		result = append(result, SourceContent{Source: r.Source, Content: string(data)})
	}
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
