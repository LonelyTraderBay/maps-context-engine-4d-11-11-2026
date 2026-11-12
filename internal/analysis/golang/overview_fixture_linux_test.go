//go:build linux

package golang

import (
	"context"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
)

// The same captured synthetic source is analyzed and projected. The
// verifier is a fixture trust boundary, not a production generation reader.
func runOverviewCapturedGraphFixture(t *testing.T, ctx context.Context, request TypedInput, contents map[string]string) {
	t.Helper()
	manifest := request.Loader.Preflight.Snapshot.Manifest()
	moduleID, err := overview.GoModuleID(manifest.WorktreeID, "go.mod")
	if err != nil {
		t.Fatal(err)
	}
	request.ModuleKey = moduleID
	request.OtherPackages = append([]TypedPackageIdentity(nil), request.OtherPackages...)
	for i := range request.OtherPackages {
		request.OtherPackages[i].ModuleKey = moduleID
	}
	request.MaxOutputBytes = 256 * 1024
	graph, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: request, MaxRelations: 2000, MaxOutputBytes: 512 * 1024})
	if err != nil || !graph.Typed.Loader.BuildSelectionVerified {
		t.Fatalf("analyzed overview fixture: %v", err)
	}
	byPackage := map[string]overview.PackageInput{}
	var symbols []model.SymbolFact
	for _, syntax := range graph.Typed.Syntax {
		p := byPackage[syntax.Package.PackageKey]
		p.Fact = syntax.Package
		p.Files = append(p.Files, syntax.Path)
		byPackage[syntax.Package.PackageKey] = p
		symbols = append(symbols, syntax.Symbols...)
	}
	var packages []overview.PackageInput
	for _, p := range byPackage {
		packages = append(packages, p)
	}
	const gen = "33333333-3333-4333-8333-333333333333"
	input := overview.Input{Snapshot: request.Loader.Preflight.Snapshot, Profile: request.Loader.Preflight.Profile, GenerationID: gen, PolicyEpoch: 1, Packages: packages, Symbols: symbols, Relations: graph.Relations, Scanner: request.Loader.Preflight.Scanner, MaxManifestBytes: 32768, MaxSourceBytes: 65536, MaxFacts: 10000, MaxOutputBytes: 1024 * 1024,
		AuthorizeScope: func(context.Context, overview.Scope) error { return nil }, AuthorizePath: func(context.Context, overview.Scope, string) error { return nil }, VerifyFacts: func(context.Context, overview.Scope, string) error { return nil }}
	report, err := overview.Build(ctx, input)
	if err != nil {
		t.Fatalf("overview from analyzed facts: %v", err)
	}
	summary, err := report.Read(ctx)
	if err != nil || len(summary.Modules) != 1 || len(summary.Modules[0].Packages) != 2 {
		t.Fatalf("analyzed hierarchy: %v", err)
	}
	all := map[string]bool{}
	calls := 0
	for _, group := range summary.Groups {
		after := ""
		for {
			page, err := report.Expand(ctx, group.Expand, after, 3)
			if err != nil {
				t.Fatal(err)
			}
			for _, site := range page.Sites {
				if all[site.Relation.RelationID] {
					t.Fatal("duplicate expanded relation")
				}
				all[site.Relation.RelationID] = true
				if site.Relation.Kind == "CALLS_DIRECT" && site.Relation.SitePath == "p.go" && (site.Source.Range.StartByte == int64(strings.Index(contents["p.go"], "sub.Fn(")) || site.Source.Range.StartByte == int64(strings.Index(contents["p.go"], "BoxValue.Get()"))) {
					calls++
				}
			}
			if page.NextAfter == "" {
				break
			}
			after = page.NextAfter
		}
	}
	if len(all) != len(graph.Relations) || calls != 2 {
		t.Fatalf("source-backed overview lost relations: %d/%d, calls=%d", len(all), len(graph.Relations), calls)
	}
}
