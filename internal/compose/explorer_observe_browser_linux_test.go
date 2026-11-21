//go:build linux

package compose

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/traces"
	explorerapi "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

type browserObserveExport struct {
	Artifact json.RawMessage
	Receipts []json.RawMessage
}

type browserObserveRun struct {
	receipt     flowview.ObserveReceipt
	artifactIDs [3]string
	childID     string
	rejectedID  string
	build       string
	revision    string
	site        *snapshot.File
}

const observeSite = "observe.mjs"

func TestT102ObserveBrowserRecords(t *testing.T) {
	node := os.Getenv("MCE4D_T102_BROWSER_NODE")
	if node == "" {
		t.Skip("T102 requires the pinned browser and Node fixture")
	}
	checkScopeReaderFixture(t, false, node)
}

func stageObservedBrowserFixture(t *testing.T, ctx context.Context, base, recordsPath, worktree, profile, node string,
	lease *runtime.Lifecycle, writer *runtime.Writer) *browserObserveRun {
	t.Helper()
	out := filepath.Join(base, "browser-export.json")
	browserCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	const browserScript = "../../tests/acceptance/explorer_observe.browser.mjs"
	verified := os.Getenv("MCE4D_T104_EXPLORER_ASSETS") != ""
	mode := "probe"
	if verified {
		mode = "verified"
	}
	cmd := exec.CommandContext(browserCtx, node, browserScript, profile, fixtureBuild(), observedRevision, out, mode)
	report, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Observe browser fixture: %v\n%s", err, report)
	}
	t.Logf("%s", report)
	raw, err := os.ReadFile(out)
	if err != nil || len(raw) > 16384 {
		t.Fatalf("read synthetic export: %v", err)
	}
	var export struct {
		First, Second browserObserveExport
		Backend       json.RawMessage
		ChildID       string
		BuildInputs   [3]string
	}
	if err := json.Unmarshal(raw, &export); err != nil || len(export.First.Receipts) != 1 || len(export.Second.Receipts) != 1 {
		t.Fatalf("synthetic export shape: %v", err)
	}
	result := &browserObserveRun{childID: export.ChildID, build: fixtureBuild(), revision: observedRevision}
	if verified {
		const siteScript = "../../tests/acceptance/explorer_observe_site.mjs"
		for i, path := range []string{out + ".bundle.mjs", siteScript, browserScript} {
			data, err := os.ReadFile(path)
			sum := sha256.Sum256(data)
			if err != nil || hex.EncodeToString(sum[:]) != export.BuildInputs[i] {
				t.Fatalf("test deployment material %d: %v", i, err)
			}
			if path == siteScript {
				file, err := snapshot.NewFile(observeSite, data, 1024)
				if err != nil {
					t.Fatal(err)
				}
				result.site = &file
			}
		}
		sum := sha256.Sum256(scopeJSON(t, export.BuildInputs))
		result.build = hex.EncodeToString(sum[:])
		result.revision = export.BuildInputs[1]
	}
	first, err := flowview.DecodeObserveReceipt(export.First.Receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := flowview.DecodeObserveReceipt(export.Second.Receipts[0])
	if err != nil || first.RequestID == second.RequestID || first.SessionID != second.SessionID ||
		first.InteractionID == second.InteractionID || first.TraceID == second.TraceID ||
		first.BuildMapID != result.build || second.BuildMapID != result.build || first.AuthorizationEpoch != 1 || second.AuthorizationEpoch != 1 {
		t.Fatalf("browser receipt identity: %v", err)
	}

	source := scopeRoot(t, base)
	owner, err := sqliteowner.OpenRecordsOwner(ctx, lease, scopeDB(t, recordsPath, 1), worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	result.receipt = first
	damaged := bytes.Replace(export.First.Artifact, []byte(`"operation":"request"`),
		[]byte(`"operation":"request","request_body":"fixture-sensitive","sql_params":["fixture-sensitive"],"dom_input":"fixture-sensitive"`), 1)
	if bytes.Equal(damaged, export.First.Artifact) {
		t.Fatal("export corruption fixture was not applied")
	}
	for i, item := range []struct {
		body               json.RawMessage
		service, operation string
	}{
		{export.First.Artifact, "frontend", "request"},
		{export.Second.Artifact, "frontend", "request"},
		{export.Backend, "backend", "handler"},
		{damaged, "frontend", "request"},
	} {
		sum := sha256.Sum256(item.body)
		id := hex.EncodeToString(sum[:])
		rel := item.service + "-" + id[:8] + ".trace.json"
		if err := os.WriteFile(filepath.Join(base, rel), item.body, 0600); err != nil {
			t.Fatal(err)
		}
		imported, err := store.ImportTrace(ctx, records.TraceImportRequest{
			Root: source, RelativePath: rel, AllowUpgrade: i == 0, Writer: writer, JobID: scopeJob,
			Authorize: func(ctx context.Context, identity records.Identity) error {
				if identity.WorktreeID != worktree {
					return ErrAuthority
				}
				return ctx.Err()
			},
			Approval: importtrace.Approval{InputHash: id, Application: "fixture", Environment: "test",
				Service: item.service, Collector: "fixture", Instrumentation: "v1", Operations: []string{item.operation},
				Recheck: func(ctx context.Context, root *paths.Root, path, digest string) error {
					if root != source || path != rel || digest != id {
						return ErrAuthority
					}
					return ctx.Err()
				}},
		})
		if i == len(result.artifactIDs) {
			if !errors.Is(err, importtrace.ErrExport) {
				t.Fatalf("out-of-schema runtime fields were admitted: %v", err)
			}
			result.rejectedID = id
			continue
		}
		if err != nil || imported.ArtifactID != id || imported.SpanCount != 1 || imported.RecordEpoch != int64(i+1) {
			t.Fatalf("import browser trace: %+v %v", imported, err)
		}
		result.artifactIDs[i] = id
	}
	return result
}

func verifyObservedBrowserFixture(t *testing.T, ctx context.Context, backend *CoreBackend, scope ScopeObservation,
	root model.GraphRef, principal app.CorePrincipal, captured *snapshot.Snapshot, run *browserObserveRun) {
	t.Helper()
	generation := scope.Vector.Members[0]
	check := func(ctx context.Context, identity records.Identity) error {
		if identity != scope.Records.Identity {
			return ErrAuthority
		}
		return backend.Reader.Access(ctx, principal, generation.WorktreeID, generation.ProfileID)
	}
	if _, _, err := backend.Reader.Records.ReadTrace(ctx, run.rejectedID, check); !errors.Is(err, records.ErrTraceUnavailable) {
		t.Fatalf("rejected export was persisted: %v", err)
	}
	artifacts := make([]importtrace.Artifact, 0, len(run.artifactIDs))
	for _, id := range run.artifactIDs {
		data, state, err := backend.Reader.Records.ReadTrace(ctx, id, check)
		if err != nil || state.RecordEpoch != 3 || data.Metadata().ProfileID != generation.ProfileID ||
			data.Metadata().BuildDigest != run.build || data.Metadata().Revision != run.revision {
			t.Fatalf("read approved browser artifact: %v", err)
		}
		artifacts = append(artifacts, data)
	}
	approved := true
	request, err := flowview.BindObserveReceipt(run.receipt, run.artifactIDs[0],
		func(ctx context.Context, receipt flowview.ObserveReceipt, id string) error {
			if !approved || receipt != run.receipt || id != run.artifactIDs[0] {
				return ErrAuthority
			}
			return ctx.Err()
		})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := flowview.CorrelateObserved(ctx, request, artifacts)
	if err != nil || len(projection.Graph.Nodes) != 2 || len(projection.Graph.Relations) != 1 || projection.UncorrelatedSpans != 1 ||
		projection.Graph.Relations[0].From != request.RequestSpan ||
		projection.Graph.Relations[0].To != (traces.Ref{ArtifactID: run.artifactIDs[2], TraceID: run.receipt.TraceID, SpanID: run.childID}) {
		t.Fatalf("correlated browser request and handler: %+v %v", projection, err)
	}
	partial, err := flowview.CorrelateObserved(ctx, request, artifacts[:2])
	if err != nil || len(partial.Graph.Nodes) != 1 || len(partial.Graph.Relations) != 0 || partial.UncorrelatedSpans != 1 {
		t.Fatalf("client-only export inferred backend execution: %+v %v", partial, err)
	}
	reader, err := NewExplorerReaderWithObservedRuns(backend, model.DefaultRuntimeConfig(),
		func(_ context.Context, actor app.CorePrincipal, pinned ScopeObservation, query model.FlowQueryRequest) (ObservedRunInput, error) {
			if actor != principal || pinned.Vector.VectorDigest != scope.Vector.VectorDigest ||
				query.ArtifactID == nil || *query.ArtifactID != run.artifactIDs[0] {
				return ObservedRunInput{}, ErrAuthority
			}
			return ObservedRunInput{Request: request, RelatedArtifacts: run.artifactIDs[1:],
				Resolve: func(context.Context, traces.Ref) (flowview.ObservedBinding, bool, error) {
					return flowview.ObservedBinding{}, false, nil
				},
				Verify: func(context.Context, flowview.ObservedBinding) error { return ErrAuthority }}, nil
		})
	if err != nil || !reader.SupportsObservedFlow() {
		t.Fatalf("observed owner configuration: %v", err)
	}
	mode := "observed"
	options := app.CoreCallOptions{RequestID: observedRequestID, DeliveryMode: "http", MaxOutputBytes: 16384}
	query := model.FlowQueryRequest{Scope: &model.ScopeRequest{PinnedVector: &scope.Vector},
		RootRef: root, Mode: &mode, ArtifactID: &run.artifactIDs[0]}
	if _, err := reader.Read(ctx, query, options); !errors.Is(err, app.ErrValidationAuthority) {
		t.Fatalf("browser trace borrowed unrelated Go source binding: %v", err)
	}
	changed := request
	changed.RequestID = observedRequestID
	if err := request.VerifyRequest(ctx, changed); !errors.Is(err, flowview.ErrObservedCorrelation) {
		t.Fatalf("changed receipt proof accepted: %v", err)
	}
	if run.site != nil {
		verifyPackagedObservedUI(t, ctx, backend, scope, principal, captured, request, run)
	}
	approved = false
	if _, err := flowview.CorrelateObserved(ctx, request, artifacts); !errors.Is(err, ErrAuthority) {
		t.Fatalf("withdrawn fixture receipt remained valid: %v", err)
	}
}

func verifyPackagedObservedUI(t *testing.T, ctx context.Context, backend *CoreBackend, scope ScopeObservation,
	principal app.CorePrincipal, captured *snapshot.Snapshot, request flowview.ObservedRequest, run *browserObserveRun) {
	t.Helper()
	member := scope.Vector.Members[0]
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	child := traces.Ref{ArtifactID: run.artifactIDs[2], TraceID: run.receipt.TraceID, SpanID: run.childID}
	root := &model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: "sendObserved"}
	bindings := make(map[traces.Ref]flowview.ObservedBinding, 2)
	for _, it := range []struct {
		ref                 traces.Ref
		symbol, role, plane string
	}{
		{request.RequestSpan, root.SymbolID, "http_request", "frontend"},
		{child, "captureObservedArrival", "http_handler", "backend"},
	} {
		name := []byte("function " + it.symbol)
		offset := bytes.Index(run.site.Bytes(), name)
		if offset < 0 {
			t.Fatalf("fixture symbol lacks captured source: %s", it.symbol)
		}
		start := int64(offset + len("function "))
		bindings[it.ref] = flowview.ObservedBinding{Span: it.ref, GraphRef: &model.SymbolRef{
			Kind: "symbol", GraphIdentity: identity, SymbolID: it.symbol,
		}, Source: model.SourceRef{WorktreeID: identity.WorktreeID, ProfileID: identity.ProfileID, GenerationID: identity.GenerationID,
			Path: observeSite, ContentHash: run.site.ContentHash(),
			Range: model.ByteRange{StartByte: start, EndByte: start + int64(len(it.symbol))}},
			Capture: captured, Role: it.role, Application: "fixture", Service: it.plane, Environment: "test",
			Revision: run.revision, BuildDigest: run.build, ProfileID: identity.ProfileID}
	}
	reader, err := NewExplorerReaderWithObservedRuns(backend, model.DefaultRuntimeConfig(),
		func(_ context.Context, actor app.CorePrincipal, selected ScopeObservation, query model.FlowQueryRequest) (ObservedRunInput, error) {
			if actor != principal || selected.Vector.VectorDigest != scope.Vector.VectorDigest ||
				query.ArtifactID == nil || *query.ArtifactID != request.RequestSpan.ArtifactID {
				return ObservedRunInput{}, ErrAuthority
			}
			return ObservedRunInput{Request: request, RelatedArtifacts: run.artifactIDs[1:],
				Resolve: func(_ context.Context, ref traces.Ref) (flowview.ObservedBinding, bool, error) {
					binding, ok := bindings[ref]
					return binding, ok, nil
				},
				Verify: func(ctx context.Context, binding flowview.ObservedBinding) error {
					if expected, found := bindings[binding.Span]; !found || expected != binding {
						return ErrAuthority
					}
					return request.VerifyRequest(ctx, request)
				}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	server, code, err := explorerapi.Listen(ctx, explorerapi.Options{Reader: reader, QueryTimeout: 3 * time.Second,
		BearerTTL: time.Minute, Assets: os.DirFS(os.Getenv("MCE4D_T104_EXPLORER_ASSETS"))})
	if err != nil {
		t.Fatal(err)
	}
	serve := make(chan error, 1)
	go func() { serve <- server.Serve(ctx) }()
	defer func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-serve; err != nil {
			t.Error(err)
		}
	}()
	browserContext, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	cmd := exec.CommandContext(browserContext, os.Getenv("MCE4D_T102_BROWSER_NODE"),
		"../../tests/acceptance/explorer_observed_ui.browser.mjs")
	cmd.Stdin = strings.NewReader(string(scopeJSON(t, map[string]string{
		"origin": server.Origin(), "code": code, "artifactID": request.RequestSpan.ArtifactID,
		"childArtifactID": child.ArtifactID, "symbol": root.SymbolID, "sourcePath": observeSite,
		"sourceHash": run.site.ContentHash(), "vectorDigest": scope.Vector.VectorDigest,
	})))
	report, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("packaged Explorer browser: %v\n%s", err, report)
	}
	t.Logf("%s", report)
}
