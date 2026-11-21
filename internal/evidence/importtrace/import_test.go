package importtrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func fixture() (Metadata, Span, Approval) {
	return Metadata{
		SchemaVersion: 1, Application: "inventory", Service: "stock", Environment: "test",
		Collector: "fixture", Instrumentation: "v1", WindowStart: "2026-10-01T01:00:00Z",
		WindowEnd: "2026-10-01T01:02:00Z", Sampling: "head", SampleRatePPM: 100_000,
	}, Span{
		SpanKey:      SpanKey{TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16)},
		ParentSpanID: strings.Repeat("c", 16), Operation: "stock.read", StartedAt: "2026-10-01T01:00:10Z",
		EndedAt: "2026-10-01T01:00:11Z", Status: "ok",
		Links: []SpanKey{{TraceID: strings.Repeat("d", 32), SpanID: strings.Repeat("e", 16)}},
	}, Approval{
		Application: "inventory", Service: "stock", Environment: "test",
		Collector: "fixture", Instrumentation: "v1", Operations: []string{"stock.read"},
	}
}

func exportBytes(t *testing.T, metadata Metadata, spans ...Span) []byte {
	t.Helper()
	body, err := json.Marshal(struct {
		Metadata Metadata `json:"metadata"`
		Spans    []Span   `json:"spans"`
	}{metadata, spans})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDecodeMetadataBeforePersistence(t *testing.T) {
	metadata, span, approved := fixture()
	body := exportBytes(t, metadata, span)
	artifact, err := decode(body, approved)
	if err != nil || artifact.Metadata().Revision != "" || artifact.Metadata().BuildDigest != "" ||
		len(artifact.Spans()) != 1 || len(artifact.Spans()[0].Links) != 1 {
		t.Fatalf("metadata for an unbound observation: %v", err)
	}
	copy := artifact.Spans()
	copy[0].Links[0].SpanID = "modified"
	if artifact.Spans()[0].Links[0] != span.Links[0] {
		t.Fatal("caller changed the validated artifact")
	}
	for _, candidate := range []struct{ name, old, replacement string }{
		{"unexpected property", `"service":"stock"`, `"service":"stock","customer_email":"synthetic@example.invalid"`},
		{"unapproved operation label", `"operation":"stock.read"`, `"operation":"customer-104"`},
		{"duplicate property", `"service":"stock"`, `"service":"stock","service":"other"`},
		{"null link array", `"links":[`, `"links":null,"unused":[`},
		{"unexpected link attributes", `"span_id":"eeeeeeeeeeeeeeee"`, `"span_id":"eeeeeeeeeeeeeeee","attributes":{}`},
		{"invalid rate", `"sample_rate_ppm":100000`, `"sample_rate_ppm":1000001`},
		{"span outside window", `"started_at":"2026-10-01T01:00:10Z"`, `"started_at":"2026-10-01T00:59:59Z"`},
		{"unreviewed revision syntax", `"revision":""`, `"revision":"HEAD"`},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			changed := strings.Replace(string(body), candidate.old, candidate.replacement, 1)
			if changed == string(body) {
				t.Fatal("fixture replacement was a no-op")
			}
			if _, err := decode([]byte(changed), approved); !errors.Is(err, ErrExport) {
				t.Fatalf("unexpected admission: %v", err)
			}
		})
	}
	if _, err := decode(exportBytes(t, metadata, span, span), approved); !errors.Is(err, ErrExport) {
		t.Fatal("duplicate span identity admitted")
	}
	span.Links = make([]SpanKey, MaxLinksPerSpan+1)
	if _, err := decode(exportBytes(t, metadata, span), approved); !errors.Is(err, ErrExport) {
		t.Fatal("oversized span admitted")
	}
}

func TestReadApprovedPinnedTrace(t *testing.T) {
	metadata, span, approved := fixture()
	body := exportBytes(t, metadata, span)
	digest := sha256.Sum256(body)
	approved.InputHash = hex.EncodeToString(digest[:])
	dir := t.TempDir()
	const relative = "fixture.trace.json"
	file := filepath.Join(dir, relative)
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	checks := 0
	approved.Recheck = func(_ context.Context, opened *paths.Root, path, hash string) error {
		checks++
		if opened != root || path != relative || hash != approved.InputHash {
			return ErrApproval
		}
		return nil
	}
	result, err := Read(ctx, root, relative, approved)
	if err != nil || result.InputHash() != approved.InputHash || checks != 2 {
		t.Fatalf("pinned file read: checks=%d err=%v", checks, err)
	}
	if err := os.WriteFile(file, append(body, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ctx, root, relative, approved); !errors.Is(err, ErrDigest) {
		t.Fatalf("changed bytes: %v", err)
	}
	if _, err := Read(ctx, root, "elsewhere.json", approved); !errors.Is(err, ErrApproval) {
		t.Fatalf("unapproved path: %v", err)
	}
}
