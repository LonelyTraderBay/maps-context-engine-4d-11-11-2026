package index

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search/semantic"
)

func TestC075SemanticCandidatesReusePublishedSQLiteSearch(t *testing.T) {
	store, capture, request := searchFixture(t)
	request.Mode, request.Query = "text", "feature"
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	baseline, err := store.Search(ctx, request)
	if err != nil || len(baseline.Text) != 2 {
		t.Fatalf("fixture lexical results: %v, %v", baseline.Text, err)
	}
	manifest := capture.Snapshot.Manifest()
	scope := model.ScopeVector{Kind: "single", CatalogIncarnation: "55555555-5555-4555-8555-555555555555",
		UnavailableMembers: []model.MemberUnavailable{}, Members: []model.ScopeMember{{
			WorktreeID: baseline.WorktreeID, ProfileID: baseline.ProfileID, GenerationID: baseline.GenerationID,
			SnapshotDigest: baseline.SnapshotDigest, IndexIncarnation: baseline.IndexIncarnation,
			PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1,
			RecordsIncarnation: "44444444-4444-4444-8444-444444444444",
		}}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	symbolRequest := request
	symbolRequest.Mode, symbolRequest.Query = "exact", "Same"
	symbols, err := store.Search(ctx, symbolRequest)
	if err != nil || len(symbols.Symbols) != 2 || !symbols.Ambiguous {
		t.Fatalf("fixture exact name collision: %v, %v", symbols.Symbols, err)
	}
	var docs []semantic.Document
	for _, symbol := range symbols.Symbols {
		docs = append(docs, semantic.Document{SymbolID: symbol.Symbol.SymbolID, Source: *symbol.Source})
	}
	wrong := docs[0]
	wrong.SymbolID = "invented-by-vector"
	docs = append(docs, wrong)
	material := []byte(`{"model_id":"fixture-vocabulary","model_version":"1","license":"MIT","dimensions":2,"weights":{"feature":[1,0],"same":[1,0]}}`)
	key := semantic.Key{ScopeDigest: scope.VectorDigest, PrincipalDigest: strings.Repeat("c", 64), Model: semantic.ModelPin{
		ModelID: "fixture-vocabulary", ModelVersion: "1", License: "MIT", SHA256: fmt.Sprintf("%x", sha256.Sum256(material)),
		Tokenizer: semantic.TokenizerVersion, Index: semantic.IndexVersion, Dimensions: 2,
	}}
	var revoked bool
	authorize := func(ctx context.Context, requested semantic.Key, _ *model.SourceRef) error {
		if revoked || requested != key {
			return semantic.ErrScope
		}
		return ctx.Err()
	}
	cache, err := semantic.Build(ctx, key, scope, capture.Snapshot, material, docs, authorize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cache.Close)
	with, err := semantic.Retrieve(ctx, store, request, key, scope, cache, authorize)
	if err != nil || !reflect.DeepEqual(with.Baseline, baseline) || len(with.Candidates) != 2 ||
		with.Candidates[0].Document != docs[0] || with.Candidates[1].Document != docs[1] || with.Candidates[0].Similarity != 1 {
		t.Fatalf("supplemental results: %+v, %v", with, err)
	}
	request.Mode, request.Query = "exact", "Same"
	ordinary, err := semantic.Retrieve(ctx, store, request, key, scope, cache, authorize)
	if err != nil || !reflect.DeepEqual(ordinary.Baseline, symbols) || len(ordinary.Candidates) != 0 {
		t.Fatalf("ordinary exact search changed: %+v, %v", ordinary, err)
	}
	request.Mode, request.Query = "text", "feature"
	revoked = true
	denied, err := semantic.Retrieve(ctx, store, request, key, scope, cache, authorize)
	if err != nil || !reflect.DeepEqual(denied.Baseline, baseline) || len(denied.Candidates) != 0 {
		t.Fatalf("capability revocation affected baseline or leaked candidate: %+v, %v", denied, err)
	}
	revoked = false
	after, err := semantic.Retrieve(ctx, store, request, key, scope, cache, authorize)
	if err != nil || len(after.Candidates) != 0 {
		t.Fatalf("revoked cache reused: %+v, %v", after.Candidates, err)
	}
}
