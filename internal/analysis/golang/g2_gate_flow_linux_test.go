//go:build linux

package golang

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/indexer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func TestG2CapturedDirtyWorkspacePublicationAndFreshness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const original = "package fixture\nimport \"example.test/g2/sub\"\nvar Value = 1\nfunc Same() {}\ntype Base struct{}\ntype Box[T any] struct { Base; Field T }\nfunc (b Box[T]) Method(v T) (result T) { result = v; return }\nfunc Target(v int) int {\n x := v\n { x := x + 1; _ = x }\n return x\n}\nfunc Closure() func() int { x := 4; _ = x; return func() int { x := 5; return x } }\nfunc Existing() int { Same(); sub.Same(); return Target(1) }\n"
	const secondary = "package sub\nfunc Same() {}\n"
	const newCaller = "package fixture\nfunc NewCaller() int { return Target(2) }\n"
	f := newG2Fixture(t, ctx, map[string]string{"go.mod": "module example.test/g2\n\ngo 1.27.4\n", "target.go": original, "sub/same.go": secondary})
	t.Setenv("PATH", filepath.Join(f.goRoot, "bin"))
	store := newG2Store(t, ctx, f)
	before, oldFiles := f.capture(t, ctx)
	assume := false
	for _, entry := range oldFiles.Entries {
		if entry.Path == "target.go" {
			assume = entry.AssumeUnchanged
		}
	}
	if !assume {
		t.Fatal("fixture did not retain assume-unchanged index flag")
	}
	if err := indexer.RevalidateLocal(ctx, f.revalidate(before, oldFiles)); err != nil {
		t.Fatalf("initial full verification: %v", err)
	}
	cache, err := NewSyntaxCache(4*1024*1024, 16)
	if err != nil {
		t.Fatal(err)
	}
	oldGraph := f.analyze(t, ctx, before, oldFiles, "scratch-old", cache)
	verifyG2ShadowAndDeclarations(t, oldGraph, original)
	store.publish(t, ctx, g2Old, before, oldFiles, oldGraph)
	allow := func(context.Context, string, string, string, int64) error { return nil }
	query := search.Request{ProfileID: before.Manifest().ProfileID, Mode: "exact", Query: "Same", Limit: 10, MaxOutputBytes: 65536, MaxManifestBytes: 32768, MaxFileBytes: 65536, AuthorizeGeneration: allow, AuthorizePath: allow, Scanner: f.scanner}
	lookup, err := store.store.Search(ctx, query)
	if err != nil || lookup.GenerationID != g2Old || lookup.SnapshotDigest != before.Manifest().SnapshotDigest || !lookup.Ambiguous || len(lookup.Symbols) != 2 || lookup.Freshness != "not_checked" {
		t.Fatalf("cross-package name ambiguity missing: %+v %v", lookup, err)
	}
	file, _ := before.File("target.go")
	query.Mode, query.Query = "position", ""
	query.Position = &model.PositionSelector{Kind: "source_position", Path: "target.go", ContentHash: file.ContentHash(), ByteOffset: int64(strings.Index(original, "func Same") + len("func "))}
	position, err := store.store.Search(ctx, query)
	if err != nil || len(position.Symbols) != 1 || position.Symbols[0].Symbol.Name != "Same" || position.Symbols[0].Source.Path != "target.go" {
		t.Fatalf("positional resolution failed: %+v %v", position, err)
	}
	request := index.PublishedFileRequest{ProfileID: before.Manifest().ProfileID, Path: "target.go", AuthorizeGeneration: allow, AuthorizePath: allow, Scanner: f.scanner, MaxManifestBytes: 32768, MaxFileBytes: 65536}
	oldRead, err := store.store.ReadPublishedFileSymbols(ctx, index.PublishedFileSymbolsRequest{File: request, MaxSymbols: 128, MaxFactsBytes: 1024 * 1024})
	if err != nil || oldRead.File.GenerationID != g2Old || string(oldRead.File.Bytes) != original || store.targetCallCount(t, ctx, g2Old) != 1 {
		t.Fatalf("old published generation mismatch: %v", err)
	}
	validation := f.validationInput(t, before, oldFiles, g2Old, oldRead.File.IndexIncarnation)
	valid, err := app.ValidateCapturedWorkspace(ctx, validation)
	if err != nil || !valid.Data.EditPreconditionValidated || valid.Freshness.Status != "observed_current" {
		t.Fatalf("initial freshness verification: %v", err)
	}
	targetPath := filepath.Join(f.repo, "target.go")
	stat, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(original, "var Value = 1", "var Value = 2", 1)
	if len(changed) != len(original) {
		t.Fatal("fixture did not preserve file size")
	}
	if err := os.WriteFile(targetPath, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(targetPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	changedStat, err := os.Stat(targetPath)
	if err != nil || changedStat.Size() != stat.Size() || !changedStat.ModTime().Equal(stat.ModTime()) {
		t.Fatalf("fixture changed file size or mtime: %v", err)
	}
	if stale, err := app.ValidateCapturedWorkspace(ctx, validation); !errors.Is(err, app.ErrFreshnessChanged) || stale.Data.EditPreconditionValidated {
		t.Fatalf("same-size same-mtime dirty file was accepted: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(targetPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, "new.go"), []byte(newCaller), 0600); err != nil {
		t.Fatal(err)
	}
	if stale, err := app.ValidateCapturedWorkspace(ctx, validation); !errors.Is(err, app.ErrFreshnessChanged) || stale.Data.EditPreconditionValidated {
		t.Fatalf("new caller was accepted as current: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(targetPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, newFiles := f.capture(t, ctx)
	untracked := false
	for _, entry := range newFiles.Entries {
		if entry.Path == "new.go" && entry.Origin == "untracked" {
			untracked = true
		}
	}
	if !untracked {
		t.Fatal("new caller was not inventoried as untracked")
	}
	invalid := indexer.InvalidationInput{Before: before, After: after, PolicyEpoch: 1, Scanner: f.scanner, MaxManifestBytes: 32768, MaxChanges: 100, MaxOutputBytes: 65536,
		AuthorizeScope: func(context.Context, string, string, string, int64) error { return nil }, AuthorizePath: func(context.Context, string, string, string, string, int64) error { return nil }}
	plan, err := indexer.PlanInvalidation(ctx, invalid)
	if err != nil || plan.Action != "rebuild" || len(plan.Changes) != 2 || plan.Changes[0].Path != "new.go" || plan.Changes[1].Path != "target.go" {
		t.Fatalf("dirty invalidation wrong: %+v %v", plan, err)
	}
	freshGraph := f.analyze(t, ctx, after, newFiles, "scratch-new-full", nil)
	newGraph := f.analyze(t, ctx, after, newFiles, "scratch-new-cache", cache)
	if !reflect.DeepEqual(freshGraph, newGraph) || cache.Stats().Hits == 0 {
		t.Fatal("cached parse and full analysis disagree or no cache hit")
	}
	verifyG2ShadowAndDeclarations(t, newGraph, changed)
	store.publish(t, ctx, g2New, after, newFiles, newGraph)
	active, err := store.store.ReadPublishedFileSymbols(ctx, index.PublishedFileSymbolsRequest{File: request, MaxSymbols: 128, MaxFactsBytes: 1024 * 1024})
	if err != nil || active.File.GenerationID != g2New || active.File.SnapshotDigest != after.Manifest().SnapshotDigest || string(active.File.Bytes) != changed || store.targetCallCount(t, ctx, g2New) != 2 || store.targetCallCount(t, ctx, g2Old) != 1 {
		t.Fatalf("generation transition lost or mixed facts: %v", err)
	}
	request.GenerationID = g2Old
	historical, err := store.store.ReadPublishedFile(ctx, request)
	if err != nil || historical.GenerationID != g2Old || string(historical.Bytes) != original {
		t.Fatalf("pinned historical source changed: %v", err)
	}
	current := f.validationInput(t, after, newFiles, g2New, active.File.IndexIncarnation)
	if valid, err := app.ValidateCapturedWorkspace(ctx, current); err != nil || !valid.Data.EditPreconditionValidated || valid.Freshness.Status != "observed_current" {
		t.Fatalf("new generation did not revalidate: %v", err)
	}
	changing := f.revalidate(after, newFiles)
	checks := 0
	changing.VerifyDependencies = func(context.Context, []string) error {
		checks++
		if checks == 2 {
			if err := os.WriteFile(targetPath, []byte(original), 0600); err != nil {
				return err
			}
			return os.Chtimes(targetPath, stat.ModTime(), stat.ModTime())
		}
		return nil
	}
	if err := indexer.RevalidateLocal(ctx, changing); !errors.Is(err, indexer.ErrSnapshotChanged) {
		t.Fatalf("source change between validation passes was missed: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(targetPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	if valid, err := app.ValidateCapturedWorkspace(ctx, current); err != nil || !valid.Data.EditPreconditionValidated {
		t.Fatalf("restored fixture did not revalidate: %v", err)
	}
}

func verifyG2ShadowAndDeclarations(t *testing.T, graph GraphResult, source string) {
	t.Helper()
	outer := strings.Index(source, "x := v")
	inner := strings.Index(source, "x := x + 1")
	positions := []int{outer, inner, inner + len("x := "), strings.Index(source, "_ = x") + len("_ = "), strings.Index(source, "return x") + len("return ")}
	ids := make([]string, len(positions))
	for _, occurrence := range graph.Typed.Occurrences {
		if occurrence.Path != "target.go" || occurrence.Name != "x" || occurrence.SymbolID == nil {
			continue
		}
		for i, position := range positions {
			if occurrence.Range.StartByte == int64(position) {
				ids[i] = *occurrence.SymbolID
			}
		}
	}
	if ids[0] == "" || ids[1] == "" || ids[0] == ids[1] || ids[2] != ids[0] || ids[3] != ids[1] || ids[4] != ids[0] {
		t.Fatalf("shadow bindings do not match lexical oracle: %v", ids)
	}
	closureOuter, closureInner := strings.Index(source, "x := 4"), strings.Index(source, "x := 5")
	closureReturn := strings.LastIndex(source, "return x") + len("return ")
	closureIDs := make([]string, 3)
	for _, occurrence := range graph.Typed.Occurrences {
		if occurrence.Path != "target.go" || occurrence.Name != "x" || occurrence.SymbolID == nil {
			continue
		}
		for i, position := range []int{closureOuter, closureInner, closureReturn} {
			if occurrence.Range.StartByte == int64(position) {
				closureIDs[i] = *occurrence.SymbolID
			}
		}
	}
	if closureIDs[0] == "" || closureIDs[1] == "" || closureIDs[0] == closureIDs[1] || closureIDs[2] != closureIDs[1] {
		t.Fatalf("closure shadow bindings disagree: %v", closureIDs)
	}
	seen := map[string]bool{}
	for _, syntax := range graph.Typed.Syntax {
		for _, symbol := range syntax.Symbols {
			seen[symbol.Kind+":"+symbol.Name] = true
		}
	}
	for _, key := range []string{"method:Method", "receiver:b", "result:result", "field:Field", "field:Base", "type:Base", "closure:"} {
		if !seen[key] {
			t.Fatalf("missing scoped declaration %s", key)
		}
	}
}
