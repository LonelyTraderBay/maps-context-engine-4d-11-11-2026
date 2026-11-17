//go:build linux

package lsp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type continuationFixture struct {
	StoreRoot, Workspace, Token, Cursor, Principal, Digest, ScopeDigest string
	Hashes                                                              map[string]string
	Expected                                                            []model.SourceRef
	Next                                                                int
}

func manifestFixtureGuard(workspace string, hashes map[string]string) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for path, expected := range hashes {
			data, err := os.ReadFile(filepath.Join(workspace, path))
			if err != nil {
				return ErrLSPAuthority
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != expected {
				return ErrLSPAuthority
			}
		}
		return nil
	}
}

func resultFixtureGuard(check func(context.Context) error, scopeDigest string) ResultGuard {
	return func(ctx context.Context, result RenameStoredResult) error {
		if result.Header.Scope.VectorDigest != scopeDigest {
			return ErrLSPAuthority
		}
		return check(ctx)
	}
}

func TestRenameContinuationChild(t *testing.T) {
	if os.Getenv("MCE4D_RENAME_CONTINUATION_FIXTURE") != "1" {
		return
	}
	var input continuationFixture
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		t.Fatal(err)
	}
	store, _ := makeResultStore(t, input.StoreRoot, 0)
	codec, _ := contextpack.NewCursorCodec([32]byte{8})
	guard := resultFixtureGuard(manifestFixtureGuard(input.Workspace, input.Hashes), input.ScopeDigest)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cursor := input.Cursor
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("continuation loop")
		}
		max := int64(65536)
		response, _, err := RenameContinue(ctx, store, codec, model.RenameContinueRequest{ResultToken: input.Token, Cursor: cursor, MaxOutputBytes: &max}, input.Principal, resultCursorAllow, guard, RenameWireOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", EffectiveMaxOutput: 131072, DeliveryMode: "cli", MaxEdits: 128})
		if err != nil {
			t.Fatal(err)
		}
		page, ok := response.Data.(model.RenameData)
		if !ok || page.ResultDigest != input.Digest || page.Applied || !page.ValidationRequired {
			t.Fatal("wrong continuation result")
		}
		for _, edit := range page.Edits {
			if input.Next >= len(input.Expected) || edit.SourceRef != input.Expected[input.Next] || edit.NewText != "Renamed" {
				t.Fatal("missing, changed or repeated edit")
			}
			input.Next++
		}
		if response.Pagination.NextCursor == nil {
			if !page.PreviewComplete || input.Next != len(input.Expected) || !hasContractGap(page.Conflicts, "IMPLICIT_SERIALIZATION_NAME_CHANGE") {
				t.Fatal("incomplete final page or missing field boundary")
			}
			out, _ := json.Marshal(map[string]int{"edits": input.Next, "pages": pages + 1})
			_, _ = os.Stdout.Write(out)
			os.Exit(0)
		}
		if page.PreviewComplete || *response.Pagination.NextCursor == cursor {
			t.Fatal("invalid continuation progress")
		}
		cursor = *response.Pagination.NextCursor
	}
}

func TestGoplsRenameSpoolCrossProcessFixture(t *testing.T) {
	binary, goRoot := os.Getenv("MCE4D_GOPLS_FIXTURE_BINARY"), os.Getenv("MCE4D_GOPLS_FIXTURE_GOROOT")
	if binary == "" || goRoot == "" {
		t.Skip("reviewed fixture gopls and toolchain not supplied")
	}
	serverRoot, err := paths.OpenRoot(filepath.Dir(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer serverRoot.Close()
	scratch := t.TempDir()
	for _, name := range []string{"input", "home", "gopath", "modcache", "cache", "tmp"} {
		if err := os.Mkdir(filepath.Join(scratch, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(scratch, "input")
	a := "package demo\ntype Thing struct { Value int }\n"
	var builder strings.Builder
	builder.WriteString("package demo\nfunc uses(t Thing) {\n")
	for i := 0; i < 1024; i++ {
		builder.WriteString("_ = t.Value\n")
	}
	builder.WriteString("}\n")
	b := builder.String()
	files := map[string]string{"go.mod": "module example.invalid/fixture\n\ngo 1.27\n", "a.go": a, "b.go": b}
	hashes := make(map[string]string, len(files))
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(text))
		hashes[name] = hex.EncodeToString(sum[:])
	}
	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, 2)
	for _, name := range []string{"a.go", "b.go"} {
		file, err := parser.ParseFile(fset, name, files[name], 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, file)
	}
	if _, err := (&types.Config{GoVersion: "go1.27"}).Check("example.invalid/fixture", fset, parsed, nil); err != nil {
		t.Fatal(err)
	}
	scope, _, principal := resultFixture(t, 0)
	version := int64(1)
	docs := make([]CapturedDocument, 0, 2)
	want := make([]model.SourceRef, 0, 1025)
	for _, name := range []string{"a.go", "b.go"} {
		file, err := snapshot.NewFile(name, []byte(files[name]), 65536)
		if err != nil {
			t.Fatal(err)
		}
		ref := model.SourceRef{WorktreeID: fixtureWorktree, ProfileID: strings.Repeat("a", 64), GenerationID: fixtureGeneration, Path: name, ContentHash: file.ContentHash()}
		docs = append(docs, CapturedDocument{File: file, Ref: ref, Version: &version})
		for at := 0; ; {
			index := strings.Index(files[name][at:], "Value")
			if index < 0 {
				break
			}
			at += index
			item := ref
			item.Range = model.ByteRange{StartByte: int64(at), EndByte: int64(at + 5)}
			want = append(want, item)
			at += 5
		}
	}
	if len(want) != 1025 {
		t.Fatal("bad fixture oracle")
	}
	storeDir := resultFixtureDir(t)
	store, _ := makeResultStore(t, storeDir, 0)
	codec, _ := contextpack.NewCursorCodec([32]byte{8})
	allowed := true
	checkManifest := manifestFixtureGuard(workspace, hashes)
	check := func(ctx context.Context) error {
		if !allowed {
			return ErrLSPAuthority
		}
		return checkManifest(ctx)
	}
	resultGuard := resultFixtureGuard(check, scope.VectorDigest)
	cursorGuard := func(ctx context.Context, b contextpack.CursorBinding) error {
		if !allowed || b.PrincipalDigest != principal || b.Scope.VectorDigest != scope.VectorDigest {
			return ErrLSPAuthority
		}
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	limiter, err := NewSessionLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	max := int64(32768)
	request := model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: hashes["a.go"], ByteOffset: int64(strings.Index(a, "Value"))}, NewName: "Renamed", PreserveContracts: true, MaxOutputBytes: &max}
	response, _, err := RenameInitial(ctx, RenameInitialInput{Session: SessionConfig{ServerBinRoot: serverRoot, ExpectedSHA256: "b94804c7a4f74bcf0765263cb022b78459fc61258fe30c67607106af96fa5029", Environment: runtime.GoEnvironmentInput{GoRoot: goRoot, ScratchRoot: scratch, WorkspaceRoot: workspace, GOOS: "linux", GOARCH: "amd64", BuildTags: []string{}}, Documents: docs, Check: check, Limiter: limiter}, Scope: scope, PrincipalID: "fixture-principal", PrincipalDigest: principal, BindingDigest: strings.Repeat("c", 64), ProviderDigest: strings.Repeat("d", 64), Request: request, Store: store, Codec: codec, CursorGuard: cursorGuard, ResultGuard: resultGuard, Wire: RenameWireOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", EffectiveMaxOutput: 131072, DeliveryMode: "cli", MaxEdits: 128}})
	if err != nil {
		t.Fatal(err)
	}
	page := response.Data.(model.RenameData)
	if page.PreviewComplete || response.Pagination.NextCursor == nil || len(page.Edits) == 0 {
		t.Fatal("first page falsely complete")
	}
	for i, edit := range page.Edits {
		if edit.SourceRef != want[i] {
			t.Fatal("first page order")
		}
	}
	child := continuationFixture{StoreRoot: storeDir, Workspace: workspace, Token: page.ResultToken, Cursor: *response.Pagination.NextCursor, Principal: principal, Digest: page.ResultDigest, ScopeDigest: scope.VectorDigest, Hashes: hashes, Expected: want, Next: len(page.Edits)}
	input, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRenameContinuationChild$")
	cmd.Env = []string{"MCE4D_RENAME_CONTINUATION_FIXTURE=1"}
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cross-process continuation: %v: %s", err, output)
	}
	var count map[string]int
	if json.Unmarshal(output, &count) != nil || count["edits"] != 1025 || count["pages"] < 1 {
		t.Fatalf("bad continuation result: %s", output)
	}
	if _, err := store.ValidateResultToken(ctx, codec, page.ResultToken, principal, cursorGuard, resultGuard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "b.go"), []byte(b+"// changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateResultToken(ctx, codec, page.ResultToken, principal, cursorGuard, resultGuard); err == nil {
		t.Fatal("stale workspace was accepted")
	}
	if err := os.WriteFile(filepath.Join(workspace, "b.go"), []byte(b), 0600); err != nil {
		t.Fatal(err)
	}
	allowed = false
	if _, err := store.ValidateResultToken(ctx, codec, page.ResultToken, principal, cursorGuard, resultGuard); !errors.Is(err, ErrLSPAuthority) {
		t.Fatalf("revoked authority accepted: %v", err)
	}
	if err := checkManifest(ctx); err != nil {
		t.Fatal(fmt.Errorf("fixture source not restored: %w", err))
	}
}
