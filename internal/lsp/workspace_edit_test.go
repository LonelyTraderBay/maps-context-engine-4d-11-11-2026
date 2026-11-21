package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func workspaceEditFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("fixture")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func workspaceEditFixtureURI(t *testing.T, path string) string {
	t.Helper()
	root := filepath.ToSlash(workspaceEditFixtureRoot(t))
	return (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(root, "/") + "/" + path}).String()
}

func workspaceEditFixture(t *testing.T) WorkspaceEditInput {
	t.Helper()
	id := "01234567-89ab-cdef-0123-456789abcdef"
	profile := strings.Repeat("a", 64)
	version := int64(7)
	docs := make([]CapturedDocument, 0, 2)
	for _, item := range []struct{ path, text string }{{"a.go", "A😀B\r\nCuối\n"}, {"b.go", "hello\nworld\n"}} {
		file, err := snapshot.NewFile(item.path, []byte(item.text), 1024)
		if err != nil {
			t.Fatal(err)
		}
		ref := model.SourceRef{WorktreeID: id, ProfileID: profile, GenerationID: id, Path: item.path, ContentHash: file.ContentHash()}
		docs = append(docs, CapturedDocument{File: file, Ref: ref, Version: &version})
	}
	return WorkspaceEditInput{WorkspaceRoot: workspaceEditFixtureRoot(t), Documents: docs, Encoding: snapshot.UTF16, MaxBytes: 1024 * 1024, MaxEdits: 10000}
}

const goodEdit = `{"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":3}},"newText":"X"}`

func documentChange(uri, version, edits string) string {
	return `{"textDocument":{"uri":"` + uri + `","version":` + version + `},"edits":[` + edits + `]}`
}

func TestWorkspaceEditCapturedUTF16AndOrdering(t *testing.T) {
	in := workspaceEditFixture(t)
	second := `{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":5}},"newText":"earth"}`
	raw := `{"documentChanges":[` + documentChange(workspaceEditFixtureURI(t, "b.go"), "7", second) + `,` + documentChange(workspaceEditFixtureURI(t, "a.go"), "7", goodEdit+","+goodEdit) + `]}`
	out, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Edits) != 2 || len(out.FilePreconditions) != 2 {
		t.Fatalf("got %d edits and %d preconditions", len(out.Edits), len(out.FilePreconditions))
	}
	a, b := out.Edits[0], out.Edits[1]
	if a.SourceRef.Path != "a.go" || a.SourceRef.Range != (model.ByteRange{StartByte: 1, EndByte: 5}) || a.NewText != "X" || b.SourceRef.Path != "b.go" || b.SourceRef.Range != (model.ByteRange{StartByte: 6, EndByte: 11}) || b.NewText != "earth" {
		t.Fatalf("incorrect captured ranges: %+v", out.Edits)
	}
	if out.FilePreconditions[0].ContentHash != in.Documents[0].File.ContentHash() || out.FilePreconditions[1].ContentHash != in.Documents[1].File.ContentHash() {
		t.Fatal("preconditions did not retain captured hashes")
	}
	if string(in.Documents[0].File.Bytes()) != "A😀B\r\nCuối\n" {
		t.Fatal("source changed")
	}
}

func TestWorkspaceEditRejectsUnsafeResponses(t *testing.T) {
	valid := documentChange(workspaceEditFixtureURI(t, "a.go"), "7", goodEdit)
	cases := map[string]string{
		"foreign path":          documentChange(workspaceEditFixtureURI(t, "../a.go"), "7", goodEdit),
		"outside path":          documentChange("file:///elsewhere/a.go", "7", goodEdit),
		"uri authority":         documentChange("file://remote/fixture/a.go", "7", goodEdit),
		"version changed":       documentChange(workspaceEditFixtureURI(t, "a.go"), "8", goodEdit),
		"null version":          documentChange(workspaceEditFixtureURI(t, "a.go"), "null", goodEdit),
		"fractional version":    documentChange(workspaceEditFixtureURI(t, "a.go"), "7.0", goodEdit),
		"surrogate split":       documentChange(workspaceEditFixtureURI(t, "a.go"), "7", strings.Replace(goodEdit, `"character":1`, `"character":2`, 1)),
		"line overflow":         documentChange(workspaceEditFixtureURI(t, "a.go"), "7", strings.Replace(goodEdit, `"line":0`, `"line":4`, 1)),
		"character overflow":    documentChange(workspaceEditFixtureURI(t, "a.go"), "7", strings.Replace(goodEdit, `"character":3`, `"character":99`, 1)),
		"null text":             documentChange(workspaceEditFixtureURI(t, "a.go"), "7", strings.Replace(goodEdit, `"X"`, `null`, 1)),
		"annotated edit":        documentChange(workspaceEditFixtureURI(t, "a.go"), "7", strings.Replace(goodEdit, `"newText":"X"`, `"newText":"X","annotationId":"x"`, 1)),
		"unknown edit field":    documentChange(workspaceEditFixtureURI(t, "a.go"), "7", strings.Replace(goodEdit, `"newText":"X"`, `"newText":"X","unsafe":true`, 1)),
		"duplicate JSON member": documentChange(workspaceEditFixtureURI(t, "a.go"), "7", strings.Replace(goodEdit, `"newText":"X"`, `"newText":"X","newText":"Y"`, 1)),
		"delete resource":       `{"kind":"delete","uri":"file:///fixture/a.go"}`,
		"rename resource":       `{"kind":"rename","oldUri":"file:///fixture/a.go","newUri":"file:///fixture/b.go"}`,
		"create resource":       `{"kind":"create","uri":"file:///fixture/c.go"}`,
		"duplicate document":    valid + `,` + valid,
		"empty document edits":  documentChange(workspaceEditFixtureURI(t, "a.go"), "7", ""),
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateWorkspaceEdit(context.Background(), []byte(`{"documentChanges":[`+change+`]}`), workspaceEditFixture(t))
			if !errors.Is(err, ErrWorkspaceEdit) {
				t.Fatalf("expected rejection, got %v", err)
			}
		})
	}
	for name, raw := range map[string]string{
		"both representations": `{"changes":{},"documentChanges":[]}`,
		"annotations":          `{"changeAnnotations":{},"documentChanges":[]}`,
		"null object":          `null`,
		"empty result":         `{"documentChanges":[]}`,
		"unknown object":       `{"arbitrary":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), workspaceEditFixture(t)); !errors.Is(err, ErrWorkspaceEdit) {
				t.Fatalf("expected rejection, got %v", err)
			}
		})
	}
}

func TestWorkspaceEditOverlapCapsAndCapturedIdentity(t *testing.T) {
	in := workspaceEditFixture(t)
	overlap := `{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}},"newText":"Z"}`
	raw := `{"documentChanges":[` + documentChange(workspaceEditFixtureURI(t, "a.go"), "7", overlap+","+goodEdit) + `]}`
	if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("overlap accepted: %v", err)
	}
	in.MaxEdits = 1
	if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in); !errors.Is(err, ErrResultTooLarge) {
		t.Fatalf("cap ignored: %v", err)
	}
	in = workspaceEditFixture(t)
	in.Documents[0].Ref.ContentHash = strings.Repeat("b", 64)
	raw = `{"documentChanges":[` + documentChange(workspaceEditFixtureURI(t, "a.go"), "7", goodEdit) + `]}`
	if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("changed hash accepted: %v", err)
	}
	in = workspaceEditFixture(t)
	in.Documents[1].File = in.Documents[0].File
	in.Documents[1].Ref = in.Documents[0].Ref
	if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("duplicate source accepted: %v", err)
	}
}

func TestWorkspaceEditVersionlessAndEncoding(t *testing.T) {
	in := workspaceEditFixture(t)
	in.Documents[0].Version = nil
	raw := `{"changes":{"` + workspaceEditFixtureURI(t, "a.go") + `":[` + goodEdit + `]}}`
	if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in); err != nil {
		t.Fatal(err)
	}
	in.Documents[0].Version = new(int64)
	if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("versionless change for open document: %v", err)
	}
	in.Documents[0].Version = nil
	in.Encoding = snapshot.UTF8
	if _, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("split UTF-8 accepted: %v", err)
	}
	in.Encoding = snapshot.UTF32
	correct := strings.Replace(goodEdit, `"character":3`, `"character":2`, 1)
	raw = `{"changes":{"` + workspaceEditFixtureURI(t, "a.go") + `":[` + correct + `]}}`
	out, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in)
	if err != nil || out.Edits[0].SourceRef.Range != (model.ByteRange{StartByte: 1, EndByte: 5}) {
		t.Fatalf("UTF-32 range: %+v %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ValidateWorkspaceEdit(ctx, []byte(raw), in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}

func TestWorkspaceEditCanMarshalResult(t *testing.T) {
	in := workspaceEditFixture(t)
	raw := `{"documentChanges":[` + documentChange(workspaceEditFixtureURI(t, "a.go"), "7", goodEdit) + `]}`
	out, err := ValidateWorkspaceEdit(context.Background(), []byte(raw), in)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(out.Edits[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = model.DecodeProposedEditJSON(data, in.MaxBytes); err != nil {
		t.Fatal(err)
	}
}
