//go:build linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type planRetentionFixture struct {
	ctx            context.Context
	pack           *FreshnessPlan
	token          string
	codec          *contextpack.CursorCodec
	binding        contextpack.CursorBinding
	guard          contextpack.CursorGuard
	request        []byte
	restore        FreshnessPlanRestoreInput
	revoked        bool
	revisionDigest string
}

func TestPlanRetentionRechecksDocumentApprovalAfterStorage(t *testing.T) {
	for _, check := range []struct {
		name        string
		publication bool
		combined    bool
	}{
		{"publication", true, false},
		{"readback", false, false},
		{"save_first_publication", true, true},
		{"save_first_readback", false, true},
	} {
		t.Run(check.name, func(t *testing.T) {
			f := newPlanRetentionFixture(t)
			state, target := planRetentionDirs(t)
			base, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
			store := &retentionBoundaryFixture{SymbolPackStorage: base}
			allowed := true
			original := f.restore.Plan.AuthorizeDocument
			f.restore.Plan.AuthorizeDocument = func(ctx context.Context, scope overview.Scope, declaration overview.DocumentDeclaration) error {
				if !allowed {
					return errors.New("document approval revoked independently of token scope")
				}
				return original(ctx, scope, declaration)
			}
			retention, err := NewPlanPackRetention(store, f.codec, 4<<20, 60, f.factory)
			if err != nil {
				t.Fatal(err)
			}
			if check.publication {
				store.afterCreate = func() { allowed = false }
			}
			options := contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "cli"}
			if check.combined {
				if !check.publication {
					store.afterRead = func() { allowed = false }
				}
				page, err := retention.SaveFirst(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, f.guard, options)
				if err == nil || len(page.Blocks) != 0 {
					t.Fatal("revoked document approval received a page from SaveFirst")
				}
				return
			}
			handle, err := retention.Save(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, f.guard)
			if check.publication {
				if err == nil || handle != nil {
					t.Fatal("revoked document approval received a saved handle")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			store.afterRead = func() { allowed = false }
			if page, err := handle.First(f.ctx, options); err == nil || len(page.Blocks) != 0 {
				t.Fatal("revoked document approval received a page")
			}
		})
	}
}

// The restore owner's authority and the caller's authority are independent.
// A factory can revoke the latter without changing scope, document approval or
// the returned owner callbacks; no handle may cross that boundary afterwards.
func TestPlanRetentionRejectsCallerRevokedByRestoreFactory(t *testing.T) {
	f := newPlanRetentionFixture(t)
	state, target := planRetentionDirs(t)
	store, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
	allowed := true
	caller := func(ctx context.Context, b contextpack.CursorBinding) error {
		if !allowed {
			return errors.New("caller revoked during independent restore")
		}
		return f.guard(ctx, b)
	}
	factory := func(ctx context.Context, b contextpack.CursorBinding, r model.PackRequest, revision string) (FreshnessPlanRestoreInput, error) {
		input, err := f.factory(ctx, b, r, revision)
		allowed = false
		return input, err
	}
	retention, err := NewPlanPackRetention(store, f.codec, 4<<20, 60, factory)
	if err != nil {
		t.Fatal(err)
	}
	if handle, err := retention.Save(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, caller); err == nil || handle != nil {
		t.Fatal("independently revoked caller received a retained plan")
	}
}

func newPlanRetentionFixture(t *testing.T) *planRetentionFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	f := &planRetentionFixture{ctx: ctx, revisionDigest: strings.Repeat("d", 64)}
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "22222222-2222-4222-8222-222222222222"
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 65536)
	if err != nil {
		t.Fatal(err)
	}
	config := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/fixture"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{"spec.md"}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 65536, MaxSourceBytes: 1048576, MaxOutputBytes: 65536, SecretRulesVersion: "v1"}
	policyID, err := model.PolicyDigest(config, 65536)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profileID, PolicyDigest: policyID, ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	files := []snapshot.File{}
	for name, content := range map[string]string{"go.mod": "module example.invalid/fixture\n\ngo 1.27\n", "spec.md": "Objective: preserve the contract.\n"} {
		file, err := snapshot.NewFile(name, []byte(content), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size})
		files = append(files, file)
	}
	captured, err := snapshot.Freeze(manifest, files, 65536, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	scopeAuth := func(context.Context, overview.Scope) error {
		if f.revoked {
			return errors.New("revoked")
		}
		return nil
	}
	pathAuth := func(ctx context.Context, scope overview.Scope, _ string) error { return scopeAuth(ctx, scope) }
	factAuth := func(ctx context.Context, scope overview.Scope, _ string) error { return scopeAuth(ctx, scope) }
	docAuth := func(ctx context.Context, scope overview.Scope, _ overview.DocumentDeclaration) error {
		return scopeAuth(ctx, scope)
	}
	view, err := overview.Build(ctx, overview.Input{Snapshot: captured, Profile: profile, GenerationID: generation, PolicyEpoch: 1, Scanner: scanner, MaxManifestBytes: 65536, MaxSourceBytes: 1048576, MaxFacts: 10000, MaxOutputBytes: 1048576, AuthorizeScope: scopeAuth, AuthorizePath: pathAuth, VerifyFacts: factAuth})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := captured.File("spec.md")
	declaration := overview.DocumentDeclaration{Kind: "objective", Source: model.SourceRef{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, Path: "spec.md", ContentHash: doc.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: int64(doc.Len())}}}
	plan, err := overview.BuildPlan(ctx, overview.PlanInput{Overview: view, Snapshot: captured, Policy: config, Selector: &model.WorkspaceSelector{Kind: "workspace"}, Declarations: []overview.DocumentDeclaration{declaration}, Scanner: scanner, TaskKind: "plan", AuthorizeDocument: docAuth, MaxBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: policyID, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	f.request = []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","freshness_mode":"analysis_snapshot","max_output_bytes":16384}`)
	request, err := model.DecodePackRequestJSON(f.request, 65536)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := model.RequestDigest("context_pack", "fixture-actor", "PackRequest", request, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	f.codec, err = contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	f.binding = contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64), RequestDigest: digest, OrderVersion: "pack-v1", Scope: scope}
	f.guard = func(context.Context, contextpack.CursorBinding) error {
		if f.revoked {
			return errors.New("revoked")
		}
		return nil
	}
	validation := ValidateCapturedInput{Scope: scope, MaxInputBytes: 65536, MaxManifestBytes: 65536}
	coverage := model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}
	f.pack, f.token, err = NewFreshnessPlan(ctx, FreshnessPlanInput{Plan: plan, Codec: f.codec, Binding: f.binding, Guard: f.guard, RequestJSON: f.request, PrincipalID: "fixture-actor", Coverage: coverage, Scanner: scanner, Validation: validation, RequestLimit: 65536, DeclarationRevisionDigest: f.revisionDigest})
	if err != nil {
		t.Fatal(err)
	}
	verifyDeclarations := func(ctx context.Context, s overview.Scope, declarations []overview.DocumentDeclaration) error {
		if len(declarations) != 1 || declarations[0] != declaration {
			return errors.New("declaration set changed")
		}
		return scopeAuth(ctx, s)
	}
	f.restore = FreshnessPlanRestoreInput{Plan: overview.PlanRestoreInput{Guard: f.guard, Snapshot: captured, AuthorizeScope: scopeAuth, AuthorizePath: pathAuth, VerifyFacts: factAuth, AuthorizeDocument: docAuth, VerifyDeclarations: verifyDeclarations}, Scanner: scanner, Validation: validation, RequestLimit: 65536, DeclarationRevisionDigest: f.revisionDigest}
	return f
}

func (f *planRetentionFixture) factory(_ context.Context, binding contextpack.CursorBinding, request model.PackRequest, revisionDigest string) (FreshnessPlanRestoreInput, error) {
	if model.ComparePinnedScope(binding.Scope, f.binding.Scope, 65536) != nil || revisionDigest != f.revisionDigest {
		return FreshnessPlanRestoreInput{}, ErrValidationAuthority
	}
	digest, err := model.RequestDigest("context_pack", "fixture-actor", "PackRequest", request, 65536, 4)
	if err != nil || digest != binding.RequestDigest {
		return FreshnessPlanRestoreInput{}, ErrValidationAuthority
	}
	return f.restore, nil
}

func planRetentionDirs(t *testing.T) (string, string) {
	t.Helper()
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	for _, path := range []string{state, target, filepath.Join(state, "scratch"), filepath.Join(state, "scratch", "pack")} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return state, target
}

func TestPlanPackRetentionRejectsRevisionChangeAcrossOwners(t *testing.T) {
	f := newPlanRetentionFixture(t)
	state, target := planRetentionDirs(t)
	store, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
	revision := f.revisionDigest
	factory := func(ctx context.Context, binding contextpack.CursorBinding, request model.PackRequest, signedRevision string) (FreshnessPlanRestoreInput, error) {
		if revision != signedRevision {
			return FreshnessPlanRestoreInput{}, ErrValidationAuthority
		}
		input, err := f.factory(ctx, binding, request, signedRevision)
		captured := revision
		input.Plan.Guard = func(context.Context, contextpack.CursorBinding) error {
			if revision != captured {
				return errors.New("declaration revision changed")
			}
			return nil
		}
		return input, err
	}
	retention, err := NewPlanPackRetention(store, f.codec, 4*1024*1024, 60, factory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retention.Save(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, f.guard); err != nil {
		t.Fatal(err)
	}
	revision = strings.Repeat("e", 64) // The declaration bytes stay exactly the same.
	if result, err := retention.Get(f.ctx, f.token, f.binding.PrincipalDigest, "fixture-actor", f.guard); err == nil || result != nil {
		t.Fatal("a new owner accepted an old result after the declaration revision changed")
	}
}

func TestPlanPackRetentionCrossProcess(t *testing.T) {
	f := newPlanRetentionFixture(t)
	state, target := planRetentionDirs(t)
	store, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
	retention, err := NewPlanPackRetention(store, f.codec, 4*1024*1024, 60, f.factory)
	if err != nil {
		t.Fatal(err)
	}
	ownerAllowed := true
	f.restore.Plan.Guard = func(context.Context, contextpack.CursorBinding) error {
		if !ownerAllowed {
			return errors.New("declaration authority revoked")
		}
		return nil
	}
	loads := 0
	factory := retention.restore
	retention.restore = func(ctx context.Context, binding contextpack.CursorBinding, request model.PackRequest, revisionDigest string) (FreshnessPlanRestoreInput, error) {
		loads++
		return factory(ctx, binding, request, revisionDigest)
	}
	envelope, err := f.pack.ArchiveEnvelope(f.ctx, "fixture-actor", f.request, 60, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	archiveClaims, err := retention.claims(f.ctx, f.token, f.binding.PrincipalDigest, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retention.restorePlan(f.ctx, envelope.Bytes, f.token, f.binding.PrincipalDigest, "other-actor", archiveClaims, f.guard); err == nil || loads != 0 {
		t.Fatalf("factory saw an unauthenticated actor: calls=%d err=%v", loads, err)
	}
	changedRequest, err := model.DecodePackRequestJSON(f.request, 65536)
	if err != nil {
		t.Fatal(err)
	}
	changedRequest.TaskKind = "explore"
	changedJSON, _ := json.Marshal(changedRequest)
	coverageJSON, _ := json.Marshal(f.pack.coverage)
	metadata, _ := json.Marshal(planArchiveMetadata{Version: 2, Request: changedJSON, Coverage: coverageJSON, DeclarationRevisionDigest: f.revisionDigest})
	signedWrongRequest, err := f.pack.pager.ArchiveForRetention(f.ctx, f.token, 60, metadata, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retention.restorePlan(f.ctx, signedWrongRequest, f.token, f.binding.PrincipalDigest, "fixture-actor", archiveClaims, f.guard); err == nil || loads != 0 {
		t.Fatalf("factory saw a request inconsistent with token: calls=%d err=%v", loads, err)
	}
	handle, err := retention.Save(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, f.guard)
	if err != nil || handle.Token() != f.token {
		t.Fatalf("save failed: %v", err)
	}
	options := contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "cli"}
	first, err := handle.First(f.ctx, options)
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("missing continuation: %v", err)
	}
	cursor := *first.Response.Pagination.NextCursor
	want, err := handle.Continue(f.ctx, cursor, options)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(want.Response.Data)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(f.ctx, os.Args[0], "-test.run=^TestPlanPackRetentionChild$")
	cmd.Env = append(os.Environ(), "MCE4D_PLAN_STATE="+state, "MCE4D_PLAN_TARGET="+target, "MCE4D_PLAN_TOKEN="+f.token, "MCE4D_PLAN_CURSOR="+cursor)
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != string(expected) {
		t.Fatalf("different process changed continuation: %v", err)
	}
	if other, err := retention.Get(f.ctx, f.token, f.binding.PrincipalDigest, "other-actor", f.guard); err == nil || other != nil {
		t.Fatal("different actor opened retained plan")
	}
	ownerGuard := f.restore.Plan.Guard
	f.restore.Plan.Guard = nil
	if other, err := retention.Get(f.ctx, f.token, f.binding.PrincipalDigest, "fixture-actor", f.guard); err == nil || other != nil {
		t.Fatal("missing declaration owner guard was accepted")
	}
	f.restore.Plan.Guard = ownerGuard
	ownerAllowed = false
	if page, err := handle.Continue(f.ctx, cursor, options); err == nil || len(page.Blocks) != 0 {
		t.Fatal("revoked declaration owner returned data")
	}
	ownerAllowed = true
	f.revoked = true
	if page, err := handle.Continue(f.ctx, cursor, options); err == nil || len(page.Blocks) != 0 {
		t.Fatal("revoked retained plan returned data")
	}
	f.revoked = false
	claims, err := f.codec.VerifyPrincipal(f.ctx, f.token, "pack", f.binding.PrincipalDigest, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "scratch", "pack", claims.Binding.ResultID+".pack")
	var once sync.Once
	var removeErr error
	options.DeliveryMode = "mcp_structured_verified"
	options.VerifyStructuredHost = func(context.Context) error {
		once.Do(func() { removeErr = os.Remove(path) })
		return removeErr
	}
	if page, err := handle.Continue(f.ctx, cursor, options); removeErr != nil || !errors.Is(err, packstore.ErrMissing) || len(page.Blocks) != 0 {
		t.Fatalf("eviction during delivery returned data: remove=%v err=%v", removeErr, err)
	}
}

func TestPlanPackRetentionChild(t *testing.T) {
	state, target, token, cursor := os.Getenv("MCE4D_PLAN_STATE"), os.Getenv("MCE4D_PLAN_TARGET"), os.Getenv("MCE4D_PLAN_TOKEN"), os.Getenv("MCE4D_PLAN_CURSOR")
	if state == "" || target == "" || token == "" || cursor == "" {
		return
	}
	f := newPlanRetentionFixture(t)
	store, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
	retention, err := NewPlanPackRetention(store, f.codec, 4*1024*1024, 60, f.factory)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := retention.Get(f.ctx, token, f.binding.PrincipalDigest, "fixture-actor", f.guard)
	if err != nil {
		t.Fatal(err)
	}
	page, err := handle.Continue(f.ctx, cursor, contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page.Response.Data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.Write(raw); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestMixedRetainedGCAuthenticatesBothFormats(t *testing.T) {
	for _, planExpires := range []bool{true, false} {
		t.Run(map[bool]string{true: "plan", false: "symbol"}[planExpires], func(t *testing.T) {
			f := newPlanRetentionFixture(t)
			symbol := newFreshnessFixture(t, "analysis_snapshot")
			state, target := planRetentionDirs(t)
			store, lifecycle := openFreshnessStoreFixture(t, f.ctx, state, target)
			planTTL, symbolTTL := int64(60), int64(60)
			if planExpires {
				planTTL = 2
			} else {
				symbolTTL = 2
			}
			plans, err := NewPlanPackRetention(store, f.codec, 4*1024*1024, planTTL, f.factory)
			if err != nil {
				t.Fatal(err)
			}
			symbols, err := NewSymbolPackRetention(store, symbol.input.Codec, 4*1024*1024, symbolTTL, func(_ context.Context, _ contextpack.CursorBinding) (FreshnessPackRestoreInput, error) {
				return freshnessRestoreFixture(t, symbol, "", nil), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			planHandle, err := plans.Save(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, f.guard)
			if err != nil {
				t.Fatal(err)
			}
			symbolPack, _, err := NewFreshnessPack(symbol.ctx, symbol.input)
			if err != nil {
				t.Fatal(err)
			}
			symbolHandle, err := symbols.Save(symbol.ctx, symbolPack, symbol.input.Binding.PrincipalDigest, symbol.input.PrincipalID, symbol.input.RequestJSON, symbol.input.Guard)
			if err != nil {
				t.Fatal(err)
			}
			job := "44444444-4444-4444-8444-444444444444"
			writer, err := runtime.AcquireWriter(f.ctx, lifecycle, job, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = writer.Close() })
			claims, err := f.codec.VerifyPrincipal(f.ctx, planHandle.Token(), "pack", f.binding.PrincipalDigest, f.guard)
			if !planExpires {
				claims, err = symbol.input.Codec.VerifyPrincipal(symbol.ctx, symbolHandle.Token(), "pack", symbol.input.Binding.PrincipalDigest, symbol.input.Guard)
			}
			if err != nil {
				t.Fatal(err)
			}
			if wait := time.Until(time.Unix(claims.IssuedAt+2, 0)) + 20*time.Millisecond; wait > 0 {
				time.Sleep(wait)
			}
			var removed int
			if planExpires {
				removed, err = symbols.GCExpired(f.ctx, writer, job)
			} else {
				removed, err = plans.GCExpired(f.ctx, writer, job)
			}
			if err != nil || removed != 1 {
				t.Fatalf("mixed GC removed %d: %v", removed, err)
			}
			if planExpires {
				if _, err := symbolHandle.First(symbol.ctx, freshnessOptions()); err != nil {
					t.Fatalf("live symbol result lost: %v", err)
				}
			} else {
				if _, err := planHandle.First(f.ctx, contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "cli"}); err != nil {
					t.Fatalf("live plan result lost: %v", err)
				}
			}
			unknownID := "99999999-9999-4999-8999-999999999999"
			unknown := []byte("unrecognized fixture bytes")
			if err := store.Create(f.ctx, unknownID, unknown, func(context.Context, []byte) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if count, err := plans.GCExpired(f.ctx, writer, job); err == nil || count != 0 {
				t.Fatalf("unknown artifact accepted by GC: count=%d err=%v", count, err)
			}
			bytes, err := os.ReadFile(filepath.Join(state, "scratch", "pack", unknownID+".pack"))
			if err != nil || string(bytes) != string(unknown) {
				t.Fatalf("unknown artifact changed: %v", err)
			}
		})
	}
}
