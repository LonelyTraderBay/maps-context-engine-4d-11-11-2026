package golang

import (
	"go/ast"
	"go/types"
)

// StdlibCallTarget is limited to direct calls or selected concrete methods
// in the guarded compiler's pinned standard library. It is never synthesized
// from a selector spelling or function-value variable.
type StdlibCallTarget struct {
	Package  string `json:"package"`
	Receiver string `json:"receiver"`
	Name     string `json:"name"`
}

func stdlibCallTarget(info *types.Info, expression ast.Expr) *StdlibCallTarget {
	callable := ast.Unparen(expression)
	var object types.Object
	switch callee := callable.(type) {
	case *ast.Ident:
		object = info.Uses[callee]
	case *ast.SelectorExpr:
		if s := info.Selections[callee]; s != nil && (s.Kind() != types.MethodVal || len(s.Index()) != 1) {
			return nil
		}
		object = info.Uses[callee.Sel]
	}
	fn, ok := object.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return nil
	}
	pkg := fn.Pkg().Path()
	if pkg != "net/http" && pkg != "database/sql" {
		return nil
	}
	receiver := ""
	if recv := fn.Signature().Recv(); recv != nil {
		t := types.Unalias(recv.Type())
		if ptr, ok := t.(*types.Pointer); ok {
			t = types.Unalias(ptr.Elem())
		}
		named, ok := t.(*types.Named)
		if !ok || named.Obj().Pkg() != fn.Pkg() {
			return nil
		}
		receiver = named.Obj().Name()
	}
	return &StdlibCallTarget{Package: pkg, Receiver: receiver, Name: fn.Name()}
}
