//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
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

const (
	observedInteraction = "11111111-1111-4111-8111-111111111111"
	observedRequestID   = "77777777-7777-4777-8777-777777777777"
	observedTraceID     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	observedRootID      = "bbbbbbbbbbbbbbbb"
	observedChildID     = "cccccccccccccccc"
	observedRevision    = "dddddddddddddddddddddddddddddddddddddddd"
)

func fixtureBuild() string { return strings.Repeat("e", 64) }

func stageObservedFixture(t *testing.T, ctx context.Context, base, recordsPath, worktree, profile string,
	lease *runtime.Lifecycle, writer *runtime.Writer) string {
	t.Helper()
	m := importtrace.Metadata{SchemaVersion: 1, Application: "fixture", Service: "frontend", Environment: "test",
		Revision: observedRevision, BuildDigest: fixtureBuild(), ProfileID: profile, Collector: "fixture",
		Instrumentation: "v1", WindowStart: "2026-10-01T01:00:00Z", WindowEnd: "2026-10-01T01:00:04Z",
		SampleRatePPM: 1000000, Sampling: "all"}
	body := scopeJSON(t, map[string]any{"metadata": m, "spans": []importtrace.Span{
		{SpanKey: importtrace.SpanKey{TraceID: observedTraceID, SpanID: observedRootID}, InteractionID: observedInteraction,
			Operation: "request", Status: "ok", StartedAt: "2026-10-01T01:00:01Z", EndedAt: "2026-10-01T01:00:03Z", Links: []importtrace.SpanKey{}},
		{SpanKey: importtrace.SpanKey{TraceID: observedTraceID, SpanID: observedChildID}, ParentSpanID: observedRootID,
			Operation: "handler", Status: "ok", StartedAt: "2026-10-01T01:00:01Z", EndedAt: "2026-10-01T01:00:02Z", Links: []importtrace.SpanKey{}},
	}})
	h := sha256.Sum256(body)
	pin := hex.EncodeToString(h[:])
	const rel = "approved.trace.json"
	if err := os.WriteFile(filepath.Join(base, rel), body, 0600); err != nil {
		t.Fatal(err)
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
	result, err := store.ImportTrace(ctx, records.TraceImportRequest{Root: source, RelativePath: rel,
		Writer: writer, JobID: scopeJob, AllowUpgrade: true,
		Authorize: func(ctx context.Context, identity records.Identity) error {
			if identity.WorktreeID != worktree {
				return ErrAuthority
			}
			return ctx.Err()
		},
		Approval: importtrace.Approval{InputHash: pin, Application: m.Application, Service: m.Service,
			Environment: m.Environment, Collector: m.Collector, Instrumentation: m.Instrumentation,
			Operations: []string{"request", "handler"},
			Recheck: func(ctx context.Context, root *paths.Root, file, id string) error {
				if root != source || file != rel || id != pin {
					return ErrAuthority
				}
				return ctx.Err()
			},
		}})
	if err != nil || result.ArtifactID != pin || result.SpanCount != 2 {
		t.Fatalf("import observed fixture: %+v %v", result, err)
	}
	return pin
}

func verifyObservedFixture(t *testing.T, ctx context.Context, backend *CoreBackend, scope ScopeObservation,
	capture *snapshot.Snapshot, source model.SourceRef, symbol, artifactID string) {
	t.Helper()
	root := &model.SymbolRef{Kind: "symbol", SymbolID: symbol,
		GraphIdentity: model.GraphIdentity{WorktreeID: source.WorktreeID, ProfileID: source.ProfileID, GenerationID: source.GenerationID}}
	request := model.FlowQueryRequest{Scope: &model.ScopeRequest{PinnedVector: &scope.Vector},
		RootRef: root, Mode: stringPointer("observed"), ArtifactID: &artifactID}
	options := app.CoreCallOptions{RequestID: observedRequestID, DeliveryMode: "http", MaxOutputBytes: 16384}
	ordinary, err := NewExplorerReader(backend, model.DefaultRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.Read(ctx, request, options); !errors.Is(err, app.ErrCoreUnsupported) {
		t.Fatalf("unconfigured observed mode was available: %v", err)
	}
	var approved atomic.Bool
	approved.Store(true)
	seed := traces.Ref{ArtifactID: artifactID, TraceID: observedTraceID, SpanID: observedRootID}
	owner := func(_ context.Context, _ app.CorePrincipal, current ScopeObservation, query model.FlowQueryRequest) (ObservedRunInput, error) {
		if current.Vector.VectorDigest != scope.Vector.VectorDigest || query.ArtifactID == nil || *query.ArtifactID != artifactID {
			return ObservedRunInput{}, ErrAuthority
		}
		return ObservedRunInput{
			Request: flowview.ObservedRequest{InteractionID: observedInteraction, RequestID: observedRequestID,
				RequestSpan: seed, BuildDigest: fixtureBuild(), TraceParent: "00-" + observedTraceID + "-" + observedRootID + "-01",
				VerifyRequest: func(ctx context.Context, r flowview.ObservedRequest) error {
					if !approved.Load() || r.RequestSpan != seed || r.InteractionID != observedInteraction || r.BuildDigest != fixtureBuild() {
						return ErrAuthority
					}
					return ctx.Err()
				}},
			Resolve: func(_ context.Context, ref traces.Ref) (flowview.ObservedBinding, bool, error) {
				role := "http_handler"
				if ref == seed {
					role = "http_request"
				}
				binding := flowview.ObservedBinding{Span: ref, GraphRef: root, Source: source, Capture: capture, Role: role,
					Application: "fixture", Service: "frontend", Environment: "test", Revision: observedRevision,
					BuildDigest: fixtureBuild(), ProfileID: source.ProfileID}
				return binding, ref == seed || ref.ArtifactID == artifactID && ref.TraceID == observedTraceID && ref.SpanID == observedChildID, nil
			},
			Verify: func(ctx context.Context, binding flowview.ObservedBinding) error {
				if !approved.Load() || binding.Source != source || binding.Revision != observedRevision || binding.BuildDigest != fixtureBuild() {
					return ErrAuthority
				}
				return ctx.Err()
			},
		}, nil
	}
	explorer, err := NewExplorerReaderWithObservedRuns(backend, model.DefaultRuntimeConfig(), owner)
	if err != nil {
		t.Fatal(err)
	}
	output, err := explorer.Read(ctx, request, options)
	if err != nil || output.Recheck == nil {
		t.Fatalf("observed flow read: %v", err)
	}
	if err := output.Recheck(ctx); err != nil {
		t.Fatalf("delivery authority: %v", err)
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := registry.DecodeResponse(contracts.HTTP, "POST /flow/query", output.Body, int(options.MaxOutputBytes), 1)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := wire.Data.(model.FlowData)
	if !ok || data.Mode != "observed" || len(data.Steps) != 2 || len(data.Edges) != 1 ||
		wire.Scope.VectorDigest != scope.Vector.VectorDigest || wire.Coverage.Status != "partial" {
		t.Fatalf("observed API projection: %+v", wire)
	}

	server, code, err := explorerapi.Listen(ctx, explorerapi.Options{Reader: explorer,
		QueryTimeout: 2 * time.Second, BearerTTL: time.Minute,
		Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Observe fixture</title>")}}})
	if err != nil {
		t.Fatal(err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx) }()
	defer func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-serveErr; err != nil {
			t.Error(err)
		}
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	post := func(path, bearer string, body []byte) (int, []byte) {
		in, err := http.NewRequestWithContext(ctx, http.MethodPost, server.Origin()+explorerapi.APIPath+path, strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		in.Header.Set("Origin", server.Origin())
		in.Header.Set("Content-Type", "application/json")
		in.Header.Set(explorerapi.CSRFHeader, explorerapi.CSRFValue)
		if bearer != "" {
			in.Header.Set("Authorization", "Bearer "+bearer)
		}
		out, err := client.Do(in)
		if err != nil {
			t.Fatal(err)
		}
		defer out.Body.Close()
		payload, err := io.ReadAll(io.LimitReader(out.Body, 262144))
		if err != nil {
			t.Fatal(err)
		}
		return out.StatusCode, payload
	}
	bootStatus, boot := post("/session/bootstrap", "", scopeJSON(t, model.BootstrapRequest{Code: code}))
	bootWire, err := registry.DecodeResponse(contracts.HTTP, "POST /session/bootstrap", boot, 262144, 1)
	if err != nil || bootStatus != http.StatusOK {
		t.Fatalf("Observe HTTP bootstrap status %d: %v", bootStatus, err)
	}
	session := bootWire.Data.(model.BootstrapData)
	if !slices.Contains(session.Capabilities, "observed_flow") || slices.Contains(session.Capabilities, "flow_query") {
		t.Fatalf("observed-only capabilities: %v", session.Capabilities)
	}
	body := scopeJSON(t, request)
	publishedStatus, published := post("/flow/query", session.Bearer, body)
	response, err := registry.DecodeResponse(contracts.HTTP, "POST /flow/query", published, 262144, 1)
	if err != nil || publishedStatus != http.StatusOK || response.Data.(model.FlowData).Mode != "observed" {
		t.Fatalf("observed-only route status %d: %v", publishedStatus, err)
	}
	approved.Store(false)
	if err := output.Recheck(ctx); err == nil {
		t.Fatal("revoked observed session kept delivery authority")
	}
	if revoked, _ := post("/flow/query", session.Bearer, body); revoked != http.StatusForbidden {
		t.Fatalf("observed route after revocation: %d", revoked)
	}
}
