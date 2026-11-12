package preflight

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/tools/go/packages"
	_ "modernc.org/sqlite"
)

func TestSQLiteFTSForeignKeysAndRollback(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	var version string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil || version != "3.54.1" {
		t.Fatalf("SQLite version=%q err=%v", version, err)
	}
	for _, query := range []string{
		"PRAGMA foreign_keys = ON",
		"CREATE TABLE parent(id INTEGER PRIMARY KEY)",
		"CREATE TABLE child(parent_id INTEGER REFERENCES parent(id))",
		"CREATE VIRTUAL TABLE documents USING fts5(body)",
		"INSERT INTO documents(body) VALUES ('context evidence')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	if _, err := db.Exec("INSERT INTO child(parent_id) VALUES (1)"); err == nil {
		t.Fatal("foreign key violation was accepted")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM documents WHERE documents MATCH ?", "evidence").Scan(&count); err != nil || count != 1 {
		t.Fatalf("FTS5 match count=%d err=%v", count, err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO parent(id) VALUES (1)"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM parent").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
}

func TestMCPJSONRPCTransportRoundTrip(t *testing.T) {
	type input struct {
		Value string `json:"value"`
	}
	type output struct {
		Value string `json:"value"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := mcp.NewServer(&mcp.Implementation{Name: "preflight-server", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo"}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, output, error) {
		return nil, output{Value: in.Value}, nil
	})
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	serverTransport := &mcp.IOTransport{Reader: serverReader, Writer: serverWriter}
	clientTransport := &mcp.IOTransport{Reader: clientReader, Writer: clientWriter}
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "preflight-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"value": "fixture"}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("call result=%v err=%v", result, err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var got output
	if err := json.Unmarshal(data, &got); err != nil || got.Value != "fixture" {
		t.Fatalf("structured output=%s err=%v", data, err)
	}
	missing, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "missing", Arguments: map[string]any{}})
	if err == nil && (missing == nil || !missing.IsError) {
		t.Fatalf("unknown tool unexpectedly succeeded: %v", missing)
	}
}

func TestGoPackagesTypedFixture(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod": "module example.test/fixture\n\ngo 1.27.4\n",
		"x.go":   "package fixture\nvar Message string\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	loaded, err := packages.Load(&packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
	}, ".")
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load count=%d err=%v", len(loaded), err)
	}
	pkg := loaded[0]
	if len(pkg.Errors) != 0 || pkg.Types == nil || pkg.TypesInfo == nil {
		t.Fatalf("partial package: %v", pkg.Errors)
	}
	if object := pkg.Types.Scope().Lookup("Message"); object == nil || object.Type().String() != "string" {
		t.Fatalf("unexpected Message object: %v", object)
	}
}

func TestTestOnlyLicenseExceptionNotLinkedIntoCLI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "./cmd/mce4d")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOPACKAGESDRIVER=off", "GOFLAGS=-mod=readonly")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list CLI dependencies: %v: %s", err, data)
	}
	for _, dependency := range strings.Split(string(data), "\n") {
		if dependency == "github.com/hashicorp/golang-lru/v2" || strings.HasPrefix(dependency, "github.com/hashicorp/golang-lru/v2/") {
			t.Fatalf("test-only license exception is linked into CLI: %s", dependency)
		}
	}
}
