package contextpack

import (
	"context"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func source(member model.ScopeMember, file snapshot.File, r model.ByteRange) model.SourceRef {
	return model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: file.Path(), ContentHash: file.ContentHash(), Range: r}
}

// Containment and imports are kept as evidence but are not expanded into every
// sibling in a file/package. Semantically relevant edges are walked both ways.
func follow(kind string) bool {
	switch kind {
	case "REFERS_TO", "READS", "WRITES", "CALLS_DIRECT", "CALLS_INTERFACE", "MAY_DISPATCH_TO", "TEST_REF", "DOC_LINK":
		return true
	default:
		return false
	}
}

func walk(ctx context.Context, s *Selection, in *Input, c catalog, symbols map[string]model.SymbolFact, files map[string]snapshot.File, target model.SymbolFact) error {
	rels := append([]model.RelationFact(nil), c.Relations...)
	sort.Slice(rels, func(i, j int) bool {
		a, b := rels[i], rels[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.SitePath != b.SitePath {
			return a.SitePath < b.SitePath
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		return a.RelationID < b.RelationID
	})
	adj := map[string][]int{}
	for i, r := range rels {
		adj[r.SourceSymbolID] = append(adj[r.SourceSymbolID], i)
		if r.TargetSymbolID != nil && *r.TargetSymbolID != r.SourceSymbolID {
			adj[*r.TargetSymbolID] = append(adj[*r.TargetSymbolID], i)
		}
	}
	type node struct {
		id    string
		depth int
	}
	queue := []node{{target.SymbolID, 0}}
	visited := map[string]bool{target.SymbolID: true}
	seen := map[string]bool{}
	stop := false
	for head := 0; head < len(queue) && !stop; head++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := queue[head]
		for _, index := range adj[current.id] {
			if err := ctx.Err(); err != nil {
				return err
			}
			r := rels[index]
			if seen[r.RelationID] {
				continue
			}
			if len(seen) >= in.MaxEdges {
				s.report.EnumerationComplete = false
				addGap(&s.report, "GRAPH_EDGE_LIMIT", "Traversal stopped at its edge bound; remaining counts are unknown.")
				stop = true
				break
			}
			seen[r.RelationID] = true
			classification := "static"
			switch {
			case r.TargetSymbolID == nil || r.EvidenceKind == "unresolved":
				classification = "unresolved"
				addGap(&s.report, "UNRESOLVED_RELATION", "At least one relation has an unresolved target or binding.")
			case r.Kind == "MAY_DISPATCH_TO" || r.EvidenceKind == "type_possible":
				classification = "candidate"
				addGap(&s.report, "POSSIBLE_DISPATCH", "Candidate implementations are not observed runtime calls.")
			case r.Kind == "CALLS_INTERFACE":
				classification = "interface"
				addGap(&s.report, "INTERFACE_DISPATCH", "An interface call does not prove a concrete runtime target.")
			case r.EvidenceKind == "text_match":
				classification = "candidate"
				addGap(&s.report, "TEXT_MATCH_ONLY", "A text match does not prove a semantic relationship.")
			case symbols[*r.TargetSymbolID].Path == nil:
				classification = "external"
				addGap(&s.report, "EXTERNAL_BOUNDARY", "A relation reaches an external symbol without captured source.")
			}
			s.report.References = append(s.report.References, Reference{Relation: r, Source: source(s.member, files[r.SitePath], r.Range), Classification: classification})
			if (!follow(r.Kind) && !(in.Focused && followFocused(r.Kind))) || r.TargetSymbolID == nil {
				continue
			}
			other := r.SourceSymbolID
			if current.id == other {
				other = *r.TargetSymbolID
			}
			if visited[other] {
				continue
			}
			if current.depth >= in.MaxDepth {
				s.report.EnumerationComplete = false
				addGap(&s.report, "GRAPH_DEPTH_LIMIT", "Traversal stopped at its depth bound; remaining counts are unknown.")
				continue
			}
			if len(visited) >= in.MaxNodes {
				s.report.EnumerationComplete = false
				addGap(&s.report, "GRAPH_NODE_LIMIT", "Traversal stopped at its node bound; remaining counts are unknown.")
				continue
			}
			visited[other] = true
			queue = append(queue, node{other, current.depth + 1})
		}
	}
	addGap(&s.report, "STATIC_RUNTIME_BOUNDARY", "Static facts do not prove a complete runtime call graph or the absence of impact.")
	if c.Coverage.Status != "supported_scope_complete" {
		addGap(&s.report, "PROVIDER_COVERAGE_INCOMPLETE", "Provider coverage is partial, unsupported or unknown; missing relations are not proof of absence.")
	}
	if len(s.report.Scope.UnavailableMembers) > 0 {
		s.report.EnumerationComplete = false
		addGap(&s.report, "SCOPE_MEMBERS_UNAVAILABLE", "Some scope members are unavailable and were not analyzed.")
	}
	if len(s.report.Scope.Members) > 1 {
		s.report.EnumerationComplete = false
		addGap(&s.report, "OTHER_MEMBERS_NOT_ENUMERATED", "This selection enumerated one pinned member; other resolved members were not traversed.")
	}
	packages := map[string]model.PackageFact{}
	for _, p := range c.Packages {
		packages[p.PackageKey] = p
	}
	moduleOf := func(id string) string {
		sym := symbols[id]
		if sym.PackageKey == nil {
			return ""
		}
		return packages[*sym.PackageKey].ModuleKey
	}
	type groupKey struct{ source, target, kind, evidence, class string }
	groups := map[groupKey]int{}
	for _, ref := range s.report.References {
		r := ref.Relation
		from, to := moduleOf(r.SourceSymbolID), ""
		if r.TargetSymbolID != nil {
			to = moduleOf(*r.TargetSymbolID)
		}
		if from == "" || r.TargetSymbolID != nil && to == "" {
			addGap(&s.report, "MODULE_BOUNDARY_UNKNOWN", "At least one selected endpoint has no verified provider package module key.")
		}
		key := groupKey{from, to, r.Kind, r.EvidenceKind, ref.Classification}
		groups[key]++
	}
	for key, count := range groups {
		s.report.Groups = append(s.report.Groups, ReferenceGroup{SourceModuleKey: key.source, TargetModuleKey: key.target, RelationKind: key.kind, EvidenceKind: key.evidence, Classification: key.class, SelectedCount: count})
	}
	sort.Slice(s.report.Groups, func(i, j int) bool {
		a, b := s.report.Groups[i], s.report.Groups[j]
		if a.SourceModuleKey != b.SourceModuleKey {
			return a.SourceModuleKey < b.SourceModuleKey
		}
		if a.TargetModuleKey != b.TargetModuleKey {
			return a.TargetModuleKey < b.TargetModuleKey
		}
		if a.RelationKind != b.RelationKind {
			return a.RelationKind < b.RelationKind
		}
		if a.EvidenceKind != b.EvidenceKind {
			return a.EvidenceKind < b.EvidenceKind
		}
		return a.Classification < b.Classification
	})
	var ranges []SourceRange
	file := files[*target.Path]
	targetSpan, reason := model.ByteRange{EndByte: int64(file.Len())}, "target_file"
	if in.Focused {
		targetSpan, reason = *target.Range, "target_declaration"
	}
	ranges = append(ranges, SourceRange{Source: source(s.member, file, targetSpan), Priority: 0, Reasons: []string{reason}})
	for id := range visited {
		sym := symbols[id]
		s.report.Nodes = append(s.report.Nodes, sym)
		if sym.Path == nil || sym.Range == nil || id == target.SymbolID {
			continue
		}
		if in.Focused && sym.Range.EndByte-sym.Range.StartByte > 3072 {
			continue
		}
		ranges = append(ranges, SourceRange{Source: source(s.member, files[*sym.Path], *sym.Range), Priority: 3, Reasons: []string{"related_declaration"}})
	}
	sort.Slice(s.report.Nodes, func(i, j int) bool { return s.report.Nodes[i].SymbolID < s.report.Nodes[j].SymbolID })
	for _, r := range s.report.References {
		priority, reason := 1, "relation_site"
		if r.Relation.Kind == "TEST_REF" {
			priority, reason = 2, "test_reference"
		}
		ranges = append(ranges, SourceRange{Source: r.Source, Priority: priority, Reasons: []string{reason}})
	}
	if in.Focused {
		// Preserve the selected declaration even if an enclosing relation site
		// spans much more source.
		related := ranges[:1]
		for _, candidate := range ranges[1:] {
			span := candidate.Source.Range
			if span.EndByte-span.StartByte > 3072 ||
				candidate.Source.Path == *target.Path && targetSpan.StartByte <= span.StartByte && span.EndByte <= targetSpan.EndByte {
				continue
			}
			related = append(related, candidate)
		}
		s.report.Sources = append(related[:1:1], mergeRanges(related[1:])...)
	} else {
		s.report.Sources = mergeRanges(ranges)
	}
	witness, err := cycleWitness(ctx, s.report.References)
	if err != nil {
		return err
	}
	s.report.CycleWitness = witness
	return nil
}

func followFocused(kind string) bool {
	switch kind {
	case "ROUTE_COMPONENT", "RENDERS_COMPONENT", "RENDERS_JSX", "BINDS_EVENT", "CHECKS_CONDITION", "BRANCH_TRUE", "BRANCH_FALSE":
		return true
	default:
		return false
	}
}

func mergeRanges(ranges []SourceRange) []SourceRange {
	sort.Slice(ranges, func(i, j int) bool {
		a, b := ranges[i].Source, ranges[j].Source
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		if a.Range.EndByte != b.Range.EndByte {
			return a.Range.EndByte < b.Range.EndByte
		}
		return ranges[i].Priority < ranges[j].Priority
	})
	out := make([]SourceRange, 0, len(ranges))
	for _, x := range ranges {
		if len(out) > 0 {
			last := &out[len(out)-1]
			a, b := last.Source, x.Source
			if a.WorktreeID == b.WorktreeID && a.ProfileID == b.ProfileID && a.GenerationID == b.GenerationID && a.Path == b.Path && a.ContentHash == b.ContentHash && (b.Range.StartByte < a.Range.EndByte || b.Range == a.Range) {
				if b.Range.EndByte > last.Source.Range.EndByte {
					last.Source.Range.EndByte = b.Range.EndByte
				}
				if x.Priority < last.Priority {
					last.Priority = x.Priority
				}
				last.Reasons = append(last.Reasons, x.Reasons...)
				continue
			}
		}
		out = append(out, x)
	}
	for i := range out {
		sort.Strings(out[i].Reasons)
		dedup := out[i].Reasons[:0]
		for _, reason := range out[i].Reasons {
			if len(dedup) == 0 || dedup[len(dedup)-1] != reason {
				dedup = append(dedup, reason)
			}
		}
		out[i].Reasons = dedup
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Source.Path != b.Source.Path {
			return a.Source.Path < b.Source.Path
		}
		if a.Source.Range.StartByte != b.Source.Range.StartByte {
			return a.Source.Range.StartByte < b.Source.Range.StartByte
		}
		return a.Source.Range.EndByte < b.Source.Range.EndByte
	})
	return out
}

// Return one actual directed cycle, not just a repeated undirected neighbor.
func cycleWitness(ctx context.Context, refs []Reference) ([]string, error) {
	type edge struct{ to, id string }
	adj := map[string][]edge{}
	nodes := map[string]bool{}
	for _, ref := range refs {
		r := ref.Relation
		if r.TargetSymbolID == nil {
			continue
		}
		adj[r.SourceSymbolID] = append(adj[r.SourceSymbolID], edge{*r.TargetSymbolID, r.RelationID})
		nodes[r.SourceSymbolID] = true
		nodes[*r.TargetSymbolID] = true
	}
	for key := range adj {
		sort.Slice(adj[key], func(i, j int) bool {
			a, b := adj[key][i], adj[key][j]
			if a.to != b.to {
				return a.to < b.to
			}
			return a.id < b.id
		})
	}
	keys := make([]string, 0, len(nodes))
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	type frame struct {
		id   string
		next int
	}
	color := map[string]uint8{}
	parent := map[string]string{}
	parentEdge := map[string]string{}
	for _, root := range keys {
		if color[root] != 0 {
			continue
		}
		color[root] = 1
		stack := []frame{{id: root}}
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			top := &stack[len(stack)-1]
			edges := adj[top.id]
			if top.next == len(edges) {
				color[top.id] = 2
				stack = stack[:len(stack)-1]
				continue
			}
			e := edges[top.next]
			top.next++
			if color[e.to] == 0 {
				parent[e.to] = top.id
				parentEdge[e.to] = e.id
				color[e.to] = 1
				stack = append(stack, frame{id: e.to})
				continue
			}
			if color[e.to] == 1 {
				witness := []string{}
				for n := top.id; n != e.to; n = parent[n] {
					witness = append(witness, parentEdge[n])
				}
				for i, j := 0, len(witness)-1; i < j; i, j = i+1, j-1 {
					witness[i], witness[j] = witness[j], witness[i]
				}
				return append(witness, e.id), nil
			}
		}
	}
	return []string{}, nil
}
