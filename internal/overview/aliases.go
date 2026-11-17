package overview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// AliasDeclaration maps one exact, captured vocabulary spelling to an entity.
// It is a candidate-lookup hint and does not imply graph edges or identity merge.
type AliasDeclaration struct {
	Alias  string          `json:"alias"`
	Entity model.EntityRef `json:"entity"`
	Source model.SourceRef `json:"source"`
}

type AliasInput struct {
	Overview     *Report
	Snapshot     *snapshot.Snapshot
	Scanner      *policy.Scanner
	Declarations []AliasDeclaration
	// These trusted callbacks prove complete approved configuration at its
	// current revision and the declared entity mapping; they must recheck on use.
	VerifyAliasSet func(context.Context, Scope, string) error
	AuthorizeAlias func(context.Context, Scope, AliasDeclaration) error
	MaxBytes       int
}

type AliasCandidate struct {
	Entity     model.EntityRef   `json:"entity"`
	SourceRefs []model.SourceRef `json:"source_refs"`
}

type AliasResult struct {
	Candidates []AliasCandidate `json:"candidates"`
	Ambiguous  bool             `json:"ambiguous"`
}

type AliasIndex struct {
	overview       *Report
	entries        []AliasDeclaration
	paths          []string
	digest         string
	scanner        *policy.Scanner
	verify         func(context.Context, Scope, string) error
	authorizeAlias func(context.Context, Scope, AliasDeclaration) error
	maxBytes       int
}

func normalizeAliasSet(entries []AliasDeclaration, max int) ([]AliasDeclaration, string, error) {
	type normalized struct {
		entry AliasDeclaration
		raw   string
	}
	values := make([]normalized, 0, len(entries))
	used := 0
	for _, entry := range entries {
		raw, err := json.Marshal(entry)
		if err != nil || len(raw) > max-used {
			return nil, "", ErrLimit
		}
		used += len(raw)
		values = append(values, normalized{entry: entry, raw: string(raw)})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].raw < values[j].raw })
	out := []AliasDeclaration{}
	for i, item := range values {
		if i == 0 || item.raw != values[i-1].raw {
			out = append(out, item.entry)
		}
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > max {
		return nil, "", ErrLimit
	}
	sum := sha256.Sum256(append([]byte("mce-alias-set-v1\n"), raw...))
	return out, hex.EncodeToString(sum[:]), nil
}

func (a *AliasIndex) authorize(ctx context.Context) error {
	if a == nil || ctx == nil || a.overview == nil || a.verify == nil || a.authorizeAlias == nil {
		return ErrInput
	}
	if err := a.overview.authorize(ctx, a.paths); err != nil {
		return err
	}
	if err := a.verify(ctx, a.overview.scope, a.digest); err != nil {
		return authorityError(ctx)
	}
	for _, entry := range a.entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.authorizeAlias(ctx, a.overview.scope, entry); err != nil {
			return authorityError(ctx)
		}
	}
	if err := a.verify(ctx, a.overview.scope, a.digest); err != nil {
		return authorityError(ctx)
	}
	return a.overview.authorize(ctx, a.paths)
}

// BuildAliasIndex never infers aliases from names or unapproved prose. The
// verifier must bind the complete normalized set to the approved config.
func BuildAliasIndex(ctx context.Context, in AliasInput) (*AliasIndex, error) {
	if ctx == nil || in.Overview == nil || in.Snapshot == nil || in.Scanner == nil || in.VerifyAliasSet == nil || in.AuthorizeAlias == nil || len(in.Declarations) > 10000 || in.MaxBytes < 4096 || in.MaxBytes > 16*1024*1024 {
		return nil, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInput
	}
	scope := in.Overview.scope
	manifest := in.Snapshot.Manifest()
	if manifest.WorktreeID != scope.Identity.WorktreeID || manifest.ProfileID != scope.Identity.ProfileID || manifest.SnapshotDigest != scope.SnapshotDigest || manifest.PolicyDigest != scope.PolicyDigest || model.VerifyCaptureDigest(manifest, 64*1024*1024) != nil {
		return nil, ErrInput
	}
	a := &AliasIndex{overview: in.Overview, scanner: in.Scanner, verify: in.VerifyAliasSet, authorizeAlias: in.AuthorizeAlias, maxBytes: in.MaxBytes}
	scanned := map[string]snapshot.File{}
	var scannedBytes int64
	for _, entry := range in.Declarations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(entry.Alias) > 4096 || !utf8.ValidString(entry.Alias) || utf8.RuneCountInString(entry.Alias) < 1 || utf8.RuneCountInString(entry.Alias) > 512 || !boundedSourceRef(entry.Source) || len(entry.Entity.Kind) > 32 || len(entry.Entity.WorktreeID) != 36 || len(entry.Entity.ProfileID) != 64 || len(entry.Entity.GenerationID) != 36 || len(entry.Entity.EntityID) > 768 || in.Scanner.CheckContent([]byte(entry.Alias)) != nil || in.Scanner.CheckContent([]byte(entry.Entity.EntityID)) != nil || policy.CheckPath(entry.Source.Path) != nil || in.Scanner.CheckContent([]byte(entry.Source.Path)) != nil {
			return nil, ErrInput
		}
		refBytes, err := json.Marshal(entry.Source)
		if err != nil {
			return nil, ErrInput
		}
		ref, err := model.DecodeSourceRefJSON(refBytes, 16384)
		if err != nil || ref.WorktreeID != scope.Identity.WorktreeID || ref.ProfileID != scope.Identity.ProfileID || ref.GenerationID != scope.Identity.GenerationID || ref.Range.EndByte <= ref.Range.StartByte {
			return nil, ErrInput
		}
		entityBytes, err := json.Marshal(&entry.Entity)
		if err != nil {
			return nil, ErrInput
		}
		entity, err := model.DecodeGraphRefJSON(entityBytes, 16384)
		if err != nil {
			return nil, ErrInput
		}
		e, ok := entity.(*model.EntityRef)
		if !ok || e.GraphIdentity != scope.Identity {
			return nil, ErrInput
		}
		if err := in.Overview.authorize(ctx, []string{ref.Path}); err != nil {
			return nil, err
		}
		if err := in.AuthorizeAlias(ctx, scope, entry); err != nil {
			return nil, authorityError(ctx)
		}
		file, ok := scanned[ref.Path]
		if !ok {
			file, ok = in.Snapshot.File(ref.Path)
			if !ok || int64(file.Len()) > 64*1024*1024-scannedBytes || in.Scanner.CheckContent(file.Bytes()) != nil {
				return nil, ErrInput
			}
			scannedBytes += int64(file.Len())
			scanned[ref.Path] = file
		}
		if file.ContentHash() != ref.ContentHash || ref.Range.EndByte > int64(file.Len()) {
			return nil, ErrInput
		}
		body, err := file.Range(int(ref.Range.StartByte), int(ref.Range.EndByte))
		if err != nil || string(body) != entry.Alias {
			return nil, ErrInput
		}
	}
	var err error
	a.entries, a.digest, err = normalizeAliasSet(in.Declarations, in.MaxBytes)
	if err != nil {
		return nil, err
	}
	for _, entry := range a.entries {
		a.paths = append(a.paths, entry.Source.Path)
	}
	if err = a.authorize(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// Lookup returns every matching candidate up to its documented bound; when the
// complete result does not fit it fails explicitly rather than hiding matches.
func (a *AliasIndex) Lookup(ctx context.Context, query string) (AliasResult, error) {
	result, err := a.collect(ctx, query)
	if err != nil {
		return AliasResult{}, err
	}
	if len(result.Candidates) > 100 {
		return AliasResult{}, ErrLimit
	}
	return result, nil
}

// LookupAll returns a complete, bounded candidate set for a caller that owns
// pagination. It repeats config, actor and source authorization on every call.
func (a *AliasIndex) LookupAll(ctx context.Context, query string) (AliasResult, error) {
	return a.collect(ctx, query)
}

func (a *AliasIndex) collect(ctx context.Context, query string) (AliasResult, error) {
	if a == nil || len(query) > 4096 || !utf8.ValidString(query) || utf8.RuneCountInString(query) < 1 || utf8.RuneCountInString(query) > 512 || a.scanner.CheckContent([]byte(query)) != nil {
		return AliasResult{}, ErrInput
	}
	if err := a.authorize(ctx); err != nil {
		return AliasResult{}, err
	}
	groups := map[string]*AliasCandidate{}
	for _, entry := range a.entries {
		if err := ctx.Err(); err != nil {
			return AliasResult{}, err
		}
		if !strings.EqualFold(entry.Alias, query) {
			continue
		}
		candidate := groups[entry.Entity.EntityID]
		if candidate == nil {
			candidate = &AliasCandidate{Entity: entry.Entity, SourceRefs: []model.SourceRef{}}
			groups[entry.Entity.EntityID] = candidate
		}
		duplicate := false
		for _, ref := range candidate.SourceRefs {
			if ref == entry.Source {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidate.SourceRefs = append(candidate.SourceRefs, entry.Source)
		}
	}
	result := AliasResult{Candidates: []AliasCandidate{}, Ambiguous: len(groups) > 1}
	for _, candidate := range groups {
		if len(candidate.SourceRefs) > 16 {
			return AliasResult{}, ErrLimit
		}
		sort.Slice(candidate.SourceRefs, func(i, j int) bool {
			a, b := candidate.SourceRefs[i], candidate.SourceRefs[j]
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			if a.Range.StartByte != b.Range.StartByte {
				return a.Range.StartByte < b.Range.StartByte
			}
			return a.Range.EndByte < b.Range.EndByte
		})
		result.Candidates = append(result.Candidates, *candidate)
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		return result.Candidates[i].Entity.EntityID < result.Candidates[j].Entity.EntityID
	})
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > a.maxBytes {
		return AliasResult{}, ErrLimit
	}
	if err = a.authorize(ctx); err != nil {
		return AliasResult{}, err
	}
	return result, nil
}
