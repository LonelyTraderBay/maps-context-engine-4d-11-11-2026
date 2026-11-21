//go:build linux

package acceptance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/history"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func TestT064RetainedSQLiteSemanticComparison(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const path = "main.go"
	before, after := "package fixture\nfunc Count() int { return 1 }\n", "package fixture\nfunc Count() int { return 2 }\n"
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{},
		ModuleRoots: []string{"."}, ToolchainDigest: bufferSHA([]byte("fixture typed compiler")),
		ProviderDigest: bufferSHA([]byte("fixture typed provider")), ModuleMode: "readonly", AnalysisMode: "typed_when_available",
		DependencyRoots: []string{}}
	m := makeFullstackMember(t, ctx, t.TempDir(), "fixture", stackUUID(61), stackUUID(62), stackUUID(63),
		profile, profile.ProviderDigest, []string{}, map[string]string{path: before})
	publish := func(symbolID string) {
		m.publish(t, ctx, func(sink func(context.Context, model.ProviderBatch) error) (model.ProviderDone, error) {
			anchor := m.sourceRef(path)
			anchor.Range.StartByte = int64(len("package fixture\n"))
			pkg := "fixture-package"
			err := sink(ctx, model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: m.job, Sequence: 1,
				Packages: []model.PackageFact{{PackageKey: pkg, ImportPath: "fixture", ModuleKey: "fixture-module",
					Variant: "production", InputDigest: m.member.SnapshotDigest, SemanticStatus: "complete_in_profile"}},
				Symbols: []model.SymbolFact{{SymbolID: symbolID, Path: &anchor.Path, Range: &anchor.Range, Kind: "function",
					PackageKey: &pkg, Name: "Count", QualifiedName: "fixture.Count", ScopeKey: pkg, Signature: "func() int", Exported: true}},
				Relations: []model.RelationFact{}, Diagnostics: []model.DiagnosticFact{}})
			return model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: m.job, Sequence: 2,
				Status: "complete_in_supported_scope", InputDigest: m.member.SnapshotDigest,
				Capabilities: []model.Capability{{Name: "fixture_typed", ProviderID: "fixture", ProviderVersion: "1",
					Status: "supported_scope_complete"}}, Gaps: []model.Gap{}}, err
		})
	}
	publish("old-count")
	observedMember := func() model.ScopeMember {
		member := m.member
		member.ServiceID = nil
		return member
	}
	beforeMember := observedMember()
	manifest := m.capture.Manifest()
	file, err := snapshot.NewFile(path, []byte(after), fullstackBytes)
	requireFullstack(t, err)
	hash, size := file.ContentHash(), int64(file.Len())
	manifest.Files[0].ContentHash, manifest.Files[0].ByteLength = &hash, &size
	manifest.SnapshotDigest = ""
	frozen, err := snapshot.Freeze(manifest, []snapshot.File{file}, fullstackBytes, fullstackBytes)
	requireFullstack(t, err)
	requireFullstack(t, os.WriteFile(filepath.Join(m.source, path), file.Bytes(), 0600))
	m.capture = frozen
	m.member.GenerationID, m.member.SnapshotDigest = stackUUID(64), frozen.Manifest().SnapshotDigest
	publish("new-count")
	afterMember := observedMember()
	scopeFor := func(member model.ScopeMember) model.ScopeVector {
		s := model.ScopeVector{Kind: "single", CatalogIncarnation: stackUUID(65), Members: []model.ScopeMember{member},
			UnavailableMembers: []model.MemberUnavailable{}}
		s.VectorDigest, err = model.ComputeScopeVectorDigest(s, fullstackBytes)
		requireFullstack(t, err)
		return s
	}
	resolver := history.DiffResolver{
		Observe: func(ctx context.Context, pinned model.ScopeVector) (model.ScopeVector, error) {
			member := pinned.Members[0]
			seen, err := m.facts.ReadGeneration(ctx, member.ProfileID, member.GenerationID, m.authorizeGeneration, fullstackBytes)
			if errors.Is(err, index.ErrSnapshotUnavailable) {
				return model.ScopeVector{}, history.ErrDiffSnapshotMissing
			}
			if err != nil || seen.SnapshotDigest != member.SnapshotDigest || seen.Identity.Incarnation != member.IndexIncarnation {
				return model.ScopeVector{}, history.ErrDiffScopeChanged
			}
			return pinned, nil
		},
		ReadMember: func(ctx context.Context, pinned model.ScopeVector, member model.ScopeMember, maxFacts int) (history.DiffMember, error) {
			graph, err := m.facts.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: pinned.VectorDigest,
				AuthorizeGeneration: m.authorizeGeneration, AuthorizePath: m.authorizePath, Scanner: m.scanner,
				MaxManifestBytes: fullstackBytes, MaxFileBytes: fullstackBytes, MaxSourceBytes: fullstackBytes,
				MaxFactBytes: fullstackBytes, MaxFacts: maxFacts})
			if err != nil {
				return history.DiffMember{}, err
			}
			return history.DiffMember{Member: member, Capture: graph.Snapshot, Symbols: graph.Symbols,
				Relations: graph.Relations, Complete: graph.Coverage.Status == "supported_scope_complete"}, nil
		},
	}
	req := history.DiffRequest{Left: scopeFor(beforeMember), Right: scopeFor(afterMember), MaxFacts: 100, MaxChanges: 10}
	out, err := resolver.Compare(ctx, req)
	requireFullstack(t, err)
	if len(out.Changes) != 1 || out.Changes[0].Change != "changed" || out.Changes[0].Match != "probable" ||
		out.Changes[0].Before.Source.GenerationID != beforeMember.GenerationID ||
		out.Changes[0].After.Source.GenerationID != afterMember.GenerationID {
		t.Fatalf("comparison did not use both retained SQLite generations: %+v", out)
	}
	expired := beforeMember
	expired.GenerationID = stackUUID(66)
	req.Left = scopeFor(expired)
	if _, err := resolver.Compare(ctx, req); !errors.Is(err, history.ErrDiffSnapshotMissing) {
		t.Fatalf("unknown generation resolved to active: %v", err)
	}
	t.Log("C064: changed declaration bound to two real retained SQLite generations, missing generation returned explicit error")
}
