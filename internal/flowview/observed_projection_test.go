package flowview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	_ "modernc.org/sqlite"
)

func TestC107C109ObservedProjectionUsesBuildCaptureAndExplicitRollback(t *testing.T) {
	ctx := t.Context()
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "22222222-2222-4222-8222-222222222222"
	const interaction = "33333333-3333-4333-8333-333333333333"
	file, err := snapshot.NewFile("service.go", []byte("package sample\nfunc handler() {}\n"), 256)
	if err != nil {
		t.Fatal(err)
	}
	hash, size := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree,
		ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64),
		Files:                  []model.ManifestFile{{Path: file.Path(), Status: "captured", ContentHash: &hash, ByteLength: &size}},
		DependencyInputDigests: []string{}}
	capture, err := snapshot.Freeze(manifest, []snapshot.File{file}, 32768, 1024)
	if err != nil {
		t.Fatal(err)
	}
	scope := model.ScopeVector{Kind: "single", CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{},
		Members: []model.ScopeMember{{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation,
			SnapshotDigest: capture.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1,
			BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "44444444-4444-4444-8444-444444444444",
			RecordsIncarnation: "55555555-5555-4555-8555-555555555555"}}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 32768)
	if err != nil {
		t.Fatal(err)
	}
	root := &model.SymbolRef{Kind: "symbol", SymbolID: "typed-handler",
		GraphIdentity: model.GraphIdentity{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation}}
	origin := importtrace.Metadata{SchemaVersion: 1, Application: "orders", Service: "web", Environment: "test",
		Revision: strings.Repeat("d", 40), BuildDigest: strings.Repeat("e", 64), ProfileID: manifest.ProfileID,
		Collector: "fixture", Instrumentation: "v1", WindowStart: "2026-10-01T01:00:00Z",
		WindowEnd: "2026-10-01T01:00:03Z", Sampling: "all", SampleRatePPM: 1000000}
	id := strings.Repeat("f", 64)
	traceID := strings.Repeat("9", 32)
	spanID := func(digit string) string { return strings.Repeat(digit, 16) }
	makeSpan := func(digit, parent, operation string) importtrace.Span {
		return importtrace.Span{SpanKey: importtrace.SpanKey{TraceID: traceID, SpanID: spanID(digit)},
			ParentSpanID: parent, Operation: operation, StartedAt: "2026-10-01T01:00:01Z",
			EndedAt: "2026-10-01T01:00:02Z", Status: "ok", Links: []importtrace.SpanKey{}}
	}
	request := makeSpan("1", "", "front.request")
	request.InteractionID = interaction
	// Names intentionally invert the apparent transaction semantics. The
	// verified instrumentation roles below supply the observation.
	statement := makeSpan("2", spanID("1"), "commit")
	rollback := makeSpan("3", spanID("1"), "statement")
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test-state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE TABLE fixture (value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT INTO fixture (value) VALUES (?)", 37)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		t.Fatalf("fixture statement did not affect one row: %d %v", count, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var persisted int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM fixture").Scan(&persisted); err != nil || persisted != 0 {
		t.Fatalf("fixture rollback persisted rows: %d %v", persisted, err)
	}
	noBinding := makeSpan("4", spanID("1"), "unmapped")
	marshal := func(v any) json.RawMessage {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	artifact, err := importtrace.DecodeStored(id, marshal(origin), []json.RawMessage{
		marshal(request), marshal(statement), marshal(rollback), marshal(noBinding),
	})
	if err != nil {
		t.Fatal(err)
	}
	selected := traces.Ref{ArtifactID: id, TraceID: traceID, SpanID: request.SpanID}
	opts := ObservedOptions{
		Scope: scope, Root: root, Steps: 10, Edges: 10,
		Request: ObservedRequest{InteractionID: interaction, RequestID: generation,
			TraceParent: "00-" + traceID + "-" + request.SpanID + "-01", RequestSpan: selected, BuildDigest: origin.BuildDigest,
			VerifyRequest: func(ctx context.Context, _ ObservedRequest) error { return ctx.Err() }},
		RecheckScope: func(ctx context.Context, _ model.ScopeVector) error { return ctx.Err() },
		Verify:       func(ctx context.Context, _ ObservedBinding) error { return ctx.Err() },
	}
	roles := map[string]string{request.SpanID: "http_request", statement.SpanID: "sql_statement", rollback.SpanID: "sql_rollback"}
	stale := ""
	opts.Resolve = func(_ context.Context, ref traces.Ref) (ObservedBinding, bool, error) {
		role, ok := roles[ref.SpanID]
		binding := ObservedBinding{Span: ref, Application: origin.Application, Service: origin.Service, Environment: origin.Environment,
			Revision: origin.Revision, BuildDigest: origin.BuildDigest, ProfileID: origin.ProfileID, GraphRef: root, Capture: capture, Role: role,
			Source: model.SourceRef{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation,
				Path: file.Path(), ContentHash: hash, Range: model.ByteRange{StartByte: 20, EndByte: 26}}}
		if role == "sql_statement" || role == "sql_rollback" {
			binding.TransactionID = strings.Repeat("7", 64)
		}
		if ref.SpanID == statement.SpanID {
			switch stale {
			case "revision":
				binding.Revision = strings.Repeat("5", 40)
			case "source":
				binding.Source.ContentHash = strings.Repeat("5", 64)
			case "generation":
				binding.Source.GenerationID = "99999999-9999-4999-8999-999999999999"
			}
		}
		return binding, ok, nil
	}
	projection, err := ProjectObserved(ctx, []importtrace.Artifact{artifact}, opts)
	if err != nil || !projection.Partial || projection.UnboundSpans != 1 || len(projection.Data.Steps) != 3 ||
		len(projection.Data.Edges) != 3 {
		t.Fatalf("observed flow: %+v %v", projection, err)
	}
	if _, err := model.DecodeFlowDataJSON(marshal(projection.Data), 1<<20); err != nil {
		t.Fatalf("observed wire DTO: %v", err)
	}
	sql := projection.Data.Steps[1]
	if sql.OutcomeStatus != "statement_success_observed" || len(sql.Limitations) != 1 ||
		sql.Limitations[0].Code != "OBSERVED_TRANSACTION_ROLLBACK" ||
		projection.Data.Steps[2].OutcomeStatus != "rollback_observed" ||
		projection.Data.Edges[2].Kind != "transaction_link" {
		t.Fatalf("statement and rollback were conflated: %+v", projection.Data)
	}
	delete(roles, rollback.SpanID)
	projection, err = ProjectObserved(ctx, []importtrace.Artifact{artifact}, opts)
	if err != nil || projection.Data.Steps[1].Limitations[0].Code != "OBSERVED_TRANSACTION_OUTCOME_UNKNOWN" {
		t.Fatalf("missing commit/rollback inferred persistence: %+v %v", projection.Data, err)
	}
	opts.Steps = 1
	projection, err = ProjectObserved(ctx, []importtrace.Artifact{artifact}, opts)
	if err != nil || !projection.Truncated || len(projection.Data.Steps) != 1 || len(projection.Data.Edges) != 0 ||
		projection.Data.Steps[0].StepKind != "http_request" {
		t.Fatalf("node cap lost the root: %+v %v", projection, err)
	}
	opts.Steps = 10
	for _, mismatch := range []string{"revision", "source", "generation"} {
		stale = mismatch
		if got, err := ProjectObserved(ctx, []importtrace.Artifact{artifact}, opts); !errors.Is(err, ErrUnverified) || len(got.Data.Steps) > 0 {
			t.Fatalf("mismatched %s produced flow: %+v %v", mismatch, got, err)
		}
	}
	if artifact.Metadata().Revision != origin.Revision {
		t.Fatal("refused source mapping modified historical trace metadata")
	}
}
