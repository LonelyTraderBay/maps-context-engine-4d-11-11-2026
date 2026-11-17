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

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type freshnessFixture struct {
	ctx        context.Context
	input      FreshnessPackInput
	scope      model.ScopeVector
	live       snapshot.File
	changed    bool
	denied     bool
	revoked    bool
	fullChecks int
	liveChecks int
	authorize  int
	factChecks int
}

func newFreshnessFixture(t *testing.T, mode string) *freshnessFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "22222222-2222-4222-8222-222222222222"
	profile := strings.Repeat("a", 64)
	conf := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/synthetic"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 65536, MaxSourceBytes: 1024 * 1024, MaxOutputBytes: 65536, SecretRulesVersion: "test"}
	policyDigest, err := model.PolicyDigest(conf, 65536)
	if err != nil {
		t.Fatal(err)
	}
	const body = "package p\nfunc Target() { Target() }\n"
	file, err := snapshot.NewFile("main.go", []byte(body), 65536)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profile, PolicyDigest: policyDigest, ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{{Path: file.Path(), Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest = captured.Manifest()
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: profile, GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: policyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	path, pkg, target := "main.go", "pkg", "target"
	declaration := int64(strings.Index(body, "Target"))
	call := int64(strings.LastIndex(body, "Target"))
	relations := []model.RelationFact{}
	for i := 0; i < 24; i++ {
		relations = append(relations, model.RelationFact{RelationID: fmt.Sprintf("call-%02d", i), SourceSymbolID: target, TargetSymbolID: &target, Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: path, Range: model.ByteRange{StartByte: call, EndByte: call + 6}})
	}
	f := &freshnessFixture{ctx: ctx, scope: scope, live: file}
	selection, err := contextpack.Select(ctx, contextpack.Input{Scope: scope, Target: model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: worktree, ProfileID: profile, GenerationID: generation}, SymbolID: target}, Snapshot: captured,
		Packages: []model.PackageFact{{PackageKey: pkg, ImportPath: "fixture/p", ModuleKey: "module", Variant: "production", InputDigest: manifest.SnapshotDigest, SemanticStatus: "partial"}},
		Symbols:  []model.SymbolFact{{SymbolID: target, Path: &path, PackageKey: &pkg, Kind: "function", Name: "Target", QualifiedName: "p.Target", ScopeKey: pkg, Range: &model.ByteRange{StartByte: declaration, EndByte: declaration + 6}}}, Relations: relations,
		Coverage: model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{{Code: "FIXTURE_PARTIAL", Reason: "Fixture coverage is partial.", SourceRefs: []model.SourceRef{}}}, ScopeDescription: "workspace"}, Scanner: scanner,
		AuthorizeScope: func(context.Context, model.ScopeVector, model.ScopeMember) error { return nil }, AuthorizePath: func(context.Context, model.ScopeMember, string) error { return nil }, VerifyFacts: func(context.Context, model.ScopeMember, string) error { f.factChecks++; return nil },
		MaxManifestBytes: 65536, MaxFactBytes: 65536, MaxSourceBytes: 1024 * 1024, MaxFacts: 100, MaxNodes: 10, MaxEdges: 100, MaxDepth: 2, MaxOutputBytes: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	slice, err := contextpack.BuildTaskSlice(ctx, contextpack.SliceInput{Selection: selection, Policy: conf, TaskKind: "rename", VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }, MaxBytes: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	request := model.PackRequest{Selector: &model.SymbolSelector{Kind: "symbol", SymbolID: target}, TaskKind: "rename", FreshnessMode: mode}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := model.RequestDigest("context_pack", "fixture-actor", "PackRequest", request, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error {
		if f.revoked {
			return errors.New("sensitive authority detail")
		}
		return nil
	}
	validation := ValidateCapturedInput{Scope: scope, MaxInputBytes: 65536, MaxManifestBytes: 65536, CurrentScope: func(context.Context) (model.ScopeVector, error) { return f.scope, nil }, ResolveCapture: func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error) { return captured, nil },
		AuthorizeSource: func(context.Context, model.ScopeMember, model.SourceRef) error {
			f.authorize++
			if f.denied {
				return errors.New("private source detail")
			}
			return nil
		},
		VerifyWorkspace: func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
			f.fullChecks++
			if f.changed {
				return WorkspaceChanged, nil
			}
			return WorkspaceObservedCurrent, nil
		}}
	f.input = FreshnessPackInput{Slice: slice, Codec: codec, Binding: contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64), RequestDigest: digest, OrderVersion: "pack-v1", Scope: scope}, Guard: guard, RequestJSON: requestJSON, PrincipalID: "fixture-actor", Validation: validation, RequestLimit: 65536,
		CaptureLive: func(context.Context, model.ScopeMember, string) (snapshot.File, error) {
			f.liveChecks++
			return f.live, nil
		}}
	return f
}

func freshnessOptions() contextpack.PackPageOptions {
	return contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "mcp_dual"}
}

func requireFreshnessPage(t *testing.T, page contextpack.PackPage, status, scope string) {
	t.Helper()
	data, ok := page.Response.Data.(model.PackData)
	if !ok || data.EditPreconditionValidated || page.Response.Readiness != "needs_context" || page.Response.Freshness.Status != status || page.Response.Freshness.Scope != scope || page.Response.Freshness.SourceView != "indexed_snapshot" || len(page.Blocks) != 2 || !bytes.Equal(page.Blocks[0], page.Blocks[1]) || model.ValidateDeliveredBytes(page.Response.Budget, page.Blocks) != nil {
		t.Fatalf("invalid page freshness or accounting: %+v", page.Response.Freshness)
	}
	var decoded map[string]any
	if json.Unmarshal(page.Blocks[0], &decoded) != nil {
		t.Fatal("invalid wire page")
	}
}

func TestFreshnessPackAnalysisDoesNotCacheLiveVerdicts(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pack.First(f.ctx, freshnessOptions())
	if err != nil {
		t.Fatal(err)
	}
	requireFreshnessPage(t, page, "not_checked", "none")
	if page.Response.Pagination.NextCursor == nil {
		t.Fatal("fixture needs continuation")
	}
	cursor := *page.Response.Pagination.NextCursor
	f.changed = true
	page, err = pack.Continue(f.ctx, token, cursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions())
	if err != nil {
		t.Fatal(err)
	}
	requireFreshnessPage(t, page, "not_checked", "none")
	if f.fullChecks != 0 || f.liveChecks != 0 {
		t.Fatal("analysis pages performed live workspace verification")
	}
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrFreshnessChanged) || result.Data.EditPreconditionValidated {
		t.Fatalf("stale workspace was approved: %v", err)
	}
	f.changed = false
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); err != nil || !result.Data.EditPreconditionValidated || result.Freshness.Status != "observed_current" {
		t.Fatalf("fresh workspace was not observed: %v", err)
	}
	f.changed = true
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrFreshnessChanged) || result.Data.EditPreconditionValidated || f.fullChecks != 3 {
		t.Fatal("cached live validation was reused")
	}
}

func TestFreshnessPackCheckedModeRechecksPagesAndFullWorkspace(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pack.First(f.ctx, freshnessOptions())
	if err != nil {
		t.Fatal(err)
	}
	requireFreshnessPage(t, page, "observed_current", "workspace")
	if f.fullChecks != 1 || page.Response.Pagination.NextCursor == nil {
		t.Fatal("first checked page did not run full validation")
	}
	cursor := *page.Response.Pagination.NextCursor
	page, err = pack.Continue(f.ctx, token, cursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions())
	if err != nil {
		t.Fatal(err)
	}
	if f.liveChecks != 2 || f.fullChecks != 1 {
		t.Fatalf("continuation did not verify returned file twice: live=%d full=%d", f.liveChecks, f.fullChecks)
	}
	requireFreshnessPage(t, page, "observed_current", "returned_files")
	if page.Response.Pagination.NextCursor == nil {
		t.Fatal("fixture needs another continuation")
	}
	changed, err := snapshot.NewFile("main.go", []byte("package p\nfunc Changed() { Target() }\n"), 65536)
	if err != nil {
		t.Fatal(err)
	}
	f.live = changed
	if got, err := pack.Continue(f.ctx, token, *page.Response.Pagination.NextCursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions()); !errors.Is(err, ErrFreshnessChanged) || got.Response.Data != nil {
		t.Fatalf("changed returned file leaked page: %v", err)
	}
	f.changed = true
	if got, err := pack.First(f.ctx, freshnessOptions()); !errors.Is(err, ErrFreshnessChanged) || got.Response.Data != nil || f.fullChecks != 2 {
		t.Fatal("cached first-page freshness was reused")
	}
}

func TestFreshnessPackRejectsChangedScopeRevocationAndWrongRequest(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	f.revoked = true
	before := f.factChecks
	if pack, _, err := NewFreshnessPack(f.ctx, f.input); !errors.Is(err, ErrValidationAuthority) || pack != nil || f.factChecks != before || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("principal was not checked before reading the slice: %v", err)
	}
	f.revoked = false
	wrong := f.input
	request, err := model.DecodePackRequestJSON(wrong.RequestJSON, wrong.RequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	request.FreshnessMode = "checked_workspace"
	wrong.RequestJSON, _ = json.Marshal(request)
	if pack, _, err := NewFreshnessPack(f.ctx, wrong); err == nil || pack != nil {
		t.Fatal("mode not bound to request digest")
	}
	wrong = f.input
	request, err = model.DecodePackRequestJSON(wrong.RequestJSON, wrong.RequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	request.Selector = &model.SymbolSelector{Kind: "symbol", SymbolID: "other"}
	wrong.RequestJSON, _ = json.Marshal(request)
	wrong.Binding.RequestDigest, _ = model.RequestDigest("context_pack", wrong.PrincipalID, "PackRequest", request, wrong.RequestLimit, 4)
	if pack, _, err := NewFreshnessPack(f.ctx, wrong); err == nil || pack != nil {
		t.Fatal("request selector did not match selected target")
	}
	wrong = f.input
	request, err = model.DecodePackRequestJSON(wrong.RequestJSON, wrong.RequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	otherWorktree := "99999999-9999-4999-8999-999999999999"
	profile := f.scope.Members[0].ProfileID
	request.Scope = &model.ScopeRequest{WorktreeID: &otherWorktree, ProfileID: &profile}
	wrong.RequestJSON, _ = json.Marshal(request)
	wrong.Binding.RequestDigest, _ = model.RequestDigest("context_pack", wrong.PrincipalID, "PackRequest", request, wrong.RequestLimit, 4)
	if pack, _, err := NewFreshnessPack(f.ctx, wrong); !errors.Is(err, ErrValidationPrecondition) || pack != nil {
		t.Fatalf("different requested worktree was accepted: %v", err)
	}
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := pack.Validate(f.ctx, token+"x", f.input.Binding.PrincipalDigest, f.input.Guard); err == nil || result.Data.EditPreconditionValidated {
		t.Fatal("invalid token accepted")
	}
	f.denied = true
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrValidationAuthority) || result.Data.EditPreconditionValidated || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked source leaked: %v", err)
	}
	f.denied = false
	f.scope.Members = append([]model.ScopeMember(nil), f.scope.Members...)
	f.scope.Members[0].RecordEpoch++
	f.scope.VectorDigest, _ = model.ComputeScopeVectorDigest(f.scope, 65536)
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrValidationScopeChanged) || result.Data.EditPreconditionValidated {
		t.Fatalf("changed record epoch accepted: %v", err)
	}
	f.revoked = true
	if page, err := pack.First(f.ctx, freshnessOptions()); !errors.Is(err, ErrValidationAuthority) || page.Response.Data != nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("revoked principal got a page or private error: %v", err)
	}
}

func TestFreshnessPackAnalysisWithoutLiveAdapters(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	f.input.CaptureLive = nil
	f.input.Validation.CurrentScope = nil
	f.input.Validation.ResolveCapture = nil
	f.input.Validation.AuthorizeSource = nil
	f.input.Validation.VerifyWorkspace = nil
	f.input.Validation.Scope = model.ScopeVector{}
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pack.First(f.ctx, freshnessOptions())
	if err != nil {
		t.Fatal(err)
	}
	requireFreshnessPage(t, page, "not_checked", "none")
	if result, err := pack.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrFreshnessUnknown) || result.Data.EditPreconditionValidated {
		t.Fatalf("missing live authority was treated as validated: %v", err)
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if result, err := pack.Validate(expired, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrFreshnessUnknown) || result.Data.EditPreconditionValidated {
		t.Fatalf("expired validation did not report unknown: %v", err)
	}
	checked := newFreshnessFixture(t, "checked_workspace")
	checked.input.CaptureLive = nil
	if result, _, err := NewFreshnessPack(checked.ctx, checked.input); result != nil || !errors.Is(err, ErrFreshnessUnknown) {
		t.Fatalf("checked mode accepted missing live adapter: %v", err)
	}
}

func TestFreshnessPackResolvesPositionAndHonorsInitialBudget(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	r, err := model.DecodePackRequestJSON(f.input.RequestJSON, f.input.RequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	r.Selector = &model.PositionSelector{Kind: "source_position", Path: f.live.Path(), ContentHash: f.live.ContentHash(), ByteOffset: 0}
	max := int64(12288)
	r.MaxOutputBytes = &max
	f.input.RequestJSON, _ = json.Marshal(r)
	f.input.Binding.RequestDigest, err = model.RequestDigest("context_pack", f.input.PrincipalID, "PackRequest", r, f.input.RequestLimit, 4)
	if err != nil {
		t.Fatal(err)
	}
	if pack, _, err := NewFreshnessPack(f.ctx, f.input); pack != nil || !errors.Is(err, ErrValidationAuthority) {
		t.Fatal("position was guessed without a resolver")
	}
	member := f.scope.Members[0]
	f.input.ResolvePosition = func(_ context.Context, _ model.PositionSelector, scope model.ScopeVector, _ *string) (model.SymbolRef, error) {
		scope.Members[0].RecordEpoch++ // callback cannot mutate the pinned copy
		return model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SymbolID: "other"}, nil
	}
	if pack, _, err := NewFreshnessPack(f.ctx, f.input); pack != nil || !errors.Is(err, ErrValidationPrecondition) {
		t.Fatal("mismatched resolved position was accepted")
	}
	f.input.ResolvePosition = func(_ context.Context, _ model.PositionSelector, scope model.ScopeVector, _ *string) (model.SymbolRef, error) {
		scope.Members[0].RecordEpoch++
		return model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SymbolID: "target"}, nil
	}
	pack, _, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	options := freshnessOptions()
	options.DeliveryMode = "http"
	page, err := pack.First(f.ctx, options)
	if err != nil || page.Response.Budget.MaxOutputBytes != max {
		t.Fatalf("initial request budget was not honored: %v", err)
	}
	if page.Response.Freshness.Status != "not_checked" || page.Response.Freshness.Scope != "none" || len(page.Blocks) != 1 || model.ValidateDeliveredBytes(page.Response.Budget, page.Blocks) != nil {
		t.Fatal("bounded HTTP page was not accounted for")
	}
}

func TestFreshnessPackRechecksStructuredHostAfterWorkspace(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	pack, _, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	options := freshnessOptions()
	options.DeliveryMode = "mcp_structured_verified"
	checks := 0
	options.VerifyStructuredHost = func(context.Context) error {
		checks++
		if checks >= 3 {
			return errors.New("private host detail")
		}
		return nil
	}
	if page, err := pack.First(f.ctx, options); !errors.Is(err, ErrValidationAuthority) || page.Response.Data != nil || strings.Contains(err.Error(), "private") || f.fullChecks != 1 {
		t.Fatalf("late host revocation leaked a page: %v checks=%d", err, checks)
	}
}

func TestFreshnessPackRejectsRevocationAfterFullCheck(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	f.input.Validation.VerifyWorkspace = func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
		f.revoked = true
		return WorkspaceObservedCurrent, nil
	}
	pack, _, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := pack.First(f.ctx, freshnessOptions()); !errors.Is(err, ErrValidationAuthority) || page.Response.Data != nil {
		t.Fatalf("late revocation returned an observed-current page: %v", err)
	}
}

func TestFreshnessPackDefaultAndReturnedFileRaces(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	// The wire request omits freshness_mode: edit-oriented tasks must default
	// to checked_workspace and the default is part of the request digest.
	f.input.RequestJSON = []byte(`{"selector":{"kind":"symbol","symbol_id":"target"},"task_kind":"rename"}`)
	request, err := model.DecodePackRequestJSON(f.input.RequestJSON, f.input.RequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	f.input.Binding.RequestDigest, err = model.RequestDigest("context_pack", f.input.PrincipalID, "PackRequest", request, f.input.RequestLimit, 4)
	if err != nil {
		t.Fatal(err)
	}
	modified, err := snapshot.NewFile("main.go", []byte("package p\nfunc Alterd() { Target() }\n"), 65536)
	if err != nil {
		t.Fatal(err)
	}
	f.input.CaptureLive = func(context.Context, model.ScopeMember, string) (snapshot.File, error) {
		f.liveChecks++
		if f.liveChecks == 1 {
			return f.live, nil
		}
		return modified, nil
	}
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pack.First(f.ctx, freshnessOptions())
	if err != nil || f.fullChecks != 1 || page.Response.Pagination.NextCursor == nil {
		t.Fatalf("default mode did not check workspace: %v", err)
	}
	if got, err := pack.Continue(f.ctx, token, *page.Response.Pagination.NextCursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions()); !errors.Is(err, ErrFreshnessChanged) || got.Response.Data != nil {
		t.Fatalf("change between returned-file reads leaked a page: %v", err)
	}
}

func TestFreshnessPackRejectsChangedScopeBetweenPages(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pack.First(f.ctx, freshnessOptions())
	if err != nil || page.Response.Pagination.NextCursor == nil {
		t.Fatalf("missing initial page: %v", err)
	}
	f.scope.Members = append([]model.ScopeMember(nil), f.scope.Members...)
	f.scope.Members[0].RecordEpoch++
	f.scope.VectorDigest, _ = model.ComputeScopeVectorDigest(f.scope, 65536)
	if got, err := pack.Continue(f.ctx, token, *page.Response.Pagination.NextCursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions()); !errors.Is(err, ErrValidationScopeChanged) || got.Response.Data != nil {
		t.Fatalf("changed scope returned a continuation: %v", err)
	}
}

func TestFreshnessSourceCollectorBoundsAndWarning(t *testing.T) {
	c := newSourceCollector(nil, 1024*1024)
	ref := model.SourceRef{WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64), GenerationID: "22222222-2222-4222-8222-222222222222", ContentHash: strings.Repeat("b", 64)}
	for i := 0; i < 256; i++ {
		ref.Path = fmt.Sprintf("src/%03d.go", i)
		if err := c.add(ref); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.add(ref); err != nil {
		t.Fatal("duplicate source consumed another slot")
	}
	ref.ContentHash = strings.Repeat("c", 64)
	if err := c.add(ref); !errors.Is(err, ErrValidationPrecondition) {
		t.Fatal("conflicting file hashes accepted")
	}
	ref.Path = "src/256.go"
	if err := c.add(ref); !errors.Is(err, ErrFreshnessUnknown) {
		t.Fatal("source cap did not fail closed")
	}
	response := model.Response{Warnings: []model.Gap{{Code: "LIVE_FRESHNESS_NOT_CHECKED", Reason: "The captured source is historical.", SourceRefs: []model.SourceRef{}}}}
	refreshPageWarnings(&response)
	if response.Warnings[0].Code != "LIVE_FRESHNESS_OBSERVATION_ONLY" || !strings.Contains(response.Warnings[0].Reason, "before editing") {
		t.Fatal("checked page retained a misleading stale warning")
	}
}

func TestFreshnessRequestScopeMatchesPinnedAndGroup(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	member := f.scope.Members[0]
	if !requestMatchesScope(&model.ScopeRequest{PinnedVector: &f.scope}, f.scope, member, 65536) {
		t.Fatal("matching pinned vector rejected")
	}
	group := "66666666-6666-4666-8666-666666666666"
	revision := int64(1)
	federated := f.scope
	federated.Kind = "federated"
	federated.GroupID, federated.GroupRevision = &group, &revision
	federated.VectorDigest, _ = model.ComputeScopeVectorDigest(federated, 65536)
	if !requestMatchesScope(&model.ScopeRequest{GroupID: &group}, federated, member, 65536) || requestMatchesScope(&model.ScopeRequest{GroupID: &group}, f.scope, member, 65536) {
		t.Fatal("group selector did not match federated scope")
	}
	if requestMatchesScope(&model.ScopeRequest{PinnedVector: &f.scope}, federated, member, 65536) {
		t.Fatal("different pinned vector was accepted")
	}
}
