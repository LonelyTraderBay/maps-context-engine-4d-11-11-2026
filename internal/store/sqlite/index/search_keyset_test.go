package index

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

func TestSearchKeysetVisitsEveryPublishedSymbolOnce(t *testing.T) {
	store, stage := providerStageFixture(t)
	stage.MaxFrameBytes = 4 * 1024 * 1024
	for i := 136; i >= 0; i-- {
		symbol := stage.Batch.Symbols[0]
		symbol.SymbolID, symbol.Name = fmt.Sprintf("keyset-%03d", i), fmt.Sprintf("Needle%03d", i)
		symbol.QualifiedName = "pkg." + symbol.Name
		stage.Batch.Symbols = append(stage.Batch.Symbols, symbol)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatal(err)
	}
	publication := publishProviderFixture(stage)
	publication.MaxFacts = 200
	if err := store.PublishProvider(ctx, publication); err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	request := search.Request{ProfileID: stage.Capture.Snapshot.Manifest().ProfileID, Mode: "prefix", Query: "Needle", Limit: 7, Keyset: true, MaxOutputBytes: 65536, MaxManifestBytes: 8192, MaxFileBytes: 4096, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: stage.Capture.Scanner}
	seen, last := map[string]bool{}, ""
	for page := 0; page < 30; page++ {
		result, err := store.Search(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if page == 0 {
			request.GenerationID = result.GenerationID
		}
		for _, hit := range result.Symbols {
			id := hit.Symbol.SymbolID
			if seen[id] || id <= last {
				t.Fatalf("duplicate or unordered symbol %q after %q", id, last)
			}
			seen[id], last = true, id
		}
		if !result.Truncated {
			if result.NextKey != "" || len(seen) != 137 {
				t.Fatalf("incomplete results: %d %q", len(seen), result.NextKey)
			}
			return
		}
		if result.NextKey == "" || result.NextKey != last || result.NextKey <= request.AfterID {
			t.Fatalf("no keyset progress: %+v", result)
		}
		request.AfterID = result.NextKey
	}
	t.Fatal("keyset did not finish")
}

func TestLocateSignedKeysetBindsRequestAndPublishedScope(t *testing.T) {
	store, capture, base := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manifest := capture.Snapshot.Manifest()
	member := model.ScopeMember{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: capture.GenerationID, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 0, IndexIncarnation: store.identity.Incarnation, RecordsIncarnation: fixtureGeneration}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: fixtureGeneration, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 32768)
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), OrderVersion: "locate-id-v1", Scope: scope}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	query := "Same"
	request := model.LocateRequest{Mode: "symbol", Query: &query, Limit: 1}
	first, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || first.NextCursor == nil || len(first.Result.Symbols) != 1 {
		t.Fatalf("first page: %+v %v", first, err)
	}
	request.Cursor, request.Limit = first.NextCursor, 2
	second, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || second.NextCursor != nil || len(second.Result.Symbols) != 1 || second.Result.Symbols[0].Symbol.SymbolID == first.Result.Symbols[0].Symbol.SymbolID {
		t.Fatalf("second page: %+v %v", second, err)
	}
	changed := "Different"
	request.Query = &changed
	if _, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("changed query accepted: %v", err)
	}
	request.Query = &query
	if _, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "other", request, base); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("changed actor accepted: %v", err)
	}
	denied := errors.New("revoked")
	if page, err := search.LocateKeysetPage(ctx, store, codec, binding, func(context.Context, contextpack.CursorBinding) error { return denied }, "actor", request, base); !errors.Is(err, denied) || len(page.Result.Symbols) != 0 {
		t.Fatalf("revoked page leaked: %+v %v", page, err)
	}
	bad := *first.NextCursor + "x"
	request.Cursor = &bad
	if _, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("tampered cursor accepted: %v", err)
	}
	request.Cursor, request.Limit = nil, 1
	request.Kinds = []string{"function"}
	filtered, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || filtered.NextCursor == nil || len(filtered.Result.Symbols) != 1 || filtered.Result.Symbols[0].Symbol.Kind != "function" {
		t.Fatalf("filtered first page: %+v %v", filtered, err)
	}
	request.Cursor = filtered.NextCursor
	filteredNext, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || filteredNext.NextCursor != nil || len(filteredNext.Result.Symbols) != 1 || filteredNext.Result.Symbols[0].Symbol.Kind != "function" || filteredNext.Result.Symbols[0].Symbol.SymbolID == filtered.Result.Symbols[0].Symbol.SymbolID {
		t.Fatalf("filtered continuation: %+v %v", filteredNext, err)
	}
	request.Cursor, request.Kinds = filtered.NextCursor, []string{"type"}
	if _, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("changed kind filter accepted: %v", err)
	}
	packagePath := "example.invalid/1"
	request.Kinds, request.Package = []string{"function"}, &packagePath
	if _, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("changed package filter accepted: %v", err)
	}
	request.Cursor = nil
	selected, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || selected.NextCursor != nil || len(selected.Result.Symbols) != 1 || selected.Result.Symbols[0].Symbol.SymbolID != "symbol1" || selected.Result.Ambiguous {
		t.Fatalf("exact package filter: %+v %v", selected, err)
	}
	request.Package, request.Kinds = nil, []string{"type"}
	empty, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || len(empty.Result.Symbols) != 0 || empty.NextCursor != nil {
		t.Fatalf("nonmatching kind returned results: %+v %v", empty, err)
	}
	request.Kinds = []string{"function' OR 1=1 --"}
	empty, err = search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || len(empty.Result.Symbols) != 0 {
		t.Fatalf("kind filter was not parameterized: %+v %v", empty, err)
	}
	packagePath = "example.invalid/1' OR 1=1 --"
	request.Package, request.Kinds = &packagePath, nil
	empty, err = search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base)
	if err != nil || len(empty.Result.Symbols) != 0 {
		t.Fatalf("package filter was not parameterized: %+v %v", empty, err)
	}
	request.Mode = "text"
	if _, err := search.LocateKeysetPage(ctx, store, codec, binding, guard, "actor", request, base); !errors.Is(err, search.ErrLocateUnsupported) {
		t.Fatalf("unsupported text filter ignored: %v", err)
	}
}

func TestSearchKeysetPinsGenerationAndRechecksAuthority(t *testing.T) {
	store, _, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request.Mode, request.Query, request.Limit, request.Keyset = "exact", "Same", 1, true
	first, err := store.Search(ctx, request)
	if err != nil || !first.Truncated || first.NextKey == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	request.AfterID = first.NextKey
	if _, err := store.Search(ctx, request); !errors.Is(err, ErrInvalidSearch) {
		t.Fatalf("unbound generation accepted: %v", err)
	}
	request.GenerationID = first.GenerationID
	second, err := store.Search(ctx, request)
	if err != nil || len(second.Symbols) != 1 || second.Truncated || !second.Ambiguous || second.Symbols[0].Symbol.SymbolID == first.Symbols[0].Symbol.SymbolID {
		t.Fatalf("second page: %+v %v", second, err)
	}
	denied := errors.New("revoked")
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error { return denied }
	if result, err := store.Search(ctx, request); !errors.Is(err, denied) || result.GenerationID != "" {
		t.Fatalf("revocation leaked results: %+v %v", result, err)
	}
}

func TestSearchPackageFilterRetainsAllVariants(t *testing.T) {
	store, stage := providerStageFixture(t)
	variant := stage.Batch.Packages[0]
	variant.PackageKey, variant.Variant = "pkg-test", "internal_test"
	stage.Batch.Packages = append(stage.Batch.Packages, variant)
	symbol := stage.Batch.Symbols[0]
	symbol.SymbolID, symbol.PackageKey = "symbol-test", &variant.PackageKey
	stage.Batch.Symbols = append(stage.Batch.Symbols, symbol)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishProvider(ctx, publishProviderFixture(stage)); err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	request := search.Request{ProfileID: stage.Capture.Snapshot.Manifest().ProfileID, Mode: "prefix", Query: "main", Package: "example.invalid/pkg", Limit: 10, Keyset: true, MaxOutputBytes: 65536, MaxManifestBytes: 8192, MaxFileBytes: 4096, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: stage.Capture.Scanner}
	result, err := store.Search(ctx, request)
	if err != nil || !result.Ambiguous || len(result.Symbols) != 2 {
		t.Fatalf("package variants lost or ambiguity hidden: %+v %v", result, err)
	}
}

func TestSearchTextKeysetUsesImmutableDocumentIDs(t *testing.T) {
	store, _, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request.Mode, request.Query, request.Limit, request.Keyset = "text", "feature", 1, true
	first, err := store.Search(ctx, request)
	if err != nil || !first.Truncated || first.NextKey == "" || len(first.Text) != 1 {
		t.Fatalf("first text: %+v %v", first, err)
	}
	request.GenerationID, request.AfterID = first.GenerationID, first.NextKey
	second, err := store.Search(ctx, request)
	if err != nil || second.Truncated || second.NextKey != "" || len(second.Text) != 1 || first.Text[0].Path == second.Text[0].Path {
		t.Fatalf("second text: %+v %v", second, err)
	}
	if strings.Join([]string{first.Text[0].EvidenceKind, second.Text[0].EvidenceKind}, ",") != "text_match,text_match" {
		t.Fatal("text hits lost their evidence kind")
	}
}
