package overview

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// Expansion is an internal exact-scope reference. It is not a public token;
// the API layer must protect and validate continuation tokens separately.
type Expansion struct {
	Scope       Scope  `json:"scope"`
	FactsDigest string `json:"facts_digest"`
	GroupID     string `json:"group_id"`
}

type Group struct {
	GroupID        string    `json:"group_id"`
	SourceModuleID string    `json:"source_module_id"`
	TargetModuleID string    `json:"target_module_id"`
	TargetBoundary string    `json:"target_boundary"` // local, external, unresolved
	Kind           string    `json:"kind"`
	EvidenceKind   string    `json:"evidence_kind"`
	Count          int       `json:"count"`
	Expand         Expansion `json:"expand_ref"`
}

type Site struct {
	Relation model.RelationFact `json:"relation"`
	Source   model.SourceRef    `json:"source"`
}

type Page struct {
	Sites     []Site `json:"sites"`
	NextAfter string `json:"next_after"`
}

func groupID(scope Scope, source, target, boundary, kind, evidence string, max int) (string, error) {
	values := []string{scope.Identity.WorktreeID, scope.Identity.ProfileID, scope.Identity.GenerationID, scope.SnapshotDigest, scope.PolicyDigest, source, target, boundary, kind, evidence}
	items := make([]model.CanonicalValue, 0, len(values))
	for _, value := range values {
		item, err := model.CanonicalString(value)
		if err != nil {
			return "", ErrInput
		}
		items = append(items, item)
	}
	_, id, err := model.EncodeCanonical("overview-edge-group-v1", model.CanonicalArray(items), max)
	if err != nil {
		return "", ErrLimit
	}
	return id, nil
}

func buildGroups(ctx context.Context, scope Scope, f facts, packages map[string]PackageInput, filePackages map[string]map[string]bool, open func(string) (snapshot.File, error), max int) ([]Group, map[string][]Site, error) {
	symbols := map[string]model.SymbolFact{}
	for _, s := range f.Symbols {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if _, duplicate := symbols[s.SymbolID]; duplicate {
			return nil, nil, ErrInput
		}
		if s.Path != nil {
			if s.PackageKey == nil || !filePackages[*s.Path][*s.PackageKey] || s.Range == nil {
				return nil, nil, ErrInput
			}
			file, err := open(*s.Path)
			if err != nil {
				return nil, nil, err
			}
			if s.Range.StartByte < 0 || s.Range.EndByte > int64(file.Len()) || file.CheckRange(int(s.Range.StartByte), int(s.Range.EndByte)) != nil {
				return nil, nil, ErrInput
			}
		} else if s.Kind != "external" {
			return nil, nil, ErrInput
		}
		symbols[s.SymbolID] = s
	}
	groups := map[string]Group{}
	sites := map[string][]Site{}
	seen := map[string]bool{}
	used := 0
	for _, rel := range f.Relations {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if seen[rel.RelationID] {
			return nil, nil, ErrInput
		}
		seen[rel.RelationID] = true
		from, ok := symbols[rel.SourceSymbolID]
		if !ok || from.Path == nil || from.PackageKey == nil || !filePackages[rel.SitePath][*from.PackageKey] {
			return nil, nil, ErrInput
		}
		source, ok := packages[*from.PackageKey]
		if !ok {
			return nil, nil, ErrInput
		}
		file, err := open(rel.SitePath)
		if err != nil {
			return nil, nil, err
		}
		if rel.Range.StartByte < 0 || rel.Range.EndByte > int64(file.Len()) || file.CheckRange(int(rel.Range.StartByte), int(rel.Range.EndByte)) != nil {
			return nil, nil, ErrInput
		}
		target, boundary := "", "unresolved"
		if rel.TargetSymbolID != nil {
			to, exists := symbols[*rel.TargetSymbolID]
			if !exists {
				return nil, nil, ErrInput
			}
			boundary = "external"
			if to.Path != nil && to.PackageKey != nil {
				p, exists := packages[*to.PackageKey]
				if !exists {
					return nil, nil, ErrInput
				}
				target, boundary = p.Fact.ModuleKey, "local"
			}
		}
		id, err := groupID(scope, source.Fact.ModuleKey, target, boundary, rel.Kind, rel.EvidenceKind, max)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := groups[id]; !exists {
			groups[id] = Group{GroupID: id, SourceModuleID: source.Fact.ModuleKey, TargetModuleID: target, TargetBoundary: boundary, Kind: rel.Kind, EvidenceKind: rel.EvidenceKind}
		}
		site := Site{Relation: rel, Source: model.SourceRef{WorktreeID: scope.Identity.WorktreeID, ProfileID: scope.Identity.ProfileID, GenerationID: scope.Identity.GenerationID, Path: rel.SitePath, ContentHash: file.ContentHash(), Range: rel.Range}}
		raw, err := json.Marshal(site)
		if err != nil || len(raw)+1 > max-used {
			return nil, nil, ErrLimit
		}
		used += len(raw) + 1
		sites[id] = append(sites[id], site)
	}
	result := make([]Group, 0, len(groups))
	for id, g := range groups {
		g.Count = len(sites[id])
		result = append(result, g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GroupID < result[j].GroupID })
	return result, sites, nil
}

// Expand returns whole original relations and source sites in stable ID order.
// The caller must use the returned continuation until empty and must not treat
// this internal reference as an authenticated or portable cursor.
func (r *Report) Expand(ctx context.Context, ref Expansion, after string, maxItems int) (Page, error) {
	if r == nil || ref.Scope != r.scope || ref.FactsDigest != r.factsDigest || maxItems < 1 || maxItems > 10000 {
		return Page{}, ErrInput
	}
	entries, ok := r.groups[ref.GroupID]
	if !ok {
		return Page{}, ErrInput
	}
	if err := r.authorize(ctx, r.paths); err != nil {
		return Page{}, err
	}
	start := 0
	if after != "" {
		i := sort.Search(len(entries), func(i int) bool { return entries[i].Relation.RelationID >= after })
		if i == len(entries) || entries[i].Relation.RelationID != after {
			return Page{}, ErrInput
		}
		start = i + 1
	}
	page := Page{Sites: []Site{}}
	// Account for the exact JSON envelope, separators and continuation before
	// accepting each item, so a page never loses progress at the byte limit.
	used := len(`{"sites":[],"next_after":}`)
	for i := start; i < len(entries) && len(page.Sites) < maxItems; i++ {
		if err := ctx.Err(); err != nil {
			return Page{}, err
		}
		raw, err := json.Marshal(entries[i])
		if err != nil {
			return Page{}, ErrInput
		}
		separator := 0
		if len(page.Sites) > 0 {
			separator = 1
		}
		next := ""
		if i+1 < len(entries) {
			next = entries[i].Relation.RelationID
		}
		cursorJSON, err := json.Marshal(next)
		if err != nil || used+separator+len(raw)+len(cursorJSON) > r.maxOutputBytes {
			if len(page.Sites) == 0 {
				return Page{}, ErrLimit
			}
			break
		}
		used += separator + len(raw)
		var site Site
		if json.Unmarshal(raw, &site) != nil {
			return Page{}, ErrInput
		}
		page.Sites = append(page.Sites, site)
	}
	if start+len(page.Sites) < len(entries) {
		if len(page.Sites) == 0 {
			return Page{}, ErrLimit
		}
		page.NextAfter = page.Sites[len(page.Sites)-1].Relation.RelationID
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > r.maxOutputBytes {
		return Page{}, ErrLimit
	}
	if err := r.authorize(ctx, r.paths); err != nil {
		return Page{}, err
	}
	return page, nil
}
