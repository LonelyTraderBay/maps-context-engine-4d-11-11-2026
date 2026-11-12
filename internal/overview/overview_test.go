package overview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const worktree = "11111111-1111-4111-8111-111111111111"
const generation = "33333333-3333-4333-8333-333333333333"

func overviewFixture(t *testing.T, count int) (context.Context, Input) {
	t.Helper()
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{".", "nested", "peer"}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 65536)
	if err != nil {
		t.Fatal(err)
	}
	var nestedSource, peerSource strings.Builder
	nestedSource.WriteString("package nested\nimport shared \"example.test/root/shared\"\nfunc Use() {\n")
	peerSource.WriteString("package peer\nimport shared \"example.test/root/shared\"\nfunc Use() {\n")
	type location struct {
		path  string
		start int64
	}
	locations := make([]location, count)
	for i := 0; i < count; i++ {
		builder, filename := &nestedSource, "nested/use.go"
		if i%3 == 0 {
			builder, filename = &peerSource, "peer/use.go"
		}
		locations[i] = location{path: filename, start: int64(builder.Len() + len("shared."))}
		builder.WriteString("shared.Shared()\n")
	}
	nestedSource.WriteString("}\n")
	peerSource.WriteString("}\n")
	contents := map[string]string{
		"go.mod":        "// root fixture\r\nmodule example.test/root\r\n\r\ngo 1.27.4\r\n",
		"shared/s.go":   "package shared\nfunc Shared() {}\n",
		"nested/go.mod": "module example.test/nested\n\ngo 1.27.4\n",
		"nested/use.go": nestedSource.String(),
		"peer/go.mod":   "module example.test/peer\n\ngo 1.27.4\n",
		"peer/use.go":   peerSource.String(),
		"orphan/go.mod": "module example.test/orphan\n",
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profileID, PolicyDigest: strings.Repeat("c", 64), ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	files := []snapshot.File{}
	for name, body := range contents {
		f, err := snapshot.NewFile(name, []byte(body), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := f.ContentHash(), int64(f.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size})
		files = append(files, f)
	}
	captured, err := snapshot.Freeze(manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	digest := captured.Manifest().SnapshotDigest
	rootID, err := GoModuleID(worktree, "go.mod")
	if err != nil {
		t.Fatal(err)
	}
	nestedID, err := GoModuleID(worktree, "nested/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	peerID, err := GoModuleID(worktree, "peer/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	packages := []PackageInput{
		{Fact: model.PackageFact{PackageKey: "shared", ImportPath: "example.test/root/shared", ModuleKey: rootID, Variant: "production", InputDigest: digest, SemanticStatus: "partial"}, Files: []string{"shared/s.go"}},
		{Fact: model.PackageFact{PackageKey: "nested", ImportPath: "example.test/nested", ModuleKey: nestedID, Variant: "production", InputDigest: digest, SemanticStatus: "partial"}, Files: []string{"nested/use.go"}},
		{Fact: model.PackageFact{PackageKey: "peer", ImportPath: "example.test/peer", ModuleKey: peerID, Variant: "production", InputDigest: digest, SemanticStatus: "partial"}, Files: []string{"peer/use.go"}},
	}
	symbol := func(id, pkg, filename, name string) model.SymbolFact {
		at := strings.Index(contents[filename], name)
		return model.SymbolFact{SymbolID: id, Path: &filename, PackageKey: &pkg, Kind: "function", Name: name, QualifiedName: pkg + "." + name, ScopeKey: "scope-" + pkg, Range: &model.ByteRange{StartByte: int64(at), EndByte: int64(at + len(name))}, Exported: true}
	}
	symbols := []model.SymbolFact{symbol("hub", "shared", "shared/s.go", "Shared"), symbol("caller-nested", "nested", "nested/use.go", "Use"), symbol("caller-peer", "peer", "peer/use.go", "Use")}
	var relations []model.RelationFact
	for i := 0; i < count; i++ {
		filename, caller := locations[i].path, "caller-nested"
		if i%3 == 0 {
			caller = "caller-peer"
		}
		at := locations[i].start
		target := "hub"
		relations = append(relations, model.RelationFact{RelationID: fmt.Sprintf("edge-%05d", i), SourceSymbolID: caller, TargetSymbolID: &target, Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: filename, Range: model.ByteRange{StartByte: at, EndByte: at + int64(len("Shared"))}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx, Input{Snapshot: captured, Profile: profile, GenerationID: generation, PolicyEpoch: 1, Packages: packages, Symbols: symbols, Relations: relations, Scanner: scanner, MaxManifestBytes: 65536, MaxSourceBytes: 1024 * 1024, MaxFacts: 10000, MaxOutputBytes: 4 * 1024 * 1024,
		AuthorizeScope: func(context.Context, Scope) error { return nil }, AuthorizePath: func(context.Context, Scope, string) error { return nil }, VerifyFacts: func(_ context.Context, s Scope, d string) error {
			if s.SnapshotDigest != digest || len(d) != 64 {
				return errors.New("bad proof")
			}
			return nil
		}}
}

func TestOverviewGroupsAllSharedConsumersAndPages(t *testing.T) {
	ctx, input := overviewFixture(t, 3001)
	report, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Modules) != 3 || len(summary.Groups) != 2 || !reflect.DeepEqual(summary.Gaps, []string{"ENTRYPOINTS_AND_TRUSTED_DOCUMENTS_NOT_COMPUTED", "MODULE_OUTSIDE_PROFILE", "SEMANTIC_COVERAGE_NOT_PROVEN"}) {
		t.Fatalf("bad overview: %+v", summary)
	}
	root := summary.Modules[0]
	if root.Root != "." || root.ParentID != "" || summary.Modules[1].ParentID != root.Ref.ModuleID || summary.Modules[2].ParentID != root.Ref.ModuleID {
		t.Fatalf("module parent hierarchy incorrect: %+v", summary.Modules)
	}
	f, _ := input.Snapshot.File(root.Source.Path)
	data, err := f.Range(int(root.Source.Range.StartByte), int(root.Source.Range.EndByte))
	if err != nil || string(data) != "module example.test/root" || root.Source.GenerationID != generation || root.Source.ContentHash != f.ContentHash() {
		t.Fatalf("module source range lost: %q %v", data, err)
	}
	all := map[string]bool{}
	sites := map[string]bool{}
	for _, group := range summary.Groups {
		if group.TargetModuleID != root.Ref.ModuleID || group.TargetBoundary != "local" || group.Kind != "CALLS_DIRECT" || group.EvidenceKind != "type_resolved" {
			t.Fatalf("incorrect grouping: %+v", group)
		}
		after := ""
		count := 0
		for {
			page, err := report.Expand(ctx, group.Expand, after, 13)
			if err != nil {
				t.Fatal(err)
			}
			for _, site := range page.Sites {
				if all[site.Relation.RelationID] || site.Source.GenerationID != generation || site.Source.Range != site.Relation.Range || site.Source.Path != site.Relation.SitePath {
					t.Fatalf("duplicate or mismatched evidence: %+v", site)
				}
				all[site.Relation.RelationID] = true
				key := fmt.Sprintf("%s:%d", site.Source.Path, site.Source.Range.StartByte)
				file, _ := input.Snapshot.File(site.Source.Path)
				text, err := file.Range(int(site.Source.Range.StartByte), int(site.Source.Range.EndByte))
				if err != nil || string(text) != "Shared" || sites[key] {
					t.Fatalf("invalid or repeated source site: %s", key)
				}
				sites[key] = true
				count++
			}
			if page.NextAfter == "" {
				break
			}
			if page.NextAfter <= after {
				t.Fatal("nonprogressing cursor")
			}
			after = page.NextAfter
		}
		if count != group.Count {
			t.Fatalf("group lost sites: %d/%d", count, group.Count)
		}
	}
	if len(all) != 3001 {
		t.Fatalf("shared hub lost callers: %d", len(all))
	}
	for i := 0; i < 3001; i++ {
		if !all[fmt.Sprintf("edge-%05d", i)] {
			t.Fatalf("missing caller %d", i)
		}
	}
	// All inputs may arrive in arbitrary order without changing the projection.
	sort.Slice(input.Packages, func(i, j int) bool { return input.Packages[i].Fact.PackageKey > input.Packages[j].Fact.PackageKey })
	sort.Slice(input.Symbols, func(i, j int) bool { return input.Symbols[i].SymbolID > input.Symbols[j].SymbolID })
	sort.Slice(input.Relations, func(i, j int) bool { return input.Relations[i].RelationID > input.Relations[j].RelationID })
	second, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := second.Read(ctx)
	if err != nil || !reflect.DeepEqual(summary, again) {
		t.Fatalf("nondeterministic projection: %v", err)
	}
	summary.Modules[0].Packages[0].Files[0] = "mutated.go"
	input.Symbols[0].Name = "mutated"
	afterMutation, err := report.Read(ctx)
	if err != nil || afterMutation.Modules[0].Packages[0].Files[0] != "shared/s.go" {
		t.Fatal("caller changed stored report")
	}
}

func TestOverviewRejectsStaleScopeAndRevocation(t *testing.T) {
	ctx, input := overviewFixture(t, 4)
	allowed := true
	input.AuthorizeScope = func(context.Context, Scope) error {
		if !allowed {
			return errors.New("private policy detail")
		}
		return nil
	}
	report, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ref := summary.Groups[0].Expand
	for _, bad := range []Expansion{
		{Scope: Scope{Identity: model.GraphIdentity{WorktreeID: worktree, ProfileID: ref.Scope.Identity.ProfileID, GenerationID: "44444444-4444-4444-8444-444444444444"}, SnapshotDigest: ref.Scope.SnapshotDigest, PolicyDigest: ref.Scope.PolicyDigest, PolicyEpoch: ref.Scope.PolicyEpoch}, FactsDigest: ref.FactsDigest, GroupID: ref.GroupID},
		{Scope: ref.Scope, FactsDigest: strings.Repeat("d", 64), GroupID: ref.GroupID},
		{Scope: ref.Scope, FactsDigest: ref.FactsDigest, GroupID: "other"},
	} {
		if page, err := report.Expand(ctx, bad, "", 2); !errors.Is(err, ErrInput) || len(page.Sites) != 0 {
			t.Fatalf("bad expansion scope accepted: %v", err)
		}
	}
	if page, err := report.Expand(ctx, ref, "nonexistent", 2); !errors.Is(err, ErrInput) || len(page.Sites) != 0 {
		t.Fatal("forged continuation accepted")
	}
	allowed = false
	if summary, err := report.Read(ctx); !errors.Is(err, ErrAuthority) || len(summary.Modules) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked summary leaked: %v", err)
	}
	if page, err := report.Expand(ctx, ref, "", 2); !errors.Is(err, ErrAuthority) || len(page.Sites) != 0 {
		t.Fatalf("revoked expansion leaked: %v", err)
	}
}

func TestOverviewRejectsInvalidMembershipBudgetAndCancellation(t *testing.T) {
	ctx, input := overviewFixture(t, 2)
	input.Packages[0].Files = []string{"nested/use.go"}
	if report, err := Build(ctx, input); !errors.Is(err, ErrInput) || report != nil {
		t.Fatalf("cross-module membership accepted: %v", err)
	}
	ctx, input = overviewFixture(t, 0)
	input.Packages = input.Packages[:1]
	input.Packages[0].Files = []string{"nested/use.go"}
	input.Packages[0].Fact.ImportPath = "example.test/root/nested"
	input.Symbols, input.Relations = nil, nil
	if report, err := Build(ctx, input); !errors.Is(err, ErrInput) || report != nil {
		t.Fatalf("nested manifest was ignored as module boundary: %v", err)
	}
	ctx, input = overviewFixture(t, 2)
	input.Relations[0].Range.EndByte = 99999
	if report, err := Build(ctx, input); !errors.Is(err, ErrInput) || report != nil {
		t.Fatalf("invalid evidence range accepted: %v", err)
	}
	ctx, input = overviewFixture(t, 2)
	input.MaxSourceBytes = 1
	if report, err := Build(ctx, input); !errors.Is(err, ErrLimit) || report != nil {
		t.Fatalf("source limit returned partial report: %v", err)
	}
	input.MaxSourceBytes = 1024 * 1024
	input.MaxFacts = 1
	if report, err := Build(ctx, input); !errors.Is(err, ErrInput) || report != nil {
		t.Fatalf("fact limit returned partial report: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	input.MaxFacts = 10000
	if report, err := Build(canceled, input); !errors.Is(err, context.Canceled) || report != nil {
		t.Fatalf("cancel returned report: %v", err)
	}
}

func TestOverviewRechecksAuthorityAndDoesNotReturnSecretFacts(t *testing.T) {
	ctx, input := overviewFixture(t, 2)
	input.Packages[0].Fact.ImportPath = "api_key=abcdefghijklmnop"
	input.AuthorizeScope = func(context.Context, Scope) error { return errors.New("private policy detail") }
	if report, err := Build(ctx, input); !errors.Is(err, ErrAuthority) || report != nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("scope was not checked before facts: %v", err)
	}
	input.AuthorizeScope = func(context.Context, Scope) error { return nil }
	if report, err := Build(ctx, input); !errors.Is(err, policy.ErrSecretBlocked) || report != nil {
		t.Fatalf("secret fact was returned: %v", err)
	}
	ctx, input = overviewFixture(t, 2)
	valid := true
	input.VerifyFacts = func(context.Context, Scope, string) error {
		if !valid {
			return errors.New("private proof detail")
		}
		return nil
	}
	report, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	valid = false
	if page, err := report.Expand(ctx, summary.Groups[0].Expand, "", 1); !errors.Is(err, ErrAuthority) || len(page.Sites) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked generation proof returned sites: %v", err)
	}
}

func TestOverviewRetainsUnresolvedAndReverseDirection(t *testing.T) {
	ctx, input := overviewFixture(t, 1)
	first := input.Relations[0]
	reversed := first
	reversed.RelationID = "reverse"
	reversed.SourceSymbolID = "hub"
	target := first.SourceSymbolID
	reversed.TargetSymbolID = &target
	reversed.SitePath = "shared/s.go"
	f, _ := input.Snapshot.File(reversed.SitePath)
	at := strings.Index(string(f.Bytes()), "Shared")
	reversed.Range = model.ByteRange{StartByte: int64(at), EndByte: int64(at + len("Shared"))}
	unresolved := first
	unresolved.RelationID = "unknown"
	unresolved.TargetSymbolID = nil
	unresolved.Kind, unresolved.EvidenceKind, unresolved.Reason = "UNRESOLVED_CALL", "unresolved", "not resolved"
	label := "unknown"
	unresolved.UnresolvedLabel = &label
	input.Relations = append(input.Relations, reversed, unresolved)
	report, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil || len(summary.Groups) != 3 || summary.RelationCount != 3 {
		t.Fatalf("wrong edge boundaries: %+v %v", summary.Groups, err)
	}
	seen := map[string]bool{}
	for _, group := range summary.Groups {
		page, err := report.Expand(ctx, group.Expand, "", 10)
		if err != nil || len(page.Sites) != 1 {
			t.Fatalf("failed expansion: %v", err)
		}
		id := page.Sites[0].Relation.RelationID
		seen[id] = true
		if id == "unknown" && (group.TargetBoundary != "unresolved" || group.TargetModuleID != "" || group.EvidenceKind != "unresolved") {
			t.Fatal("unresolved edge became exact")
		}
		if id == "reverse" && group.SourceModuleID == group.TargetModuleID {
			t.Fatal("reverse direction lost")
		}
	}
	if !seen["reverse"] || !seen["unknown"] || !seen[first.RelationID] {
		t.Fatal("missing grouped evidence")
	}
}

func TestOverviewExpansionMakesProgressWithinByteBudget(t *testing.T) {
	ctx, input := overviewFixture(t, 30)
	report, err := Build(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := report.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	report.maxOutputBytes = 1024
	for _, group := range summary.Groups {
		after := ""
		total := 0
		pages := 0
		for {
			page, err := report.Expand(ctx, group.Expand, after, 10000)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(page)
			if err != nil || len(raw) > 1024 || len(page.Sites) == 0 {
				t.Fatalf("invalid byte-limited page: %d %v", len(raw), err)
			}
			total += len(page.Sites)
			pages++
			if page.NextAfter == "" {
				break
			}
			if page.NextAfter <= after {
				t.Fatal("continuation did not advance")
			}
			after = page.NextAfter
		}
		if pages < 2 || total != group.Count {
			t.Fatalf("byte budget lost evidence: pages=%d sites=%d expected=%d", pages, total, group.Count)
		}
	}
	var missing *Report
	if _, err := missing.Read(ctx); !errors.Is(err, ErrInput) {
		t.Fatal("nil report did not fail closed")
	}
}

func TestGoModuleIDIsStableAndScoped(t *testing.T) {
	id, err := GoModuleID(worktree, "a/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	again, err := GoModuleID(worktree, "a/go.mod")
	if err != nil || id != again {
		t.Fatal("unstable ID")
	}
	other, _ := GoModuleID(worktree, "b/go.mod")
	worktreeOther, _ := GoModuleID("22222222-2222-4222-8222-222222222222", "a/go.mod")
	if id == other || id == worktreeOther || len(id) != 64 {
		t.Fatal("module identities collided across locations")
	}
	for _, p := range []string{"../go.mod", "a/go.mod/child", "a/mod.txt"} {
		if _, err := GoModuleID(worktree, p); !errors.Is(err, ErrInput) {
			t.Fatal("invalid manifest path accepted")
		}
	}
}
