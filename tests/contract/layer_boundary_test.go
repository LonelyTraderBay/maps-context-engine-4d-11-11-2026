package contract

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026"

func matchesPackage(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
func blockedLayerImport(layer, path string) bool {
	sql := matchesPackage(path, "database/sql") || matchesPackage(path, "modernc.org/sqlite") || matchesPackage(path, modulePath+"/internal/store")
	if layer == "app" {
		return sql || matchesPackage(path, "net/http") || matchesPackage(path, "os/exec") || matchesPackage(path, modulePath+"/internal/analysis/providers") || matchesPackage(path, modulePath+"/internal/cli") || matchesPackage(path, modulePath+"/internal/mcp") || matchesPackage(path, modulePath+"/internal/explorer")
	}
	if layer == "store" {
		return matchesPackage(path, modulePath+"/internal/app") || matchesPackage(path, modulePath+"/internal/cli") || matchesPackage(path, modulePath+"/internal/mcp") || matchesPackage(path, modulePath+"/internal/explorer")
	}
	if layer == "cli" || layer == "mcp" || layer == "explorer" {
		return sql || matchesPackage(path, "go/parser") || matchesPackage(path, "go/ast") || matchesPackage(path, "go/types") || matchesPackage(path, "go/token")
	}
	return false
}
func TestAdapterAndApplicationImportBoundaries(t *testing.T) {
	for _, layer := range []string{"app", "store", "cli", "mcp", "explorer"} {
		root := filepath.Join("..", "..", "internal", layer)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) && path == root {
				return fs.SkipDir
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				name, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				if blockedLayerImport(layer, name) {
					t.Errorf("%s imports forbidden layer %s", path, name)
				}
			}
			return nil
		})
		if err != nil && err != fs.SkipDir {
			t.Fatal(err)
		}
	}
}
func TestBoundaryDisallowsDirectAdapters(t *testing.T) {
	for _, tc := range []struct{ layer, path string }{{"app", modulePath + "/internal/store/sqlite"}, {"app", "database/sql"}, {"app", "net/http"}, {"app", modulePath + "/internal/analysis/providers"}, {"app", modulePath + "/internal/explorer"}, {"store", modulePath + "/internal/app"}, {"explorer", "go/parser"}, {"mcp", modulePath + "/internal/store/sqlite"}, {"cli", "modernc.org/sqlite"}} {
		if !blockedLayerImport(tc.layer, tc.path) {
			t.Errorf("allowed %s -> %s", tc.layer, tc.path)
		}
	}
	if blockedLayerImport("store", modulePath+"/internal/model") || blockedLayerImport("mcp", modulePath+"/internal/app") {
		t.Fatal("blocked intended layer dependency")
	}
}
