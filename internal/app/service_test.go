package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/telemetry"
)

type coreAuthorityFixture struct {
	principal                     CorePrincipal
	requestChecks, responseChecks int
	revokeResponseAt              int
	denied                        bool
}

func (a *coreAuthorityFixture) Authenticate(context.Context) (CorePrincipal, error) {
	if a.denied {
		return CorePrincipal{}, errors.New("private authentication detail")
	}
	return a.principal, nil
}
func (a *coreAuthorityFixture) AuthorizeRequest(_ context.Context, _ CorePrincipal, _ string, request model.Request) error {
	a.requestChecks++
	// Mutating this callback's copy must not mutate the dispatched request.
	if locate, ok := request.(model.LocateRequest); ok && locate.Query != nil {
		*locate.Query = "mutated by authority"
	}
	return nil
}
func (a *coreAuthorityFixture) AuthorizeResponse(_ context.Context, _ CorePrincipal, _ string, _ model.Request, response model.Response) error {
	a.responseChecks++
	if response.Scope != nil && len(response.Scope.Members) > 0 {
		response.Scope.Members[0].RecordEpoch++
	}
	if a.revokeResponseAt > 0 && a.responseChecks >= a.revokeResponseAt {
		return errors.New("private scope detail")
	}
	return nil
}

type coreBackendFixture struct {
	f           *freshnessFixture
	pack        *FreshnessPack
	token       string
	lastName    string
	lastQuery   string
	lastRename  string
	err         error
	response    func(CoreCallOptions) ([][]byte, error)
	wait        bool
	deliveryErr error
}

func (b *coreBackendFixture) VerifyDelivery(context.Context, CorePrincipal, string, model.Request, model.Response) error {
	return b.deliveryErr
}

func (b *coreBackendFixture) run(ctx context.Context, name string, options CoreCallOptions) ([][]byte, error) {
	b.lastName = name
	if b.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if b.err != nil {
		return nil, b.err
	}
	if b.response != nil {
		return b.response(options)
	}
	return nil, ErrCoreUnsupported
}

func (b *coreBackendFixture) WorkspaceStatus(ctx context.Context, _ CorePrincipal, _ model.StatusRequest, o CoreCallOptions) ([][]byte, error) {
	return b.run(ctx, "status", o)
}
func (b *coreBackendFixture) Locate(ctx context.Context, _ CorePrincipal, r model.LocateRequest, o CoreCallOptions) ([][]byte, error) {
	if r.Query != nil {
		b.lastQuery = *r.Query
	}
	return b.run(ctx, "locate", o)
}
func (b *coreBackendFixture) ContextPack(ctx context.Context, _ CorePrincipal, _ model.PackRequest, o CoreCallOptions) ([][]byte, error) {
	if b.pack == nil || b.err != nil || b.response != nil || b.wait {
		return b.run(ctx, "pack", o)
	}
	b.lastName = "pack"
	page, err := b.pack.First(ctx, corePageOptions(o))
	return page.Blocks, err
}
func (b *coreBackendFixture) ExpandPack(ctx context.Context, p CorePrincipal, r model.ExpandRequest, o CoreCallOptions) ([][]byte, error) {
	if b.pack == nil || b.err != nil || b.response != nil || b.wait {
		return b.run(ctx, "expand", o)
	}
	b.lastName = "expand"
	page, err := b.pack.Continue(ctx, r.PackToken, r.NextCursor, p.Digest, b.f.input.Guard, corePageOptions(o))
	return page.Blocks, err
}
func (b *coreBackendFixture) ValidatePack(ctx context.Context, p CorePrincipal, r model.ValidateRequest, o CoreCallOptions) ([][]byte, error) {
	if b.pack == nil || b.err != nil || b.response != nil || b.wait {
		return b.run(ctx, "validate", o)
	}
	b.lastName = "validate"
	if r.PackToken == nil {
		return nil, ErrCoreUnsupported
	}
	validation, err := b.pack.Validate(ctx, *r.PackToken, p.Digest, b.f.input.Guard)
	if err != nil {
		return nil, err
	}
	response := coreTestEnvelope(o, &b.f.scope)
	response.Freshness, response.Data = validation.Freshness, validation.Data
	return coreTestBlocks(response)
}
func (b *coreBackendFixture) RenamePreview(ctx context.Context, _ CorePrincipal, r model.Request, o CoreCallOptions) ([][]byte, error) {
	switch r.(type) {
	case *model.RenameInitialRequest:
		b.lastRename = "initial"
	case *model.RenameContinueRequest:
		b.lastRename = "continuation"
	}
	return b.run(ctx, "rename", o)
}

func corePageOptions(o CoreCallOptions) contextpack.PackPageOptions {
	return contextpack.PackPageOptions{RequestID: o.RequestID, MaxOutputBytes: o.MaxOutputBytes, MaxItems: 1, DeliveryMode: o.DeliveryMode, VerifyStructuredHost: o.VerifyStructuredHost}
}

func coreTestEnvelope(o CoreCallOptions, scope *model.ScopeVector) model.Response {
	zero := int64(0)
	return model.Response{SchemaVersion: 1, RequestID: o.RequestID, Status: "ok", Scope: scope, Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}, Coverage: model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}, Readiness: "needs_context", Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero}, Budget: model.Budget{MaxOutputBytes: o.MaxOutputBytes, DeliveryMode: o.DeliveryMode}, Warnings: []model.Gap{}}
}

func coreTestBlocks(response model.Response) ([][]byte, error) {
	_, blocks, err := model.SettleSerializedBudget(response.Budget, func(size int64) ([][]byte, error) {
		response.Budget.SerializedBytes = size
		raw, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		if response.Budget.DeliveryMode == "mcp_dual" {
			return [][]byte{raw, append([]byte(nil), raw...)}, nil
		}
		return [][]byte{raw}, nil
	})
	return blocks, err
}

func coreServiceFixture(t *testing.T) (*CoreService, *coreAuthorityFixture, *coreBackendFixture, *telemetry.Metrics) {
	t.Helper()
	cfg := model.DefaultRuntimeConfig()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = model.DecodeRuntimeConfigJSON(raw, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := model.RuntimeConfigDigest(cfg, 65536)
	if err != nil {
		t.Fatal(err)
	}
	config := &RuntimeConfigService{bootConfig: cfg, bootDigest: digest}
	auth := &coreAuthorityFixture{principal: CorePrincipal{ID: "fixture-actor", Digest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64)}}
	backend := &coreBackendFixture{}
	metrics := telemetry.NewMetrics()
	service, err := NewCoreService(config, auth, backend, metrics)
	if err != nil {
		t.Fatal(err)
	}
	return service, auth, backend, metrics
}

func coreTestOptions() CoreCallOptions {
	return CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "mcp_dual"}
}

func decodeCoreResult(t *testing.T, name string, result CoreCallResult) model.Response {
	t.Helper()
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Blocks) == 0 {
		t.Fatal("missing response block")
	}
	response, err := registry.DecodeResponse(contracts.MCP, name, result.Blocks[0], 131072, 4)
	if err != nil || model.ValidateDeliveredBytes(response.Budget, result.Blocks) != nil {
		t.Fatalf("invalid response/accounting: %v", err)
	}
	return response
}

func TestCoreServiceDispatchesTypedStatusAndLocate(t *testing.T) {
	service, auth, backend, metrics := coreServiceFixture(t)
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, nil)
		response.Data = model.StatusData{Operation: "status", Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "missing", RegisteredScopes: []model.RegisteredScopeStatus{}}
		return coreTestBlocks(response)
	}
	result, err := service.Call(context.Background(), "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || result.IsError || backend.lastName != "status" || auth.requestChecks != 3 || auth.responseChecks != 2 {
		t.Fatalf("status dispatch: %v", err)
	}
	response := decodeCoreResult(t, "mce4d_workspace_status", result)
	if response.Data.(model.StatusData).IndexState != "missing" || !bytes.Equal(result.Blocks[0], result.Blocks[1]) {
		t.Fatal("status wire or dual payload mismatch")
	}
	f := newFreshnessFixture(t, "analysis_snapshot")
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, &f.scope)
		member := f.scope.Members[0]
		ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: f.live.Path(), ContentHash: f.live.ContentHash(), Range: model.ByteRange{StartByte: 15, EndByte: 21}}
		response.Data = model.LocateData{Operation: "locate", Candidates: []model.Candidate{{Ref: &model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SymbolID: "target"}, Name: "Target", QualifiedName: "p.Target", SourceRefs: []model.SourceRef{ref}, EvidenceRefs: []model.EvidenceRef{}}}}
		return coreTestBlocks(response)
	}
	result, err = service.Call(context.Background(), "mce4d_locate", []byte(`{"query":"Target"}`), coreTestOptions())
	if err != nil || result.IsError || backend.lastName != "locate" || backend.lastQuery != "Target" {
		t.Fatalf("locate dispatch or request copy: %v", err)
	}
	response = decodeCoreResult(t, "mce4d_locate", result)
	if response.Scope.Members[0].RecordEpoch != 1 || len(response.Data.(model.LocateData).Candidates) != 1 {
		t.Fatal("authority callback mutated response")
	}
	if metrics.Snapshot().Measurements[telemetry.OutputBytes].Observations != 2 {
		t.Fatal("successful output sizes were not measured")
	}
}

func TestCoreServicePackExpandAndValidateUseRealPagedPack(t *testing.T) {
	service, _, backend, _ := coreServiceFixture(t)
	f := newFreshnessFixture(t, "analysis_snapshot")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	backend.f, backend.pack, backend.token = f, pack, token
	result, err := service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || result.IsError {
		t.Fatalf("pack dispatch: %v", err)
	}
	response := decodeCoreResult(t, "mce4d_context_pack", result)
	if response.Freshness.Status != "not_checked" || response.Pagination.NextCursor == nil {
		t.Fatal("analysis page lost snapshot status or cursor")
	}
	expand, _ := json.Marshal(model.ExpandRequest{PackToken: token, NextCursor: *response.Pagination.NextCursor})
	result, err = service.Call(f.ctx, "mce4d_expand_pack", expand, coreTestOptions())
	if err != nil || result.IsError || backend.lastName != "expand" {
		t.Fatalf("expand dispatch: %v", err)
	}
	if got := decodeCoreResult(t, "mce4d_expand_pack", result); got.Data.(model.PackData).PackToken != token || got.Freshness.Status != "not_checked" {
		t.Fatal("continuation was rebound")
	}
	validate, _ := json.Marshal(model.ValidateRequest{PackToken: &token})
	result, err = service.Call(f.ctx, "mce4d_validate_pack", validate, coreTestOptions())
	if err != nil || result.IsError || backend.lastName != "validate" {
		t.Fatalf("validation dispatch: %v", err)
	}
	if got := decodeCoreResult(t, "mce4d_validate_pack", result); !got.Data.(model.ValidateData).EditPreconditionValidated || got.Freshness.Scope != "workspace" || f.fullChecks != 1 {
		t.Fatal("validation did not perform full workspace check")
	}
}

func TestCoreServiceSanitizesErrorsAndRejectsBadOutput(t *testing.T) {
	service, auth, backend, _ := coreServiceFixture(t)
	backend.err = fmt.Errorf("private provider content: %w", policy.ErrSecretBlocked)
	result, err := service.Call(context.Background(), "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || !result.IsError || bytes.Contains(result.Blocks[0], []byte("private")) {
		t.Fatalf("backend error leaked: %v", err)
	}
	if got := decodeCoreResult(t, "mce4d_workspace_status", result); got.Error.Code != "SECRET_BLOCKED" || got.Data != nil {
		t.Fatal("wrong sanitized error mapping")
	}
	backend.err = nil
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, nil)
		response.Data = model.StatusData{Operation: "status", Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "missing", RegisteredScopes: []model.RegisteredScopeStatus{}}
		blocks, err := coreTestBlocks(response)
		blocks[1] = append(blocks[1], ' ')
		return blocks, err
	}
	result, err = service.Call(context.Background(), "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("mismatched dual output accepted: %v", err)
	}
	auth.denied = true
	result, err = service.Call(context.Background(), "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "AUTH_REQUIRED" || bytes.Contains(result.Blocks[0], []byte("private")) {
		t.Fatalf("authentication failure leaked: %v", err)
	}
}

func TestCoreServiceRejectsMalformedRequestsAndCancellation(t *testing.T) {
	for name, raw := range map[string][]byte{
		"malformed":  []byte(`{"verify":`),
		"duplicate":  []byte(`{"verify":"none","verify":"workspace"}`),
		"unknown":    []byte(`{"unrecognized":"PRIVATE_TEST_MARKER"}`),
		"null":       []byte(`{"verify":null}`),
		"trailing":   []byte(`{} {}`),
		"over_limit": []byte(strings.Repeat(" ", 32769)),
	} {
		t.Run(name, func(t *testing.T) {
			service, _, backend, _ := coreServiceFixture(t)
			result, err := service.Call(context.Background(), "mce4d_workspace_status", raw, coreTestOptions())
			if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "INVALID_ARGUMENT" || backend.lastName != "" || bytes.Contains(result.Blocks[0], []byte("PRIVATE_TEST_MARKER")) {
				t.Fatalf("invalid request reached backend or leaked input: %v", err)
			}
		})
	}
	service, _, backend, _ := coreServiceFixture(t)
	backend.wait = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	result, err := service.Call(ctx, "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "CANCELLED" {
		t.Fatalf("cancelled request did not settle correctly: %v", err)
	}
}

func TestCoreServiceReportsUnknownCountsWhenContextCannotContinue(t *testing.T) {
	service, _, backend, _ := coreServiceFixture(t)
	backend.err = contextpack.ErrContinuationUnavailable
	const name = "mce4d_context_pack"
	raw := []byte(`{"selector":{"kind":"symbol","symbol_id":"target"},"task_kind":"behavior_change"}`)
	result, err := service.Call(context.Background(), name, raw, coreTestOptions())
	if err != nil || !result.IsError || backend.lastName != "pack" {
		t.Fatalf("incomplete context was not returned as an error: %v", err)
	}
	response := decodeCoreResult(t, name, result)
	p := response.Pagination
	if response.Error.Code != "CONTEXT_INCOMPLETE" || response.Data != nil || p.EnumerationComplete ||
		!p.Truncated || p.RequiredPending != nil || p.OptionalPending != nil || p.NextCursor != nil ||
		p.LimitReason == nil || *p.LimitReason != "CONTINUATION_UNAVAILABLE" {
		t.Fatalf("incomplete traversal claimed an enumerated result: %+v", response)
	}
}

func TestCoreServiceRejectsExpiredResultBeforeDelivery(t *testing.T) {
	service, _, backend, _ := coreServiceFixture(t)
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, nil)
		response.Data = model.StatusData{Operation: "status", Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "missing", RegisteredScopes: []model.RegisteredScopeStatus{}}
		return coreTestBlocks(response)
	}
	backend.deliveryErr = contextpack.ErrPackExpired
	result, err := service.Call(context.Background(), "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "CURSOR_EXPIRED" {
		t.Fatalf("expired delivery was released: %v", err)
	}
}

func TestCoreServiceLateRevocationAndDeadline(t *testing.T) {
	service, auth, backend, _ := coreServiceFixture(t)
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, nil)
		response.Data = model.StatusData{Operation: "status", Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "missing", RegisteredScopes: []model.RegisteredScopeStatus{}}
		return coreTestBlocks(response)
	}
	auth.revokeResponseAt = 2
	result, err := service.Call(context.Background(), "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "ACCESS_DENIED" || bytes.Contains(result.Blocks[0], []byte("private")) {
		t.Fatalf("late revocation returned source data: %v", err)
	}
	auth.revokeResponseAt = 0
	backend.response, backend.wait = nil, true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err = service.Call(ctx, "mce4d_workspace_status", []byte(`{}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "TIMEOUT" {
		t.Fatalf("deadline did not propagate: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err = service.Call(ctx, "mce4d_workspace_status", []byte(`{"verify":"workspace"}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_workspace_status", result).Error.Code != "FRESHNESS_UNKNOWN" {
		t.Fatalf("workspace verification timeout claimed a result: %v", err)
	}
}

func TestCoreServiceRenameInitialAndContinuationAreTyped(t *testing.T) {
	service, _, backend, _ := coreServiceFixture(t)
	f := newFreshnessFixture(t, "analysis_snapshot")
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, &f.scope)
		response.Data = model.RenameData{Operation: "rename_preview", Applied: false, SourceMutationCount: 0, ValidationRequired: true, Edits: []model.ProposedEdit{}, Conflicts: []model.Gap{}, PreviewComplete: true, ResultToken: "fixture-token", ResultID: "66666666-6666-4666-8666-666666666666", ResultDigest: strings.Repeat("f", 64), TotalEdits: 0, ComputationComplete: true}
		return coreTestBlocks(response)
	}
	initial := model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: f.live.Path(), ContentHash: f.live.ContentHash(), ByteOffset: 15}, NewName: "Changed", PreserveContracts: true}
	raw, _ := json.Marshal(initial)
	result, err := service.Call(f.ctx, "mce4d_rename_preview", raw, coreTestOptions())
	if err != nil || result.IsError || backend.lastRename != "initial" {
		t.Fatalf("initial rename dispatch: %v", err)
	}
	if data := decodeCoreResult(t, "mce4d_rename_preview", result).Data.(model.RenameData); data.Applied || !data.ValidationRequired || data.SourceMutationCount != 0 {
		t.Fatal("rename preview did not retain read-only semantics")
	}
	continuation := model.RenameContinueRequest{ResultToken: "fixture-token", Cursor: "fixture-cursor"}
	raw, _ = json.Marshal(continuation)
	result, err = service.Call(f.ctx, "mce4d_rename_preview", raw, coreTestOptions())
	if err != nil || result.IsError || backend.lastRename != "continuation" {
		t.Fatalf("rename continuation dispatch: %v", err)
	}
}

func TestCoreServiceRejectsCrossScopeAndMisleadingSnapshot(t *testing.T) {
	service, _, backend, _ := coreServiceFixture(t)
	f := newFreshnessFixture(t, "analysis_snapshot")
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, &f.scope)
		member := f.scope.Members[0]
		ref := model.SourceRef{WorktreeID: "99999999-9999-4999-8999-999999999999", ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: f.live.Path(), ContentHash: f.live.ContentHash(), Range: model.ByteRange{StartByte: 15, EndByte: 21}}
		response.Data = model.LocateData{Operation: "locate", Candidates: []model.Candidate{{Ref: &model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SymbolID: "target"}, Name: "Target", QualifiedName: "p.Target", SourceRefs: []model.SourceRef{ref}, EvidenceRefs: []model.EvidenceRef{}}}}
		return coreTestBlocks(response)
	}
	result, err := service.Call(f.ctx, "mce4d_locate", []byte(`{"query":"Target"}`), coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_locate", result).Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("cross-scope source leaked: %v", err)
	}
	backend.response = func(o CoreCallOptions) ([][]byte, error) {
		response := coreTestEnvelope(o, &f.scope)
		response.Freshness.Scope = "workspace"
		member := f.scope.Members[0]
		response.Data = model.PackData{Operation: "context_pack", PackToken: "fixture-token", TargetRefs: []model.GraphRef{&model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SymbolID: "target"}}, Items: []model.EvidenceItem{}, EditPreconditionValidated: false}
		return coreTestBlocks(response)
	}
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("analysis snapshot claimed workspace freshness: %v", err)
	}
}

func TestCoreServiceStructuredHostAndTimeoutModes(t *testing.T) {
	service, _, backend, _ := coreServiceFixture(t)
	options := coreTestOptions()
	options.DeliveryMode = "mcp_structured_verified"
	options.VerifyStructuredHost = func(context.Context) error { return errors.New("private host detail") }
	if result, err := service.Call(context.Background(), "mce4d_workspace_status", []byte(`{}`), options); !errors.Is(err, ErrValidationAuthority) || len(result.Blocks) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("unverified structured host got a payload: %v", err)
	}
	backend.wait = true
	for _, test := range []struct{ mode, code string }{{"analysis_snapshot", "TIMEOUT"}, {"checked_workspace", "FRESHNESS_UNKNOWN"}} {
		f := newFreshnessFixture(t, test.mode)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		result, err := service.Call(ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
		cancel()
		if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != test.code {
			t.Fatalf("%s timeout mapped incorrectly: %v", test.mode, err)
		}
	}
}
