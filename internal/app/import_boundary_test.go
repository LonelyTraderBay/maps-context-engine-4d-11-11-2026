package app

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Application use cases depend on ports rather than concrete filesystem or
// storage adapters. Test files may compose real adapters as fixtures.
func TestApplicationDoesNotImportConcreteAdapters(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range file.Imports {
			path, err := strconv.Unquote(item.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"database/sql", "/internal/runtime", "/internal/paths", "/internal/catalog", "/internal/store/"} {
				if path == forbidden || strings.Contains(path, forbidden) {
					t.Errorf("%s imports concrete adapter %s", name, path)
				}
			}
		}
	}
}
