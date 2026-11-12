package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// This guard detects the explicitly forbidden mutable globals by name. It does
// not prove that all possible aliases or indirect mutable state are absent.
func forbiddenGlobals(file *ast.File) []string {
	var bad []string
	for _, decl := range file.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok || g.Tok != token.VAR {
			continue
		}
		for _, spec := range g.Specs {
			v, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range v.Names {
				n := strings.ToLower(strings.ReplaceAll(name.Name, "_", ""))
				if n == "currentrepo" || n == "currentsession" {
					bad = append(bad, name.Name)
				}
			}
		}
	}
	return bad
}
func TestNoNamedMutableCurrentRepoOrSession(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, name := range forbiddenGlobals(f) {
			t.Errorf("%s: forbidden global %s", path, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestNamedGlobalGuardDetectsVars(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture; var currentRepo string; var current_session int; func f(){var currentRepo string; _ = currentRepo}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(forbiddenGlobals(f)) != 2 {
		t.Fatal("failed to detect forbidden globals")
	}
}
