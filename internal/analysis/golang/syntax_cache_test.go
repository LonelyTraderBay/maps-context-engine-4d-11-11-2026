package golang

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func cacheSnapshotVariant(t *testing.T, input SyntaxInput, extra bool, edit func(*model.CaptureManifest, *model.GoProfile)) SyntaxInput {
	t.Helper()
	manifest := input.Snapshot.Manifest()
	profile := *input.Profile
	files := []snapshot.File{}
	for _, item := range manifest.Files {
		if f, ok := input.Snapshot.File(item.Path); ok {
			files = append(files, f)
		}
	}
	if extra {
		f, err := snapshot.NewFile("other.go", []byte("package sample\nvar Other int\n"), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := f.ContentHash(), int64(f.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: "other.go", Status: "captured", ContentHash: &hash, ByteLength: &size})
		files = append(files, f)
	}
	if edit != nil {
		edit(&manifest, &profile)
	}
	profileID, err := model.ProfileDigest(&profile, 8192)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ProfileID, manifest.ProviderDigest, manifest.SnapshotDigest = profileID, profile.ProviderDigest, ""
	captured, err := snapshot.Freeze(manifest, files, 8192, 65536)
	if err != nil {
		t.Fatal(err)
	}
	input.Profile, input.Snapshot = &profile, captured
	return input
}

func TestSyntaxCacheRebuildsFactsForNewSnapshot(t *testing.T) {
	input := syntaxFixture(t, "p.go", "package sample\nfunc Work() int { x:=1; return x }\n")
	cache, err := NewSyntaxCache(2*1024*1024, 4)
	if err != nil {
		t.Fatal(err)
	}
	input.Cache = cache
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if stats := cache.Stats(); stats.Misses != 1 || stats.Entries != 1 || stats.Hits != 0 {
		t.Fatalf("missing cold parse: %+v", stats)
	}
	secondInput := cacheSnapshotVariant(t, input, true, nil)
	got, err := AnalyzeSyntaxFile(ctx, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	freshInput := secondInput
	freshInput.Cache = nil
	want, err := AnalyzeSyntaxFile(ctx, freshInput)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || got.SnapshotDigest == first.SnapshotDigest || reflect.DeepEqual(got.Occurrences, first.Occurrences) {
		t.Fatal("cached syntax did not rebind facts to current snapshot")
	}
	if stats := cache.Stats(); stats.Hits != 1 || stats.Entries != 1 {
		t.Fatalf("unchanged source was reparsed: %+v", stats)
	}
}

func TestSyntaxCacheIsolatesPolicyProviderAndWorktree(t *testing.T) {
	input := syntaxFixture(t, "p.go", "package sample\nfunc Work() {}\n")
	cache, _ := NewSyntaxCache(2*1024*1024, 8)
	input.Cache = cache
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := AnalyzeSyntaxFile(ctx, input); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*model.CaptureManifest, *model.GoProfile){
		func(m *model.CaptureManifest, _ *model.GoProfile) { m.PolicyDigest = strings.Repeat("e", 64) },
		func(_ *model.CaptureManifest, p *model.GoProfile) { p.ProviderDigest = strings.Repeat("d", 64) },
		func(m *model.CaptureManifest, _ *model.GoProfile) {
			m.WorktreeID = "22222222-2222-4222-8222-222222222222"
		},
	} {
		variant := cacheSnapshotVariant(t, input, false, edit)
		if _, err := AnalyzeSyntaxFile(ctx, variant); err != nil {
			t.Fatal(err)
		}
	}
	if stats := cache.Stats(); stats.Misses != 4 || stats.Hits != 0 || stats.Entries != 4 {
		t.Fatalf("cache crossed trust scope: %+v", stats)
	}
	small, err := policy.NewScanner(1)
	if err != nil {
		t.Fatal(err)
	}
	input.Scanner = small
	if result, err := AnalyzeSyntaxFile(ctx, input); !errors.Is(err, policy.ErrContentLimit) || len(result.Symbols) != 0 {
		t.Fatalf("cache bypassed source scanner: %v", err)
	}
	if cache.Stats().Hits != 0 {
		t.Fatal("cache was consulted before source validation")
	}
}

func TestSyntaxCacheVerifiesBytesEvenWhenKeyMatches(t *testing.T) {
	input := syntaxFixture(t, "p.go", "package sample\nfunc Work() {}\n")
	cache, _ := NewSyntaxCache(2*1024*1024, 4)
	input.Cache = cache
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := AnalyzeSyntaxFile(ctx, input); err != nil {
		t.Fatal(err)
	}
	m := input.Snapshot.Manifest()
	file, _ := input.Snapshot.File(input.Path)
	key := syntaxCacheKey{worktree: m.WorktreeID, profile: m.ProfileID, policy: m.PolicyDigest, provider: input.Profile.ProviderDigest, toolchain: input.Profile.ToolchainDigest, path: input.Path, content: file.ContentHash(), options: "go-parser-comments-v1"}
	if _, _, hit, _ := cache.lookup(key, []byte("different bytes")); hit {
		t.Fatal("cache trusted a key without verifying bytes")
	}
}

func TestSyntaxCacheEvictionAndNoNegativeCaching(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cache, err := NewSyntaxCache(2*1024*1024, 1)
	if err != nil {
		t.Fatal(err)
	}
	first := syntaxFixture(t, "a.go", "package sample\nfunc A() {}\n")
	first.Cache = cache
	second := syntaxFixture(t, "b.go", "package sample\nfunc B() {}\n")
	second.Cache = cache
	for _, input := range []SyntaxInput{first, second, first} {
		if _, err := AnalyzeSyntaxFile(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if stats := cache.Stats(); stats.Entries != 1 || stats.Misses != 3 || stats.Evictions != 2 || stats.EstimatedBytes > 2*1024*1024 {
		t.Fatalf("bad bounded eviction: %+v", stats)
	}
	cache.Clear()
	broken := syntaxFixture(t, "broken.go", "package sample\nfunc ???\n")
	broken.Cache = cache
	for i := 0; i < 2; i++ {
		result, err := AnalyzeSyntaxFile(ctx, broken)
		if err != nil || len(result.Diagnostics) != 1 {
			t.Fatalf("parse diagnostic missing: %v", err)
		}
	}
	if cache.Stats().Entries != 0 {
		t.Fatal("negative parse result was cached")
	}
	if _, err := NewSyntaxCache(0, 1); !errors.Is(err, ErrSyntaxCacheConfig) {
		t.Fatal("zero byte budget accepted")
	}
}

func TestSyntaxCacheConcurrentReadersAndClearEpoch(t *testing.T) {
	input := syntaxFixture(t, "p.go", "package sample\nfunc Work(x int) int { return x+1 }\n")
	cache, _ := NewSyntaxCache(2*1024*1024, 4)
	input.Cache = cache
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	want, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := AnalyzeSyntaxFile(ctx, input)
			if err != nil {
				errs <- err
				return
			}
			if !reflect.DeepEqual(got, want) {
				errs <- errors.New("different cached result")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if cache.Stats().Hits != 16 {
		t.Fatal("concurrent cache hits missing")
	}
	key := syntaxCacheKey{path: "epoch"}
	source := []byte("package sample\n")
	_, _, _, epoch := cache.lookup(key, source)
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, "epoch.go", source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	cache.Clear()
	if err := cache.remember(ctx, key, source, files, parsed, 100, epoch); err != nil {
		t.Fatal(err)
	}
	if cache.Stats().Entries != 0 {
		t.Fatal("clear allowed an older in-flight parse to repopulate")
	}
}
