package index

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

func TestCapturedFileSearchRequiresAuthorityAndAdvances(t *testing.T) {
	hash, size := strings.Repeat("a", 64), int64(3)
	files := []model.ManifestFile{{Path: "main.go", Status: "captured", ContentHash: &hash, ByteLength: &size}, {Path: "other.go", Status: "captured", ContentHash: &hash, ByteLength: &size}, {Path: "missing.go", Status: "missing"}}
	request := search.Request{Mode: "file", Query: ".go", Limit: 1, MaxFileBytes: 16, Keyset: true}
	denied := errors.New("denied")
	result := search.Result{Text: []search.TextHit{}}
	if err := searchCapturedFiles(context.Background(), files, request, func(path string) error {
		if path == "other.go" {
			return denied
		}
		return nil
	}, &result); !errors.Is(err, denied) || len(result.Text) != 0 || result.Truncated {
		t.Fatalf("denied match leaked a partial result: %+v %v", result, err)
	}
	if err := searchCapturedFiles(context.Background(), files, request, func(path string) error {
		if path == "missing.go" {
			return denied
		}
		return nil
	}, &result); !errors.Is(err, denied) || len(result.Text) != 0 {
		t.Fatalf("unauthorized unavailable match leaked a partial result: %+v %v", result, err)
	}
	allow := func(string) error { return nil }
	if err := searchCapturedFiles(context.Background(), files, request, allow, &result); err != nil || len(result.Text) != 1 || !result.Truncated || result.NextKey == "" || result.Text[0].EvidenceKind != "path_match" || result.Coverage != "partial" {
		t.Fatalf("first file page: %+v %v", result, err)
	}
	request.AfterID = result.NextKey
	next := search.Result{Text: []search.TextHit{}}
	if err := searchCapturedFiles(context.Background(), files, request, allow, &next); err != nil || len(next.Text) != 1 || next.Truncated || next.NextKey != "" || next.Text[0].Path == result.Text[0].Path || next.Coverage != "partial" {
		t.Fatalf("second file page: %+v %v", next, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := searchCapturedFiles(ctx, files, request, allow, &search.Result{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search ran: %v", err)
	}
}
