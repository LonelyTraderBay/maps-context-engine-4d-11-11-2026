package app

import (
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

func TestFreshnessPackLargeAnalysisRemainsAvailableWhenValidationIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "22222222-2222-4222-8222-222222222222"
	conf := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/synthetic"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 65536, MaxSourceBytes: 1024 * 1024, MaxOutputBytes: 65536, SecretRulesVersion: "test"}
	policyDigest, err := model.PolicyDigest(conf, 65536)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: strings.Repeat("a", 64), PolicyDigest: policyDigest, ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	files := []snapshot.File{}
	appendFile := func(path, content string) {
		t.Helper()
		file, err := snapshot.NewFile(path, []byte(content), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &length})
		files = append(files, file)
	}
	targetPath, pkg, target := "main.go", "pkg", "target"
	appendFile(targetPath, "package p\nfunc Target() {}\n")
	symbols := []model.SymbolFact{{SymbolID: target, Path: &targetPath, PackageKey: &pkg, Kind: "function", Name: "Target", QualifiedName: "p.Target", ScopeKey: pkg, Range: &model.ByteRange{StartByte: 15, EndByte: 21}}}
	relations := []model.RelationFact{}
	for i := 0; i < 257; i++ {
		path, name, id := fmt.Sprintf("src/f%03d.go", i), fmt.Sprintf("F%03d", i), fmt.Sprintf("symbol-%03d", i)
		body := "package p\nfunc " + name + "() { Target() }\n"
		appendFile(path, body)
		start, call := int64(strings.Index(body, name)), int64(strings.Index(body, "Target"))
		symbols = append(symbols, model.SymbolFact{SymbolID: id, Path: &path, PackageKey: &pkg, Kind: "function", Name: name, QualifiedName: "p." + name, ScopeKey: pkg, Range: &model.ByteRange{StartByte: start, EndByte: start + int64(len(name))}})
		relations = append(relations, model.RelationFact{RelationID: fmt.Sprintf("call-%03d", i), SourceSymbolID: id, TargetSymbolID: &target, Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: path, Range: model.ByteRange{StartByte: call, EndByte: call + 6}})
	}
	captured, err := snapshot.Freeze(manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest = captured.Manifest()
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: policyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := contextpack.Select(ctx, contextpack.Input{Scope: scope, Target: model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation}, SymbolID: target}, Snapshot: captured,
		Packages: []model.PackageFact{{PackageKey: pkg, ImportPath: "fixture/p", ModuleKey: "module", Variant: "production", InputDigest: manifest.SnapshotDigest, SemanticStatus: "partial"}}, Symbols: symbols, Relations: relations,
		Coverage: model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "workspace"}, Scanner: scanner,
		AuthorizeScope: func(context.Context, model.ScopeVector, model.ScopeMember) error { return nil }, AuthorizePath: func(context.Context, model.ScopeMember, string) error { return nil }, VerifyFacts: func(context.Context, model.ScopeMember, string) error { return nil }, MaxManifestBytes: 65536, MaxFactBytes: 1024 * 1024, MaxSourceBytes: 1024 * 1024, MaxFacts: 1000, MaxNodes: 300, MaxEdges: 300, MaxDepth: 2, MaxOutputBytes: 4 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	slice, err := contextpack.BuildTaskSlice(ctx, contextpack.SliceInput{Selection: selection, Policy: conf, TaskKind: "explore", VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }, MaxBytes: 4 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	request := model.PackRequest{Selector: &model.SymbolSelector{Kind: "symbol", SymbolID: target}, TaskKind: "explore", FreshnessMode: "analysis_snapshot"}
	raw, _ := json.Marshal(request)
	digest, err := model.RequestDigest("context_pack", "actor", "PackRequest", request, 65536, 4)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := contextpack.NewCursorCodec([32]byte{1})
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	validation := ValidateCapturedInput{Scope: scope, MaxInputBytes: 1024 * 1024, MaxManifestBytes: 65536, CurrentScope: func(context.Context) (model.ScopeVector, error) { return scope, nil }, ResolveCapture: func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error) { return captured, nil }, AuthorizeSource: func(context.Context, model.ScopeMember, model.SourceRef) error { return nil }, VerifyWorkspace: func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
		return WorkspaceObservedCurrent, nil
	}}
	input := FreshnessPackInput{Slice: slice, Codec: codec, Binding: contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64), RequestDigest: digest, OrderVersion: "pack-v1", Scope: scope}, Guard: guard, RequestJSON: raw, PrincipalID: "actor", Validation: validation, RequestLimit: 65536}
	pack, token, err := NewFreshnessPack(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pack.First(ctx, freshnessOptions())
	if err != nil || page.Response.Freshness.Status != "not_checked" {
		t.Fatalf("large analysis snapshot unavailable: %v", err)
	}
	if result, err := pack.Validate(ctx, token, input.Binding.PrincipalDigest, guard); !errors.Is(err, ErrFreshnessUnknown) || result.Data.EditPreconditionValidated {
		t.Fatalf("over-limit validation did not fail closed: %v", err)
	}
}
