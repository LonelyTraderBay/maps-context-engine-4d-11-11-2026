package records

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func TestC070TraceImportAtomicAndUnbound(t *testing.T) {
	ctx, db, store, _, identity := initialized(t)
	rootPath := t.TempDir()
	root, err := paths.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	const relative = "stock.trace.json"
	const body = `{"metadata":{"schema_version":1,"application":"inventory","service":"stock","environment":"test",
		"revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","build_digest":"","profile_id":"",
		"collector":"fixture","instrumentation":"v1","window_start":"2026-10-01T01:00:00Z","window_end":"2026-10-01T01:02:00Z",
		"sample_rate_ppm":100000,"sampling":"head"},"spans":[{"trace_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"span_id":"bbbbbbbbbbbbbbbb","parent_span_id":"cccccccccccccccc","interaction_id":"","operation":"stock.read",
		"started_at":"2026-10-01T01:00:10Z","ended_at":"2026-10-01T01:00:11Z","status":"ok","links":[]}]}`
	checkAccess := func(_ context.Context, approvedRoot *paths.Root, path, _ string) error {
		if approvedRoot != root || path != relative {
			return fixtureDenied
		}
		return nil
	}
	req := TraceImportRequest{
		Root: root, RelativePath: relative, Writer: &fixtureWriter{}, JobID: fixtureJob, Authorize: allowed,
		Approval: importtrace.Approval{
			Application: "inventory", Service: "stock", Environment: "test", Collector: "fixture",
			Instrumentation: "v1", Operations: []string{"stock.read"}, Recheck: checkAccess,
		},
	}
	write := func(input string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(rootPath, relative), []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(input))
		req.Approval.InputHash = hex.EncodeToString(hash[:])
	}
	assertVersion := func(version int) {
		t.Helper()
		var got int
		if err := db.QueryRow(`SELECT schema_version FROM schema_meta`).Scan(&got); err != nil || got != version {
			t.Fatalf("schema version = %d, want %d: %v", got, version, err)
		}
	}
	write(body)
	if _, err := store.ImportTrace(ctx, req); !errors.Is(err, ErrEvidenceMigrationRequired) {
		t.Fatalf("migration was not required: %v", err)
	}
	req.AllowUpgrade = true
	actualLease := store.lease
	store.lease = sharedOnly{actualLease}
	if _, err := store.ImportTrace(ctx, req); !errors.Is(err, fixtureDenied) {
		t.Fatalf("shared lease permitted upgrade: %v", err)
	}
	store.lease = actualLease
	assertVersion(1)
	write(strings.Replace(body, `"service":"stock"`, `"service":"stock","customer_email":"fake@example.invalid"`, 1))
	if _, err := store.ImportTrace(ctx, req); !errors.Is(err, importtrace.ErrExport) {
		t.Fatalf("unexpected field reached migration: %v", err)
	}
	assertVersion(1)
	write(strings.Replace(body, `"operation":"stock.read"`, `"operation":"stock.read","events":[]`, 1) + strings.Repeat(" ", importtrace.MaxExportBytes))
	if _, err := store.ImportTrace(ctx, req); !errors.Is(err, importtrace.ErrExport) {
		t.Fatalf("oversized export reached migration: %v", err)
	}
	assertVersion(1)
	write(body)
	var checks int
	req.Approval.Recheck = func(ctx context.Context, root *paths.Root, path, hash string) error {
		checks++
		if checks == 4 {
			return fixtureDenied
		}
		return checkAccess(ctx, root, path, hash)
	}
	if _, err := store.ImportTrace(ctx, req); !errors.Is(err, importtrace.ErrApproval) || checks != 4 {
		t.Fatalf("final authorization did not roll back transaction: %d, %v", checks, err)
	}
	assertVersion(1)
	var artifacts int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name IN ('evidence_artifacts','evidence_items')`).Scan(&artifacts); err != nil || artifacts != 0 {
		t.Fatalf("rolled-back migration left evidence tables: %d %v", artifacts, err)
	}
	req.Approval.Recheck = checkAccess
	got, err := store.ImportTrace(ctx, req)
	if err != nil || got.ArtifactID != req.Approval.InputHash || got.RecordEpoch != 1 || got.SpanCount != 1 || got.Replayed {
		t.Fatalf("import: %+v %v", got, err)
	}
	assertVersion(2)
	state, err := store.ReadState(ctx, 10, allowed)
	if err != nil || state.Identity != identity || state.RecordEpoch != 1 {
		t.Fatalf("legacy read on v2: %+v %v", state, err)
	}
	var binding, inputHash, kind string
	err = db.QueryRow(`SELECT a.input_hash,a.artifact_kind,i.source_binding_json FROM evidence_artifacts a
		JOIN evidence_items i ON i.artifact_id=a.artifact_id`).Scan(&inputHash, &kind, &binding)
	if err != nil || inputHash != got.ArtifactID || kind != "runtime_trace" || binding != unboundTrace {
		t.Fatalf("persisted trace did not remain unbound: %q %q %v", kind, binding, err)
	}
	reopened, err := Open(ctx, db, identity.WorktreeID, actualLease)
	if err != nil {
		t.Fatal(err)
	}
	req.AllowUpgrade = false
	replay, err := reopened.ImportTrace(ctx, req)
	if err != nil || !replay.Replayed || replay.ArtifactID != got.ArtifactID || replay.RecordEpoch != got.RecordEpoch {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM evidence_items`).Scan(&count); err != nil || count != got.SpanCount {
		t.Fatalf("replay created extra spans: %s %v", strconv.Itoa(count), err)
	}
	artifact, observed, err := reopened.ReadTrace(ctx, got.ArtifactID, allowed)
	if err != nil || artifact.InputHash() != got.ArtifactID || len(artifact.Spans()) != 1 ||
		observed.Identity != identity || observed.RecordEpoch != 1 {
		t.Fatalf("authorized trace read: %+v %v", observed, err)
	}
	if _, _, err := reopened.ReadTrace(ctx, strings.Repeat("f", 64), allowed); !errors.Is(err, ErrTraceUnavailable) {
		t.Fatalf("unknown trace scope: %v", err)
	}
	reads := 0
	denied, state, err := reopened.ReadTrace(ctx, got.ArtifactID, func(context.Context, Identity) error {
		reads++
		if reads == 2 {
			return fixtureDenied
		}
		return nil
	})
	if !errors.Is(err, fixtureDenied) || denied.InputHash() != "" || state != (State{}) {
		t.Fatalf("revoked trace emitted data: %+v %v", state, err)
	}
}

type sharedOnly struct{ ports.LifecycleLease }

func (s sharedOnly) Check(ctx context.Context, worktree string, mode ports.LeaseMode) error {
	if mode != ports.SharedLease {
		return fixtureDenied
	}
	return s.LifecycleLease.Check(ctx, worktree, mode)
}
