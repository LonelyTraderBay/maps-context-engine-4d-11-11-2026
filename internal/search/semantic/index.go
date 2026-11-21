// Package semantic adds an opt-in, in-memory candidate search to an existing
// exact/lexical reader. Similarity never constructs graph or contract facts.
package semantic

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const (
	MaxDocuments    = 256
	MaxResults      = 16
	maxDocumentSize = 64 << 10
	maxQuerySize    = 512
	maxManifestSize = 8 << 20
)

var (
	ErrModel     = errors.New("semantic model pin or bytes are invalid")
	ErrScope     = errors.New("semantic scope is unavailable")
	ErrDocuments = errors.New("semantic document input is invalid")
)

// Key partitions and invalidates an index on a scope, principal or model
// change. ScopeDigest binds the capture, policy revision and store identities.
type Key struct {
	ScopeDigest, PrincipalDigest string
	Model                        ModelPin
}

// Authorize is the owner's current check of the optional capability and scope.
// A nil source is a scope check. A non-nil source also checks path permission.
type Authorize func(context.Context, Key, *model.SourceRef) error

type Document struct {
	SymbolID string
	Source   model.SourceRef
}

type Candidate struct {
	Document
	Similarity float64
}

type Result struct {
	Baseline   search.Result
	Candidates []Candidate
}

type entry struct {
	document Document
	vector   []float64
}

type generation struct {
	key   Key
	model vocabulary
	items []entry
}

type Index struct {
	mu    sync.Mutex
	state *generation
}

var scanner, scannerErr = policy.NewScanner(maxDocumentSize)

// Build indexes only explicitly supplied, owner-approved source ranges in one
// pinned capture. Vectors and model material are held in memory only.
func Build(ctx context.Context, key Key, scope model.ScopeVector, captured *snapshot.Snapshot, material []byte,
	docs []Document, authorize Authorize) (*Index, error) {
	if ctx == nil || !validScope(key, scope) || captured == nil || !allowed(ctx, key, nil, authorize) {
		return nil, ErrScope
	}
	if scannerErr != nil || len(docs) == 0 || len(docs) > MaxDocuments {
		return nil, ErrDocuments
	}
	loaded, err := openModel(key.Model, material)
	if err != nil {
		return nil, err
	}
	refs := make([]snapshot.ExpectedSource, 0, len(docs))
	seen := make(map[string]bool, len(docs))
	for _, doc := range docs {
		ref := doc.Source
		if !label.MatchString(doc.SymbolID) || seen[doc.SymbolID] || policy.CheckPath(ref.Path) != nil ||
			scanner.CheckContent([]byte(ref.Path)) != nil || ref.Range.StartByte >= ref.Range.EndByte {
			return nil, ErrDocuments
		}
		if !allowed(ctx, key, &ref, authorize) {
			return nil, ErrScope
		}
		seen[doc.SymbolID] = true
		refs = append(refs, snapshot.ExpectedSource{Ref: doc.Source})
	}
	if err := snapshot.ValidateCapturedSources(ctx, scope, refs, captured, maxManifestSize); err != nil {
		return nil, ErrDocuments
	}
	items := make([]entry, 0, len(docs))
	for _, doc := range docs {
		ref := doc.Source
		file, _ := captured.File(ref.Path)
		if !allowed(ctx, key, &ref, authorize) {
			return nil, ErrScope
		}
		if file.Len() > maxDocumentSize || scanner.CheckContent(file.Bytes()) != nil {
			continue
		}
		source, err := file.Range(int(ref.Range.StartByte), int(ref.Range.EndByte))
		if err != nil {
			return nil, ErrDocuments
		}
		if vector := loaded.embed(string(source)); vector != nil {
			items = append(items, entry{document: doc, vector: vector})
		}
	}
	if !allowed(ctx, key, nil, authorize) {
		return nil, ErrScope
	}
	return &Index{state: &generation{key: key, model: loaded, items: items}}, nil
}

// Retrieve always keeps the original baseline request and result. A candidate
// is supplemental and needs a unique exact position lookup in the same
// published generation, with matching symbol ID, range and source hash.
func Retrieve(ctx context.Context, reader search.Reader, request search.Request, key Key, scope model.ScopeVector,
	index *Index, authorize Authorize) (Result, error) {
	if ctx == nil || reader == nil {
		return Result{}, ErrScope
	}
	baseline, err := reader.Search(ctx, request)
	if err != nil {
		return Result{}, err
	}
	result := Result{Baseline: baseline, Candidates: []Candidate{}}
	if index == nil {
		return result, nil
	}
	state := index.current(key)
	if state == nil {
		return result, nil
	}
	if !validScope(key, scope) || !allowed(ctx, key, nil, authorize) || !matches(baseline, scope.Members[0]) {
		index.Close()
		return result, nil
	}
	if request.Mode != "text" || request.AfterID != "" || request.Limit <= 0 || len(request.Query) > maxQuerySize ||
		scanner.CheckContent([]byte(request.Query)) != nil {
		return result, nil
	}
	query := state.model.embed(request.Query)
	if query == nil {
		return result, nil
	}
	member := scope.Members[0]
	for _, candidate := range state.rank(query, min(request.Limit, MaxResults)) {
		ref := candidate.Source
		if !allowed(ctx, key, &ref, authorize) {
			index.Close()
			return Result{Baseline: baseline, Candidates: []Candidate{}}, nil
		}
		positional := request
		positional.ProfileID, positional.GenerationID = member.ProfileID, member.GenerationID
		positional.Mode, positional.Query, positional.Qualified = "position", "", false
		positional.Keyset, positional.AfterID, positional.Limit = false, "", 2
		positional.Position = &model.PositionSelector{Kind: "source_position", Path: ref.Path,
			ContentHash: ref.ContentHash, ByteOffset: ref.Range.StartByte}
		exact, err := reader.Search(ctx, positional)
		if err == nil && matches(exact, member) && exactMatch(exact, candidate.Document) {
			result.Candidates = append(result.Candidates, candidate)
		}
	}
	for _, candidate := range result.Candidates {
		ref := candidate.Source
		if !allowed(ctx, key, &ref, authorize) {
			index.Close()
			return Result{Baseline: baseline, Candidates: []Candidate{}}, nil
		}
	}
	if !allowed(ctx, key, nil, authorize) {
		index.Close()
		result.Candidates = []Candidate{}
	} else if index.current(key) != state {
		result.Candidates = []Candidate{}
	}
	return result, nil
}

// Close discards the optional cache without changing the underlying reader.
func (i *Index) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.state = nil
}

func (i *Index) current(key Key) *generation {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != nil && i.state.key != key {
		i.state = nil
	}
	return i.state
}

func (g *generation) rank(query []float64, limit int) []Candidate {
	rows := make([]Candidate, 0, len(g.items))
	for _, item := range g.items {
		var score float64
		for i, weight := range query {
			score += weight * item.vector[i]
		}
		if score > 0 {
			rows = append(rows, Candidate{Document: item.document, Similarity: min(1, score)})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Similarity != rows[j].Similarity {
			return rows[i].Similarity > rows[j].Similarity
		}
		return rows[i].SymbolID < rows[j].SymbolID
	})
	return rows[:min(limit, len(rows))]
}

func allowed(ctx context.Context, key Key, ref *model.SourceRef, authorize Authorize) bool {
	return authorize != nil && ctx.Err() == nil && authorize(ctx, key, ref) == nil && ctx.Err() == nil
}

func validScope(key Key, scope model.ScopeVector) bool {
	return model.ValidDigest(key.PrincipalDigest) && key.ScopeDigest == scope.VectorDigest && scope.Kind == "single" &&
		len(scope.Members) == 1 && len(scope.UnavailableMembers) == 0 && model.VerifyScopeVectorDigest(scope, maxManifestSize) == nil
}

func matches(result search.Result, member model.ScopeMember) bool {
	return (result.State == "READY" || result.State == "READY_PARTIAL") && result.WorktreeID == member.WorktreeID && result.ProfileID == member.ProfileID &&
		result.GenerationID == member.GenerationID && result.SnapshotDigest == member.SnapshotDigest &&
		result.IndexIncarnation == member.IndexIncarnation
}

func exactMatch(result search.Result, doc Document) bool {
	return !result.Ambiguous && !result.Truncated && len(result.Symbols) == 1 && result.Symbols[0].Source != nil &&
		result.Symbols[0].Symbol.SymbolID == doc.SymbolID && *result.Symbols[0].Source == doc.Source
}
