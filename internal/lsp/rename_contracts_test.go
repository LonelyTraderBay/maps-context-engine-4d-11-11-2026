package lsp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func contractDocument(t *testing.T, source string, start, end int) (CapturedDocument, model.ProposedEdit) {
	t.Helper()
	file, err := snapshot.NewFile("a.go", []byte(source), 65536)
	if err != nil {
		t.Fatal(err)
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ref := model.SourceRef{WorktreeID: id, ProfileID: strings.Repeat("a", 64), GenerationID: id, Path: "a.go", ContentHash: file.ContentHash()}
	editRef := ref
	editRef.Range = model.ByteRange{StartByte: int64(start), EndByte: int64(end)}
	return CapturedDocument{File: file, Ref: ref}, model.ProposedEdit{SourceRef: editRef, NewText: "Renamed"}
}

func hasContractGap(gaps []model.Gap, code string) bool {
	for _, gap := range gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}

func TestRenameContractFieldTags(t *testing.T) {
	for _, tc := range []struct {
		name, field       string
		implicit, unknown bool
	}{
		{"absent", "Value string", true, false},
		{"default tag", "Value string `json:\",omitempty\"`", true, false},
		{"explicit tag", "Value string `json:\"custom\"`", false, false},
		{"invalid JSON name", "Value string `json:\"bad\\\"name\"`", true, false},
		{"excluded", "Value string `json:\"-\"`", false, false},
		{"unrelated tag", "Value string `yaml:\"x\"`", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package p\ntype Thing struct { " + tc.field + " }\n"
			start := strings.Index(source, "Value")
			doc, edit := contractDocument(t, source, start, start+len("Value"))
			gaps, err := InspectRenameContracts(context.Background(), []CapturedDocument{doc}, []model.ProposedEdit{edit}, true)
			if err != nil {
				t.Fatal(err)
			}
			if hasContractGap(gaps, "IMPLICIT_SERIALIZATION_NAME_CHANGE") != tc.implicit || hasContractGap(gaps, "SERIALIZATION_BOUNDARY_UNKNOWN") != tc.unknown || !hasContractGap(gaps, "SERIALIZATION_RUNTIME_NOT_PROVEN") || !hasContractGap(gaps, "REFLECTION_RUNTIME_NOT_PROVEN") {
				t.Fatalf("wrong boundaries: %+v", gaps)
			}
		})
	}
}

func TestRenameContractsRejectLiteralEditsByDefault(t *testing.T) {
	source := "package p\ntype Thing struct { Value string `json:\"stable\"` }\n"
	start := strings.Index(source, "stable")
	doc, edit := contractDocument(t, source, start, start+len("stable"))
	if _, err := InspectRenameContracts(context.Background(), []CapturedDocument{doc}, []model.ProposedEdit{edit}, true); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("literal edit accepted: %v", err)
	}
	if _, err := InspectRenameContracts(context.Background(), []CapturedDocument{doc}, []model.ProposedEdit{edit}, false); err != nil {
		t.Fatalf("explicit non-preserve request rejected: %v", err)
	}
	edit.SourceRef.Range.EndByte = edit.SourceRef.Range.StartByte
	if _, err := InspectRenameContracts(context.Background(), []CapturedDocument{doc}, []model.ProposedEdit{edit}, true); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("insertion inside literal accepted: %v", err)
	}
	edit.SourceRef.Range.StartByte = 0
	edit.SourceRef.Range.EndByte = 0
	edit.NewText = `"unsafe"`
	if _, err := InspectRenameContracts(context.Background(), []CapturedDocument{doc}, []model.ProposedEdit{edit}, true); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("new literal accepted: %v", err)
	}
}

func TestRenameContractsRawStringCRLFUsesOriginalOffsets(t *testing.T) {
	source := "package p\nvar text = `A\r\nB\r\nC\r\nlast`\n"
	start := strings.Index(source, "last") + len("last") - 1
	doc, edit := contractDocument(t, source, start, start+1)
	if _, err := InspectRenameContracts(context.Background(), []CapturedDocument{doc}, []model.ProposedEdit{edit}, true); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("raw literal edit after CRLF accepted: %v", err)
	}
}

func TestRenameContractsRejectForeignSourceAndCancel(t *testing.T) {
	source := "package p\nvar value = 1\n"
	start := strings.Index(source, "value")
	doc, edit := contractDocument(t, source, start, start+5)
	edit.SourceRef.ContentHash = strings.Repeat("b", 64)
	if _, err := InspectRenameContracts(context.Background(), []CapturedDocument{doc}, []model.ProposedEdit{edit}, true); !errors.Is(err, ErrWorkspaceEdit) {
		t.Fatalf("foreign source accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectRenameContracts(ctx, []CapturedDocument{doc}, []model.ProposedEdit{edit}, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}
