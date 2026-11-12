package contract

import (
	"context"
	"crypto/sha256"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

func TestProviderFixtureLoadIdentityAndCapturedBytes(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "input.go")
	before := []byte("package fixture\ntype Item struct{}\n")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/isolated\n\ngo 1.27.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, before, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	load := func() *types.Package {
		t.Helper()
		p, err := packages.Load(&packages.Config{Context: ctx, Dir: dir, Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo}, ".")
		if err != nil || len(p) != 1 || len(p[0].Errors) != 0 || p[0].Types == nil {
			t.Fatalf("load: %v %v", p, err)
		}
		return p[0].Types
	}
	first := load()
	second := load()
	one := first.Scope().Lookup("Item")
	two := second.Scope().Lookup("Item")
	if one == nil || two == nil || one == two || types.Identical(one.Type(), two.Type()) {
		t.Fatal("separate typed loads were incorrectly treated as same identity")
	}
	capturedAST, err := parser.ParseFile(token.NewFileSet(), file, before, 0)
	if err != nil {
		t.Fatal(err)
	}
	if capturedAST.Name.Name != "fixture" {
		t.Fatal("unexpected captured source")
	}
	after := []byte("package fixture\ntype Changed struct{}\n")
	if err = os.WriteFile(file, after, 0600); err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) == sha256.Sum256(after) || load().Scope().Lookup("Item") != nil {
		t.Fatal("changed source not detected")
	}
}
