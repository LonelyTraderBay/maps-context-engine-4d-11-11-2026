//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

func TestProtectedTraceImportFixture(t *testing.T) {
	ctx, profile := profileCatalogFixture(t, true)
	dir := t.TempDir()
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	const path = "fixture.trace.json"
	const raw = `{"metadata":{"schema_version":1,"application":"inventory","service":"stock","environment":"test",
		"revision":"","build_digest":"","profile_id":"","collector":"fixture","instrumentation":"v1",
		"window_start":"2026-10-01T01:00:00Z","window_end":"2026-10-01T01:01:00Z","sample_rate_ppm":1000000,"sampling":"all"},
		"spans":[{"trace_id":"11111111111111111111111111111111","span_id":"2222222222222222","parent_span_id":"",
		"interaction_id":"","operation":"stock.read","started_at":"2026-10-01T01:00:10Z",
		"ended_at":"2026-10-01T01:00:14Z","status":"ok","links":[]}]}`
	if err := os.WriteFile(filepath.Join(dir, path), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(raw))
	pin := hex.EncodeToString(sum[:])
	check := func(ctx context.Context) error { return ctx.Err() }
	req := ProtectedTraceImport{
		SourceRoot: root, RelativePath: path,
		Approval: importtrace.Approval{
			InputHash: pin, Application: "inventory", Service: "stock", Environment: "test",
			Collector: "fixture", Instrumentation: "v1", Operations: []string{"stock.read"},
			Recheck: func(ctx context.Context, approvedRoot *paths.Root, approvedPath, digest string) error {
				if approvedRoot != root || approvedPath != path || digest != pin {
					return ErrProfileDenied
				}
				return ctx.Err()
			},
		},
	}
	if _, err := ImportProtectedTrace(ctx, profile, ownerWorktree, time.Second, req, check); !errors.Is(err, records.ErrEvidenceMigrationRequired) {
		t.Fatalf("silent schema upgrade: %v", err)
	}
	req.ApproveUpgrade = true
	wrote, err := ImportProtectedTrace(ctx, profile, ownerWorktree, time.Second, req, check)
	if err != nil || wrote.ArtifactID != pin || wrote.RecordEpoch != 1 || wrote.SpanCount != 1 || wrote.Replayed {
		t.Fatalf("protected import: %+v %v", wrote, err)
	}
	req.ApproveUpgrade = false
	replay, err := ImportProtectedTrace(ctx, profile, ownerWorktree, time.Second, req, check)
	if err != nil || replay.ArtifactID != pin || replay.RecordEpoch != 1 || replay.SpanCount != 1 || !replay.Replayed {
		t.Fatalf("protected reopen/replay: %+v %v", replay, err)
	}
	owner, err := OpenProtectedWorktree(ctx, profile, ownerWorktree, check)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Records.Store()
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.ReadState(ctx, 10, func(ctx context.Context, _ records.Identity) error { return ctx.Err() })
	if err != nil || state.RecordEpoch != wrote.RecordEpoch || state.Identity.Incarnation != owner.manifest.RecordsIncarnation {
		t.Fatalf("published WAL authority state: %+v %v", state, err)
	}
	artifact, observed, err := store.ReadTrace(ctx, pin, func(ctx context.Context, _ records.Identity) error { return ctx.Err() })
	if err != nil || observed.RecordEpoch != wrote.RecordEpoch {
		t.Fatalf("protected trace read: %+v %v", observed, err)
	}
	graph, err := traces.Project(ctx, []importtrace.Artifact{artifact})
	if err != nil || graph.Plane != "observed_runtime" || len(graph.Nodes) != 1 || len(graph.Relations) != 0 ||
		len(graph.Gaps) != 1 || graph.Gaps[0].Code != "INSTRUMENTATION_COVERAGE_UNKNOWN" {
		t.Fatalf("observed-only graph from protected records: %+v %v", graph, err)
	}
}
