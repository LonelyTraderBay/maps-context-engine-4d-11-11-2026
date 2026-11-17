//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	mceMCP "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/mcp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// This only runs against the test-owned synthetic Git repository and SQLite
// stores created by TestScopeReaderComposesRealStoresAndRejectsChanges.
func verifyMCPCanary(t *testing.T, parent context.Context, service *app.CoreService, repo string) {
	t.Helper()
	agents := filepath.Join(repo, "AGENTS.md")
	f, err := os.OpenFile(agents, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(f, "Fixture policy: do not modify this file.\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(agents); err != nil {
			t.Errorf("remove test-owned AGENTS fixture: %v", err)
		}
	})
	tree := func() [32]byte {
		t.Helper()
		h := sha256.New()
		err := filepath.WalkDir(repo, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsupported canary entry")
			}
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%q %o ", rel, info.Mode())
			if info.Mode().IsRegular() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				fmt.Fprintf(h, "%x", sha256.Sum256(data))
			} else if !info.IsDir() {
				return fmt.Errorf("unsupported canary file type")
			}
			fmt.Fprintln(h)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var digest [32]byte
		copy(digest[:], h.Sum(nil))
		return digest
	}
	before := tree()
	server, err := mceMCP.New(service)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, sr, sw) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "published-store-fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.IOTransport{Reader: cr, Writer: cw, MaxLineLength: 1024 * 1024}, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	closed := false
	closeSession := func() {
		if closed {
			return
		}
		closed = true
		_ = session.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("MCP server did not terminate")
		}
	}
	defer closeSession()
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	call := func(name string, args any) (model.Response, bool) {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result == nil || len(result.Content) != 1 {
			t.Fatalf("%s failed: %v", name, err)
		}
		text, ok := result.Content[0].(*sdk.TextContent)
		if !ok {
			t.Fatal("missing text representation")
		}
		structured, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		response, err := registry.DecodeResponse(contracts.MCP, name, structured, 131072, 4)
		if err != nil || model.ValidateDeliveredBytes(response.Budget, [][]byte{[]byte(text.Text), structured}) != nil || result.IsError != (response.Status == "error" || response.Status == "blocked") {
			t.Fatalf("%s invalid MCP envelope/accounting: %v", name, err)
		}
		return response, result.IsError
	}
	status, failed := call("mce4d_workspace_status", map[string]any{})
	if failed || status.Data.(model.StatusData).IndexState != "published" {
		t.Fatal("published status unavailable")
	}
	located, failed := call("mce4d_locate", map[string]any{"query": "Example"})
	if failed || len(located.Data.(model.LocateData).Candidates) == 0 {
		t.Fatal("published lookup failed")
	}
	pack, failed := call("mce4d_context_pack", map[string]any{"selector": map[string]any{"kind": "symbol", "symbol_id": "target-main"}, "task_kind": "explore", "max_output_bytes": 16384})
	if failed || pack.Pagination.NextCursor == nil {
		t.Fatal("published pack did not provide its expected continuation")
	}
	token := pack.Data.(model.PackData).PackToken
	expanded, failed := call("mce4d_expand_pack", map[string]any{"pack_token": token, "next_cursor": *pack.Pagination.NextCursor, "max_output_bytes": 16384})
	if failed || expanded.Data.(model.PackData).PackToken != token {
		t.Fatal("published pack continuation failed")
	}
	validated, failed := call("mce4d_validate_pack", map[string]any{"pack_token": token})
	if !failed || validated.Error.Code != "FRESHNESS_UNKNOWN" {
		t.Fatal("missing workspace verifier did not fail closed")
	}
	rename, failed := call("mce4d_rename_preview", map[string]any{"result_token": "fixture", "cursor": "fixture"})
	if !failed || rename.Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatal("unimplemented rename did not fail closed")
	}
	closeSession()
	if after := tree(); before != after {
		t.Fatal("MCP calls changed the synthetic target tree or Git metadata")
	}
}
