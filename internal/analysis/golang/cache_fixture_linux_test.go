//go:build linux

package golang

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func runCacheDifferentialFixture(t *testing.T, ctx context.Context, parent, goRoot string, tool *runtimeadapter.GoExecutable) {
	t.Helper()
	dir := filepath.Join(parent, "cache-differential")
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	syntaxCache, err := NewSyntaxCache(4*1024*1024, 32)
	if err != nil {
		t.Fatal(err)
	}
	type step struct {
		calls                             int
		added, changeTarget, changeModule bool
	}
	steps := []step{{0, false, false, false}, {0, true, false, false}, {2, true, true, false}, {1, false, false, true}}
	rng := rand.New(rand.NewSource(20261112))
	for i := 0; i < 6; i++ {
		steps = append(steps, step{calls: rng.Intn(4), added: rng.Intn(2) == 1, changeTarget: rng.Intn(2) == 1, changeModule: rng.Intn(2) == 1})
	}
	var previous *snapshot.Snapshot
	for stepIndex, current := range steps {
		calls := ""
		for i := 0; i < current.calls; i++ {
			calls += "sum += Target()\n"
		}
		value := 1
		if current.changeTarget {
			value = 2
		}
		moduleSuffix := ""
		if current.changeModule {
			moduleSuffix = "// changed fixture module metadata\n"
		}
		contents := map[string]string{
			"go.mod":    "module example.test/fixture\n\ngo 1.27.4\n" + moduleSuffix,
			"p.go":      fmt.Sprintf("package fixture\nfunc Target() int { return %d }\n", value),
			"caller.go": "package fixture\nfunc Existing() int { sum := 0\n" + calls + "return sum }\n",
		}
		if current.added {
			contents["added.go"] = "package fixture\nfunc NewCaller() int { return Target() }\n"
		}
		workspace := filepath.Join(dir, "input")
		addedPath := filepath.Join(workspace, "added.go")
		if !current.added {
			if err := os.Remove(addedPath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		for name, value := range contents {
			if err := os.WriteFile(filepath.Join(workspace, name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		preflight := loaderPreflightFixture(t, contents)
		preflight.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
		preflight.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
		if previous == nil {
			previous = preflight.Snapshot
		}
		invalidation := indexer.InvalidationInput{Before: previous, After: preflight.Snapshot, PolicyEpoch: 1, Scanner: preflight.Scanner, MaxManifestBytes: 32768, MaxChanges: 1000, MaxOutputBytes: 512 * 1024,
			AuthorizeScope: func(context.Context, string, string, string, int64) error { return nil }, AuthorizePath: func(context.Context, string, string, string, string, int64) error { return nil }}
		cache, err := indexer.ReuseCapturedContent(ctx, indexer.ContentReuseInput{Invalidation: invalidation, MaxSourceBytes: 65536, MaxReuseFiles: 100})
		if err != nil || cache.SemanticMode != "full_rebuild" || cache.Captured == nil || cache.Captured.Manifest().SnapshotDigest != preflight.Snapshot.Manifest().SnapshotDigest {
			t.Fatalf("cache step %d: %v", stepIndex, err)
		}
		partition := func(captured *snapshot.Snapshot) indexer.Partition {
			manifest := captured.Manifest()
			files := []string{}
			for _, item := range manifest.Files {
				if strings.HasSuffix(item.Path, ".go") && item.Status == "captured" {
					files = append(files, item.Path)
				}
			}
			return indexer.Partition{ID: "fixture-package", Variant: "production", Files: files, Imports: []string{}, InputFingerprint: manifest.SnapshotDigest}
		}
		partitionPlan, err := indexer.PlanPartitions(ctx, indexer.PartitionInput{Invalidation: invalidation, Before: []indexer.Partition{partition(previous)}, After: []indexer.Partition{partition(preflight.Snapshot)}, MaxPartitions: 10, MaxEdges: 100,
			VerifyPartitions: func(context.Context, model.CaptureManifest, string) (bool, error) { return true, nil }})
		if err != nil || partitionPlan.SemanticMode != "full_rebuild" || !partitionPlan.NegativeCacheInvalidated || !reflect.DeepEqual(partitionPlan.Affected, []string{"fixture-package"}) {
			t.Fatalf("partition fallback step %d: %v", stepIndex, err)
		}
		request := TypedInput{Loader: CapturedLoaderInput{Preflight: preflight, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace, Tool: tool, MaxOutputBytes: 65536,
			VerifyScratch: LoaderScratchVerifier(root, func(context.Context) error { return nil })}, PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOccurrences: 1000, MaxOutputBytes: 256 * 1024}
		full, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: request, MaxRelations: 2000, MaxOutputBytes: 512 * 1024})
		if err != nil || !full.Typed.Loader.BuildSelectionVerified {
			t.Fatalf("full graph step %d: %v", stepIndex, err)
		}
		request.Loader.Preflight.Snapshot = cache.Captured
		request.SyntaxCache = syntaxCache
		reused, err := AnalyzeCapturedGraph(ctx, GraphInput{Typed: request, MaxRelations: 2000, MaxOutputBytes: 512 * 1024})
		if err != nil || !reflect.DeepEqual(full, reused) {
			t.Fatalf("content reuse differed from full graph at step %d: %v", stepIndex, err)
		}
		var target string
		for _, syntax := range full.Typed.Syntax {
			for _, symbol := range syntax.Symbols {
				if symbol.Kind == "function" && symbol.Name == "Target" {
					target = symbol.SymbolID
				}
			}
		}
		count := 0
		for _, relation := range full.Relations {
			if relation.Kind == "CALLS_DIRECT" && relation.TargetSymbolID != nil && *relation.TargetSymbolID == target {
				count++
			}
		}
		want := current.calls
		if current.added {
			want++
		}
		if target == "" || count != want || full.Status != "partial" {
			t.Fatalf("caller count or boundary wrong at step %d: got=%d want=%d", stepIndex, count, want)
		}
		previous = preflight.Snapshot
	}
	if stats := syntaxCache.Stats(); stats.Hits == 0 {
		t.Fatal("mutation fixture did not reuse unchanged syntax parses")
	}
}
