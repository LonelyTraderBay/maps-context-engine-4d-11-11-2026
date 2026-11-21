package semantic

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestC075ModelPinsAndSourcePrivacy(t *testing.T) {
	f := newFixture(t)
	if len(f.index(t).state.items) != 1 {
		t.Fatal("an excluded file contributed a vector")
	}
	for name, change := range map[string]func(*Key){
		"version":    func(k *Key) { k.Model.ModelVersion = "2" },
		"tokenizer":  func(k *Key) { k.Model.Tokenizer = "foreign" },
		"dimensions": func(k *Key) { k.Model.Dimensions++ },
		"index":      func(k *Key) { k.Model.Index = "foreign" },
		"digest":     func(k *Key) { k.Model.SHA256 = strings.Repeat("0", 64) },
		"license":    func(k *Key) { k.Model.License = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			key := f.key
			change(&key)
			if _, err := Build(t.Context(), key, f.scope, f.snapshot, f.material, f.docs, allowAll); !errors.Is(err, ErrModel) {
				t.Fatalf("invalid model pin: %v", err)
			}
		})
	}
	bad := append([]Document(nil), f.docs...)
	bad[0].Source.ContentHash = strings.Repeat("0", 64)
	if _, err := Build(t.Context(), f.key, f.scope, f.snapshot, f.material, bad, allowAll); !errors.Is(err, ErrDocuments) {
		t.Fatalf("wrong capture hash: %v", err)
	}
	bad[0].Source.Path = ".env"
	if _, err := Build(t.Context(), f.key, f.scope, f.snapshot, f.material, bad, allowAll); !errors.Is(err, ErrDocuments) {
		t.Fatalf("excluded path: %v", err)
	}
	newPin := f.key.Model
	invalid := []byte(strings.Replace(string(f.material), `"weights":{`, `"extra":false,"weights":{`, 1))
	newPin.SHA256 = fmt.Sprintf("%x", sha256.Sum256(invalid))
	if _, err := openModel(newPin, invalid); !errors.Is(err, ErrModel) {
		t.Fatalf("unknown model field: %v", err)
	}
}

func TestC075CacheKeyAndFallback(t *testing.T) {
	f := newFixture(t)
	var calls int
	reader := readerFunc(func(_ context.Context, got search.Request) (search.Result, error) {
		calls++
		if got.Mode == "text" {
			return f.baseline, nil
		}
		return f.exact, nil
	})
	for _, query := range []string{"word-unrecognized", "password = " + strings.Repeat("f", 24)} {
		calls = 0
		req := search.Request{Mode: "text", Query: query, Limit: 10}
		out, err := Retrieve(t.Context(), reader, req, f.key, f.scope, f.index(t), allowAll)
		if err != nil || !reflect.DeepEqual(out.Baseline, f.baseline) || len(out.Candidates) != 0 || calls != 1 {
			t.Fatalf("fallback called resolver: %d calls, %v", calls, err)
		}
	}
	for _, field := range []string{"model", "tokenizer", "index", "policy_epoch", "principal"} {
		t.Run(field, func(t *testing.T) {
			index := f.index(t)
			key, scope := f.key, f.scope
			switch field {
			case "model":
				key.Model.ModelVersion = "2"
			case "tokenizer":
				key.Model.Tokenizer = "unicode-words-v2"
			case "index":
				key.Model.Index = "cosine-memory-v2"
			case "policy_epoch":
				scope.Members = append([]model.ScopeMember(nil), scope.Members...)
				scope.Members[0].PolicyEpoch++
				key.ScopeDigest = setScopeDigest(t, &scope)
			case "principal":
				key.PrincipalDigest = strings.Repeat("1", 64)
			}
			calls = 0
			query := search.Request{Mode: "text", Query: "lookup", Limit: 4}
			response, err := Retrieve(t.Context(), reader, query, key, scope, index, allowAll)
			if err != nil || calls != 1 || len(response.Candidates) != 0 || !reflect.DeepEqual(response.Baseline, f.baseline) {
				t.Fatalf("stale cache changed fallback: %+v, %v", response, err)
			}
			if index.current(f.key) != nil {
				t.Fatal("stale cache remained available")
			}
		})
	}
}

func TestC075ExactBindingAndFinalPolicyCheck(t *testing.T) {
	f := newFixture(t)
	for _, fault := range []string{"none", "ambiguous", "wrong-source", "wrong-symbol", "foreign-index", "revoked-after-read", "discarded"} {
		t.Run(fault, func(t *testing.T) {
			index := f.index(t)
			revoked := false
			reader := readerFunc(func(_ context.Context, got search.Request) (search.Result, error) {
				if got.Mode == "text" {
					return f.baseline, nil
				}
				exact := f.exact
				exact.Symbols = append([]search.SymbolHit(nil), f.exact.Symbols...)
				if got.Mode != "position" || got.Position.Kind != "source_position" || got.Position.ContentHash != f.docs[0].Source.ContentHash || got.GenerationID != f.scope.Members[0].GenerationID {
					t.Fatalf("lookup did not pin exact source: %+v", got.Position)
				}
				switch fault {
				case "ambiguous":
					exact.Ambiguous = true
				case "wrong-source":
					source := *exact.Symbols[0].Source
					source.Range.StartByte++
					exact.Symbols[0].Source = &source
				case "wrong-symbol":
					exact.Symbols[0].Symbol.SymbolID = "different-symbol"
				case "foreign-index":
					exact.IndexIncarnation = "changed"
				case "revoked-after-read":
					revoked = true
				case "discarded":
					index.Close()
				}
				return exact, nil
			})
			guard := func(ctx context.Context, _ Key, ref *model.SourceRef) error {
				if revoked && ref != nil {
					return ErrScope
				}
				return ctx.Err()
			}
			response, err := Retrieve(t.Context(), reader, search.Request{Mode: "text", Query: "lookup", Limit: 2}, f.key, f.scope, index, guard)
			if err != nil || !reflect.DeepEqual(response.Baseline, f.baseline) {
				t.Fatalf("baseline changed: %v", err)
			}
			if fault == "none" {
				if len(response.Candidates) != 1 || response.Candidates[0].Document != f.docs[0] {
					t.Fatalf("approved semantic candidate missing: %+v", response.Candidates)
				}
			} else if len(response.Candidates) != 0 {
				t.Fatalf("unverified candidate returned: %+v", response.Candidates)
			}
		})
	}
}

type fixture struct {
	key             Key
	scope           model.ScopeVector
	snapshot        *snapshot.Snapshot
	material        []byte
	docs            []Document
	baseline, exact search.Result
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "22222222-2222-4222-8222-222222222222"
	profile := strings.Repeat("a", 64)
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profile,
		PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), DependencyInputDigests: []string{}}
	var files []snapshot.File
	var docs []Document
	for _, seed := range []struct{ path, code string }{{"public.go", "func Find() {}"},
		{"blocked.go", "func Find() {}\n// password = " + strings.Repeat("f", 24)}} {
		file, err := snapshot.NewFile(seed.path, []byte(seed.code), 4096)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: seed.path, Status: "captured", ContentHash: &hash, ByteLength: &size})
		files = append(files, file)
		docs = append(docs, Document{SymbolID: "symbol-" + seed.path, Source: model.SourceRef{WorktreeID: worktree,
			ProfileID: profile, GenerationID: generation, Path: seed.path, ContentHash: hash,
			Range: model.ByteRange{StartByte: 5, EndByte: 9}}})
	}
	captured, err := snapshot.Freeze(manifest, files, 8192, 4096)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: profile, GenerationID: generation,
		SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1,
		RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555",
		UnavailableMembers: []model.MemberUnavailable{}}
	setScopeDigest(t, &scope)
	material := []byte(`{"model_id":"fixture","model_version":"1","license":"0BSD","dimensions":2,"weights":{"find":[2,1],"lookup":[3,2],"password":[2,1]}}`)
	key := Key{ScopeDigest: scope.VectorDigest, PrincipalDigest: strings.Repeat("d", 64), Model: ModelPin{
		ModelID: "fixture", ModelVersion: "1", License: "0BSD", SHA256: fmt.Sprintf("%x", sha256.Sum256(material)),
		Tokenizer: TokenizerVersion, Index: IndexVersion, Dimensions: 2,
	}}
	base := search.Result{WorktreeID: worktree, ProfileID: profile, GenerationID: generation,
		SnapshotDigest: member.SnapshotDigest, IndexIncarnation: member.IndexIncarnation, State: "READY",
		Text: []search.TextHit{{Path: "unrelated.md", EvidenceKind: "text_match"}}}
	exact := base
	exact.Text = nil
	exact.Symbols = []search.SymbolHit{{Symbol: model.SymbolFact{SymbolID: docs[0].SymbolID}, Source: &docs[0].Source}}
	return fixture{key: key, snapshot: captured, scope: scope, material: material, docs: docs, baseline: base, exact: exact}
}

func (f fixture) index(t *testing.T) *Index {
	t.Helper()
	index, err := Build(t.Context(), f.key, f.scope, f.snapshot, f.material, f.docs, allowAll)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(index.Close)
	return index
}

func setScopeDigest(t *testing.T, scope *model.ScopeVector) string {
	t.Helper()
	var err error
	if scope.VectorDigest, err = model.ComputeScopeVectorDigest(*scope, 8192); err != nil {
		t.Fatal(err)
	}
	return scope.VectorDigest
}

func allowAll(ctx context.Context, _ Key, _ *model.SourceRef) error { return ctx.Err() }

type readerFunc func(context.Context, search.Request) (search.Result, error)

func (f readerFunc) Search(ctx context.Context, req search.Request) (search.Result, error) {
	return f(ctx, req)
}
