//go:build linux

package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestServerVerifierRejectsNonGoplsEvenWithMatchingHash(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := verifyGoplsFile(ctx, file, hex.EncodeToString(sum[:])); !errors.Is(err, ErrLSPAuthority) {
		t.Fatalf("accepted non-gopls executable: %v", err)
	}
}

func TestServerVerifierRejectsReplacedPath(t *testing.T) {
	binary := os.Getenv("MCE4D_GOPLS_FIXTURE_BINARY")
	if binary == "" {
		t.Skip("reviewed fixture gopls not supplied")
	}
	dir := t.TempDir()
	source, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	path := filepath.Join(dir, "gopls")
	copyFile, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(copyFile, source); err != nil {
		t.Fatal(err)
	}
	if err = copyFile.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	pinned, err := root.OpenRegular("gopls")
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hash := "b94804c7a4f74bcf0765263cb022b78459fc61258fe30c67607106af96fa5029"
	if err := verifyServerBinding(ctx, root, pinned, hash); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(replacement, source); err != nil {
		t.Fatal(err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyServerBinding(ctx, root, pinned, hash); !errors.Is(err, ErrLSPAuthority) {
		t.Fatalf("same-bytes replacement inode accepted: %v", err)
	}
}

func TestPreparedRenameRangeMustContainExactPosition(t *testing.T) {
	in := workspaceEditFixture(t)
	doc := in.Documents[0]
	raw := jsonRange(`{"start":{"line":0,"character":1},"end":{"line":0,"character":3}}`)
	if !validPreparedRange(raw, doc, snapshot.UTF16, 1) || !validPreparedRange(raw, doc, snapshot.UTF16, 4) || validPreparedRange(raw, doc, snapshot.UTF16, 5) {
		t.Fatal("prepared range did not contain exact byte position")
	}
	if validPreparedRange(jsonRange(`{"start":{"line":0,"character":2},"end":{"line":0,"character":3}}`), doc, snapshot.UTF16, 1) || validPreparedRange(jsonRange(`{"defaultBehavior":true}`), doc, snapshot.UTF16, 1) {
		t.Fatal("accepted invalid prepare result")
	}
}

func jsonRange(value string) []byte { return []byte(value) }

// This opt-in test uses only a freshly created fixture. The default test suite
// does not download, locate or execute any external language server.
func TestGoplsCapturedRenameFixture(t *testing.T) {
	binary, goRoot := os.Getenv("MCE4D_GOPLS_FIXTURE_BINARY"), os.Getenv("MCE4D_GOPLS_FIXTURE_GOROOT")
	if binary == "" || goRoot == "" {
		t.Skip("reviewed fixture gopls and toolchain not supplied")
	}
	if filepath.Base(binary) != "gopls" {
		t.Fatal("fixture executable name")
	}
	root, err := paths.OpenRoot(filepath.Dir(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	scratch := t.TempDir()
	for _, dir := range []string{"input", "home", "gopath", "modcache", "cache", "tmp"} {
		if err := os.Mkdir(filepath.Join(scratch, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(scratch, "input")
	a := "package demo\r\n// Tên ພາສາ 😀\r\ntype Inner struct { Value int }\r\ntype Outer struct { Inner }\r\n"
	b := "package demo\nvar object = Outer{Value: 1}\nfunc Read() int { return object.Value }\nvar _ = Inner{Value: 2}\n"
	files := map[string]string{"go.mod": "module example.invalid/fixture\n\ngo 1.27\n", "a.go": a, "b.go": b}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Independently check that the synthetic package is well typed.
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
	id := "01234567-89ab-cdef-0123-456789abcdef"
	version := int64(1)
	docs := make([]CapturedDocument, 0, 2)
	for _, name := range []string{"a.go", "b.go"} {
		file, err := snapshot.NewFile(name, []byte(files[name]), 65536)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, CapturedDocument{File: file, Ref: model.SourceRef{WorktreeID: id, ProfileID: strings.Repeat("a", 64), GenerationID: id, Path: name, ContentHash: file.ContentHash()}, Version: &version})
	}
	checks := 0
	guard := func(ctx context.Context) error {
		checks++
		if err := ctx.Err(); err != nil {
			return err
		}
		for name, expected := range files {
			data, err := os.ReadFile(filepath.Join(workspace, name))
			if err != nil || string(data) != expected {
				return ErrLSPAuthority
			}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	limiter, err := NewSessionLimiter(0)
	if err != nil {
		t.Fatal(err)
	}
	config := SessionConfig{ServerBinRoot: root, ExpectedSHA256: "b94804c7a4f74bcf0765263cb022b78459fc61258fe30c67607106af96fa5029", Environment: runtime.GoEnvironmentInput{GoRoot: goRoot, ScratchRoot: scratch, WorkspaceRoot: workspace, GOOS: "linux", GOARCH: "amd64", BuildTags: []string{}}, Documents: docs, Check: guard, Limiter: limiter}
	s, err := StartSession(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartSession(ctx, config); !errors.Is(err, ErrLSPSessionLimit) {
		t.Fatalf("concurrent session exceeded limit: %v", err)
	}
	selector := model.PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: docs[0].File.ContentHash(), ByteOffset: int64(strings.Index(a, "Value"))}
	result, err := s.Preview(ctx, selector, "Score")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edits) != 4 || len(result.FilePreconditions) != 2 || checks < 4 {
		t.Fatalf("incomplete result: %d edits, %d preconditions, %d checks", len(result.Edits), len(result.FilePreconditions), checks)
	}
	want := map[string][]int64{"a.go": {int64(strings.Index(a, "Value"))}, "b.go": {int64(strings.Index(b, "Value")), int64(strings.Index(b, "object.Value") + len("object.")), int64(strings.LastIndex(b, "Value"))}}
	got := map[string][]int64{}
	for _, edit := range result.Edits {
		if edit.NewText != "Score" || edit.SourceRef.Range.EndByte-edit.SourceRef.Range.StartByte != 5 {
			t.Fatalf("unexpected edit: %+v", edit)
		}
		got[edit.SourceRef.Path] = append(got[edit.SourceRef.Path], edit.SourceRef.Range.StartByte)
	}
	for name, positions := range want {
		sort.Slice(got[name], func(i, j int) bool { return got[name][i] < got[name][j] })
		if len(got[name]) != len(positions) {
			t.Fatalf("missing %s edits", name)
		}
		for i := range positions {
			if got[name][i] != positions[i] {
				t.Fatalf("wrong %s edit: %v versus %v", name, got[name], positions)
			}
		}
	}
	if _, err := s.Preview(ctx, selector, "Another"); !errors.Is(err, ErrLSPProtocol) {
		t.Fatalf("session reused: %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	permit, err := limiter.acquire(ctx)
	if err != nil {
		t.Fatalf("completed session retained its quota: %v", err)
	}
	permit.release()
	if err := guard(ctx); err != nil {
		t.Fatal("fixture source changed")
	}
	checksDuringFailure := 0
	badConfig := config
	badConfig.Check = func(ctx context.Context) error {
		checksDuringFailure++
		if checksDuringFailure == 3 {
			return ErrLSPAuthority
		}
		return guard(ctx)
	}
	if _, err := StartSession(ctx, badConfig); !errors.Is(err, ErrLSPAuthority) {
		t.Fatalf("late startup revoke accepted: %v", err)
	}
	permit, err = limiter.acquire(ctx)
	if err != nil {
		t.Fatalf("failed initialized session retained its quota: %v", err)
	}
	permit.release()
}
