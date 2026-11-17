//go:build linux

package compose

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	mceMCP "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/mcp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Uses the real catalog/index/records, Linux spool, gopls and wire adapters.
// Binary/toolchain and growth reservations here are explicitly fixture owners;
// this does not prove a native installation's typed closure or total quota.
func verifyRetainedRenameCanary(t *testing.T, parent context.Context, base *CoreBackend, config *app.RuntimeConfigService, p app.CorePrincipal, scope model.ScopeVector, captured *snapshot.Snapshot, stateRoot *paths.Root, lease *runtime.Lifecycle, stateDir, repo string) {
	t.Helper()
	binary, goRoot := os.Getenv("MCE4D_GOPLS_FIXTURE_BINARY"), os.Getenv("MCE4D_GOPLS_FIXTURE_GOROOT")
	if binary == "" || goRoot == "" {
		t.Skip("reviewed fixture gopls/toolchain not supplied")
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	serverRoot := scopeRoot(t, filepath.Dir(binary))
	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	provider := hex.EncodeToString(sum[:])
	if provider != "b94804c7a4f74bcf0765263cb022b78459fc61258fe30c67607106af96fa5029" {
		t.Fatal("unreviewed fixture provider")
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "scratch", "rename"), 0700); err != nil {
		t.Fatal(err)
	}
	scanner, _ := policy.NewScanner(32 << 20)
	allowed := true
	check := func(ctx context.Context, actor app.CorePrincipal, asked model.ScopeVector) error {
		if !allowed || actor != p || model.ComparePinnedScope(scope, asked, 65536) != nil {
			return app.ErrValidationAuthority
		}
		_, err := base.Reader.Recheck(ctx, p, scope)
		return err
	}
	store, err := lsp.NewBudgetedResultStore(stateRoot, lease, scope.Members[0].WorktreeID, scanner, 2<<20, 4<<20, 900, 8,
		func(ctx context.Context) error { return check(ctx, p, scope) }, func(context.Context, int64) (packstore.BudgetReservation, error) {
			return scopeBudgetReservation{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	limiter, _ := lsp.NewSessionLimiter(1)
	opens, closes := 0, 0
	owner := RenameOwner{Store: store, ProviderDigest: provider, Check: check}
	owner.Open = func(ctx context.Context, actor app.CorePrincipal, asked model.ScopeVector, request model.RenameInitialRequest, snap *snapshot.Snapshot) (lsp.SessionConfig, func() error, error) {
		opens++
		if err := check(ctx, actor, asked); err != nil {
			return lsp.SessionConfig{}, nil, err
		}
		if snap.Manifest().SnapshotDigest != captured.Manifest().SnapshotDigest {
			return lsp.SessionConfig{}, nil, app.ErrValidationPrecondition
		}
		scratch := t.TempDir()
		for _, name := range []string{"input", "home", "gopath", "modcache", "cache", "tmp"} {
			if err := os.Mkdir(filepath.Join(scratch, name), 0700); err != nil {
				return lsp.SessionConfig{}, nil, err
			}
		}
		workspace := filepath.Join(scratch, "input")
		documents := []lsp.CapturedDocument{}
		parsed := []*ast.File{}
		fset := token.NewFileSet()
		version := int64(1)
		for _, item := range snap.Manifest().Files {
			file, ok := snap.File(item.Path)
			if !ok {
				return lsp.SessionConfig{}, nil, app.ErrValidationPrecondition
			}
			if err := os.WriteFile(filepath.Join(workspace, item.Path), file.Bytes(), 0600); err != nil {
				return lsp.SessionConfig{}, nil, err
			}
			if strings.HasSuffix(item.Path, ".go") {
				astFile, err := parser.ParseFile(fset, item.Path, file.Bytes(), 0)
				if err != nil {
					return lsp.SessionConfig{}, nil, err
				}
				if len(astFile.Imports) != 0 {
					return lsp.SessionConfig{}, nil, fmt.Errorf("fixture only proves no-import closure")
				}
				parsed = append(parsed, astFile)
				m := scope.Members[0]
				documents = append(documents, lsp.CapturedDocument{File: file, Ref: model.SourceRef{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: m.GenerationID, Path: item.Path, ContentHash: file.ContentHash()}, Version: &version})
			}
		}
		if _, err := (&types.Config{GoVersion: "go1.27"}).Check("example.invalid/fixture", fset, parsed, nil); err != nil {
			return lsp.SessionConfig{}, nil, err
		}
		guard := func(ctx context.Context) error {
			if err := check(ctx, actor, asked); err != nil {
				return err
			}
			for _, item := range snap.Manifest().Files {
				file, _ := snap.File(item.Path)
				got, err := os.ReadFile(filepath.Join(workspace, item.Path))
				if err != nil || !bytes.Equal(got, file.Bytes()) {
					return lsp.ErrLSPAuthority
				}
			}
			state, err := base.verifyWorkspace(ctx, p, scope.Members[0], snap)
			if err != nil {
				return err
			}
			if state != app.WorkspaceObservedCurrent {
				return app.ErrFreshnessChanged
			}
			return nil
		}
		return lsp.SessionConfig{ServerBinRoot: serverRoot, ExpectedSHA256: provider, Environment: runtime.GoEnvironmentInput{GoRoot: goRoot, ScratchRoot: scratch, WorkspaceRoot: workspace, GOOS: "linux", GOARCH: "amd64", BuildTags: []string{}}, Documents: documents, Check: guard, Limiter: limiter}, func() error { closes++; return nil }, nil
	}
	backend, err := base.WithRenamePreview(owner)
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := contracts.LoadRegistry()
	deps := cli.Dependencies{CPUs: 4, OpenRetainedCore: func(context.Context, string, string, string) (*app.CoreService, func() error, error) {
		// A distinct backend/service on every call cannot rely on its RAM cache.
		fresh := *backend
		service, err := app.NewCoreService(config, base.Authority, &fresh, nil)
		return service, func() error { return nil }, err
	}}
	query := func(action string, args any) (model.Response, int) {
		t.Helper()
		var out, diagnostic bytes.Buffer
		code := cli.Run(ctx, []string{"query", action, "--input-json", "-"}, io.NopCloser(bytes.NewReader(scopeJSON(t, args))), &out, &diagnostic, deps)
		name := "mce4d_rename_preview"
		if action == "validate" {
			name = "mce4d_validate_pack"
		}
		response, err := registry.DecodeResponse(contracts.MCP, name, out.Bytes(), 131072, 4)
		if err != nil || diagnostic.Len() != 0 {
			t.Fatalf("CLI %s: exit%d %v %s", action, code, err, diagnostic.String())
		}
		return response, code
	}
	file, _ := captured.File("main.go")
	request := &model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: "main.go", ContentHash: file.ContentHash(), ByteOffset: int64(bytes.Index(file.Bytes(), []byte("Example")))}, NewName: "Renamed", PreserveContracts: true}
	response, code := query("rename-preview", request)
	data, ok := response.Data.(model.RenameData)
	if code != 6 || !ok || data.PreviewComplete || data.TotalEdits != 65 || data.Applied || data.SourceMutationCount != 0 || !data.ValidationRequired || opens != 1 || closes != 1 {
		t.Fatalf("CLI initial: %+v %d opens%d closes%d", response, code, opens, closes)
	}
	firstCursor := *response.Pagination.NextCursor
	edits, conflicts := append([]model.ProposedEdit(nil), data.Edits...), append([]model.Gap{}, data.Conflicts...)
	seen := map[string]bool{}
	pages := 1
	childPages := 0
	baseDir := filepath.Dir(repo)
	var nonceBytes [32]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(nonceBytes[:])
	marker := filepath.Join(baseDir, ".retained-cli-rename-fixture")
	markerFile, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(markerFile, nonce); err != nil {
		_ = markerFile.Close()
		t.Fatal(err)
	}
	if err := markerFile.Close(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(marker); err != nil {
			t.Error(err)
		}
	}()
	for response.Pagination.NextCursor != nil {
		cursor := *response.Pagination.NextCursor
		if seen[cursor] || pages > 128 {
			t.Fatal("CLI cursor loop")
		}
		seen[cursor] = true
		continuation := &model.RenameContinueRequest{ResultToken: data.ResultToken, Cursor: cursor}
		if pages%2 == 1 {
			response, code = runRetainedCLIProcess(t, ctx, baseDir, nonce, scope.Members[0].WorktreeID, scope.Members[0].ProfileID, "rename-preview", scopeJSON(t, continuation), nil)
			childPages++
		} else {
			response, code = query("rename-preview", continuation)
		}
		page, ok := response.Data.(model.RenameData)
		if (code != 0 && code != 6) || !ok || page.ResultID != data.ResultID || page.ResultDigest != data.ResultDigest || page.ResultToken != data.ResultToken || page.TotalEdits != 65 || opens != 1 || closes != 1 {
			t.Fatalf("CLI continuation reran or mixed computation: %+v", response)
		}
		edits = append(edits, page.Edits...)
		conflicts = append(conflicts, page.Conflicts...)
		pages++
		if page.PreviewComplete != (response.Pagination.NextCursor == nil) {
			t.Fatal("premature preview completion")
		}
	}
	preconditions := []model.RenameFilePrecondition{{WorktreeID: scope.Members[0].WorktreeID, ProfileID: scope.Members[0].ProfileID, GenerationID: scope.Members[0].GenerationID, Path: "main.go", ContentHash: file.ContentHash()}}
	digest, err := model.RenameResultDigest(scope, *request, provider, preconditions, edits, conflicts, 2<<20)
	if err != nil || digest != data.ResultDigest || len(edits) != 65 || pages < 2 || childPages < 1 {
		t.Fatalf("CLI complete result/digest: edits%d pages%d %v", len(edits), pages, err)
	}
	t.Logf("65 edits collected over %d CLI pages, %d in separate processes; provider opened once", pages, childPages)
	validated, code := query("validate", model.ValidateRequest{ResultToken: &data.ResultToken})
	valid, ok := validated.Data.(model.ValidateData)
	if code != 0 || !ok || !valid.EditPreconditionValidated || validated.Freshness.Status != "observed_current" || opens != 1 {
		t.Fatalf("CLI result validation: %+v %d", validated, code)
	}
	invalid := *request
	invalid.NewName = "bad name"
	invalidResponse, invalidCode := query("rename-preview", &invalid)
	if invalidCode != 2 || invalidResponse.Error == nil || invalidResponse.Error.Code != "INVALID_ARGUMENT" || opens != 1 || closes != 1 {
		t.Fatal("invalid Go name reached the provider")
	}
	// A result token is neither a pack token nor a page cursor.
	bad, code := query("validate", model.ValidateRequest{PackToken: &data.ResultToken})
	if code != 3 || bad.Error == nil {
		t.Fatal("cross-purpose token accepted")
	}
	allowed = false
	bad, code = query("validate", model.ValidateRequest{ResultToken: &data.ResultToken})
	if code != 3 || bad.Error == nil || bad.Error.Code != "ACCESS_DENIED" || opens != 1 {
		t.Fatalf("revoked result: %+v", bad)
	}
	allowed = true
	service, err := app.NewCoreService(config, base.Authority, backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The requested budget is lower than the effective policy ceiling. A
	// too-small dual envelope must report a budget error, not a result limit,
	// and may not rerun LSP to produce a different computation.
	small := int64(4096)
	budgetResult, err := service.Call(ctx, "mce4d_rename_preview", scopeJSON(t, &model.RenameContinueRequest{ResultToken: data.ResultToken, Cursor: firstCursor, MaxOutputBytes: &small}), app.CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 4096, DeliveryMode: "mcp_dual"})
	if err != nil || !budgetResult.IsError || len(budgetResult.Blocks) != 2 {
		t.Fatalf("tiny continuation budget: %v", err)
	}
	budgetResponse, err := registry.DecodeResponse(contracts.MCP, "mce4d_rename_preview", budgetResult.Blocks[0], 131072, 4)
	if err != nil || budgetResponse.Error == nil || budgetResponse.Error.Code != "BUDGET_TOO_SMALL" || opens != 1 {
		t.Fatalf("request budget was confused with policy limit: %+v %v", budgetResponse, err)
	}
	verifyMCPRenameCanary(t, ctx, service, request)
	if opens != 2 || closes != 2 {
		t.Fatalf("validation reran provider: opens%d closes%d", opens, closes)
	}
	for _, item := range captured.Manifest().Files {
		file, _ := captured.File(item.Path)
		got, err := os.ReadFile(filepath.Join(repo, item.Path))
		if err != nil || !bytes.Equal(got, file.Bytes()) {
			t.Fatal("preview mutated target")
		}
	}
	// Edit an unedited captured rule using the test controller, then restore it.
	document, _ := captured.File("design.md")
	if err := os.WriteFile(filepath.Join(repo, "design.md"), append(document.Bytes(), []byte("changed by test controller\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(filepath.Join(repo, "design.md"), document.Bytes(), 0600); err != nil {
			t.Error(err)
		}
	}()
	bad, code = query("validate", model.ValidateRequest{ResultToken: &data.ResultToken})
	if code != 3 || bad.Error == nil || bad.Error.Code != "SOURCE_PRECONDITION_FAILED" {
		t.Fatalf("unedited rule change accepted: %+v", bad)
	}
}

func verifyMCPRenameCanary(t *testing.T, ctx context.Context, service *app.CoreService, request *model.RenameInitialRequest) {
	t.Helper()
	server, err := mceMCP.New(service)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, sr, sw) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "rename-preview-fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.IOTransport{Reader: cr, Writer: cw, MaxLineLength: 1 << 20}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = session.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("MCP did not stop")
		}
	}()
	registry, _ := contracts.LoadRegistry()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 6 {
		t.Fatal("sealed six-tool registry changed")
	}
	call := func(name string, args any) model.Response {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError || len(result.Content) != 1 {
			t.Fatalf("MCP call %s: %+v %v", name, result, err)
		}
		text, ok := result.Content[0].(*sdk.TextContent)
		if !ok {
			t.Fatal("MCP text missing")
		}
		structured, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if json.Unmarshal([]byte(text.Text), &a) != nil || json.Unmarshal(structured, &b) != nil || !bytes.Equal(scopeJSON(t, a), scopeJSON(t, b)) {
			t.Fatal("MCP text/structured mismatch")
		}
		response, err := registry.DecodeResponse(contracts.MCP, name, []byte(text.Text), 131072, 4)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	preview := call("mce4d_rename_preview", request)
	data, ok := preview.Data.(model.RenameData)
	if !ok || data.TotalEdits != 65 || data.PreviewComplete || data.Applied || data.SourceMutationCount != 0 {
		t.Fatal("MCP preview incomplete or applied")
	}
	count, pages := len(data.Edits), 1
	seen := map[string]bool{}
	for preview.Pagination.NextCursor != nil {
		cursor := *preview.Pagination.NextCursor
		if seen[cursor] || pages > 128 {
			t.Fatal("MCP cursor loop")
		}
		seen[cursor] = true
		preview = call("mce4d_rename_preview", &model.RenameContinueRequest{ResultToken: data.ResultToken, Cursor: cursor})
		page, ok := preview.Data.(model.RenameData)
		if !ok || page.ResultID != data.ResultID || page.ResultDigest != data.ResultDigest || page.ResultToken != data.ResultToken || page.PreviewComplete != (preview.Pagination.NextCursor == nil) {
			t.Fatal("MCP mixed or incomplete result")
		}
		count += len(page.Edits)
		pages++
	}
	if count != 65 || pages < 2 {
		t.Fatal("MCP lost edits or pagination")
	}
	validation := call("mce4d_validate_pack", model.ValidateRequest{ResultToken: &data.ResultToken})
	if !validation.Data.(model.ValidateData).EditPreconditionValidated || validation.Freshness.Status != "observed_current" {
		t.Fatal("MCP result validation failed")
	}
}
