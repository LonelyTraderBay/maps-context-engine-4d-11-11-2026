package golang

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
	"time"
)

// The oracle uses a separate type-checker run and does not feed its objects
// into the syntax analyzer or its identity calculation.
func TestSyntaxDeclarationsMatchIndependentTypeOracle(t *testing.T) {
	const source = `package sample
type Box[T any] struct { Field T }
func (b *Box[T]) Use(p int) (result int) {
	x := p
	x, y := 2, 1
	{ x := y; _ = x }
	for k, v := range []int{x,y} { _ = k; _ = v }
	return x
}
func Flow(ch <-chan int, xs []int) {
	for i := 0; i < len(xs); i++ { value := xs[i]; _ = value }
	if branch := <-ch; branch > 0 { _ = branch } else { other := 1; _ = other }
	switch tag := len(xs); tag { case 1: first := tag; _ = first; default: second := tag; _ = second }
	select { case msg := <-ch: _ = msg; default: }
}
`
	input := syntaxFixture(t, "oracle.go", source)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "oracle.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	info := types.Info{Defs: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{}).Check("example.invalid/fixture", fset, []*ast.File{file}, &info); err != nil {
		t.Fatal(err)
	}
	byOffset := map[int64]modelSymbol{}
	for _, symbol := range result.Symbols {
		if symbol.Kind != "file" && symbol.Kind != "package" && symbol.Kind != "closure" && symbol.Range != nil {
			byOffset[symbol.Range.StartByte] = modelSymbol{name: symbol.Name, scope: symbol.ScopeKey}
		}
	}
	var xBindings []struct {
		expected *types.Scope
		actual   string
	}
	checked := 0
	for id, object := range info.Defs {
		if object == nil || id.Name == "_" {
			continue
		}
		switch object.(type) {
		case *types.Var, *types.Const, *types.TypeName, *types.Func:
		default:
			continue
		}
		offset := int64(fset.File(id.Pos()).Offset(id.Pos()))
		actual, ok := byOffset[offset]
		if !ok || actual.name != id.Name {
			t.Errorf("missing declaration %s at byte %d", id.Name, offset)
			continue
		}
		checked++
		if id.Name == "x" {
			xBindings = append(xBindings, struct {
				expected *types.Scope
				actual   string
			}{object.Parent(), actual.scope})
		}
	}
	if checked < 10 || len(xBindings) != 2 {
		t.Fatalf("oracle did not cover expected bindings: %d x=%d", checked, len(xBindings))
	}
	if (xBindings[0].expected == xBindings[1].expected) != (xBindings[0].actual == xBindings[1].actual) {
		t.Fatal("lexical shadow scopes disagree with type oracle")
	}
}

type modelSymbol struct{ name, scope string }
