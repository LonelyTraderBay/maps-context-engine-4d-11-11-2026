package contract

import (
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modelImport = "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"

// These layers use Go's standard library; ports may also depend on model.
func allowedBoundaryImport(layer, path string) bool {
	if isForbiddenModelFacility(path) {
		return false
	}
	if layer == "ports" && path == modelImport {
		return true
	}
	pkg, err := build.Default.Import(path, "", build.FindOnly)
	return err == nil && pkg.Goroot
}
func TestModelAndPortsImportBoundary(t *testing.T) {
	for _, layer := range []string{"model", "ports"} {
		dir := filepath.Join("..", "..", "internal", layer)
		files, err := os.ReadDir(dir)
		if os.IsNotExist(err) && layer == "ports" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, file.Name())
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range parsed.Imports {
				name, err := strconv.Unquote(item.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if !allowedBoundaryImport(layer, name) {
					t.Errorf("%s imports disallowed dependency %s", path, name)
				}
			}
		}
	}
}
func TestBoundaryRuleExamples(t *testing.T) {
	// Some packages live in the standard library but are forbidden by the model contract.
	for _, name := range []string{"database/sql", "database/sql/driver", "net/http", "net/http/httptest", "os/exec"} {
		if allowedBoundaryImport("model", name) {
			t.Errorf("allowed %s", name)
		}
	}
	for _, name := range []string{"modernc.org/sqlite", "github.com/modelcontextprotocol/go-sdk/mcp", "example/internal/store/sqlite", "example/internal/app/catalog"} {
		if allowedBoundaryImport("model", name) {
			t.Errorf("allowed %s", name)
		}
	}
	if !allowedBoundaryImport("ports", modelImport) || allowedBoundaryImport("model", modelImport) || !allowedBoundaryImport("model", "encoding/json") {
		t.Fatal("unexpected model or stdlib boundary")
	}
}
func isForbiddenModelFacility(path string) bool {
	return path == "database/sql" || strings.HasPrefix(path, "database/sql/") || path == "net/http" || strings.HasPrefix(path, "net/http/") || path == "os/exec"
}
