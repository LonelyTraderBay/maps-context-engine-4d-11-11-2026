package history

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type factPair struct {
	left, right int
}

type matchIndex struct {
	left, right map[int]int
	leftAmbig   map[int]bool
	rightAmbig  map[int]bool
}

func matchFacts(left, right []semanticFact) matchIndex {
	m := matchIndex{left: map[int]int{}, right: map[int]int{}, leftAmbig: map[int]bool{}, rightAmbig: map[int]bool{}}
	l, r := make(map[string][]int), make(map[string][]int)
	for i, fact := range left {
		l[fact.exact] = append(l[fact.exact], i)
	}
	for i, fact := range right {
		r[fact.exact] = append(r[fact.exact], i)
	}
	for key, a := range l {
		b := r[key]
		if len(a) == 1 && len(b) == 1 {
			m.left[a[0]], m.right[b[0]] = b[0], a[0]
		}
	}
	l, r = probableKeys(left, m.left), probableKeys(right, m.right)
	pairs := map[factPair]bool{}
	lcount, rcount := map[int]int{}, map[int]int{}
	for key, a := range l {
		b := r[key]
		switch {
		case len(b) == 0:
			continue
		case len(a) == 1 && len(b) == 1:
			pair := factPair{left: a[0], right: b[0]}
			if !pairs[pair] {
				pairs[pair] = true
				lcount[pair.left]++
				rcount[pair.right]++
			}
		default:
			for _, i := range a {
				m.leftAmbig[i] = true
			}
			for _, i := range b {
				m.rightAmbig[i] = true
			}
		}
	}
	for pair := range pairs {
		if lcount[pair.left] == 1 && rcount[pair.right] == 1 &&
			!m.leftAmbig[pair.left] && !m.rightAmbig[pair.right] {
			m.left[pair.left], m.right[pair.right] = pair.right, pair.left
		} else {
			m.leftAmbig[pair.left], m.rightAmbig[pair.right] = true, true
		}
	}
	return m
}

func probableKeys(facts []semanticFact, matched map[int]int) map[string][]int {
	out := make(map[string][]int)
	for i, fact := range facts {
		if _, ok := matched[i]; ok {
			continue
		}
		for _, key := range fact.keys {
			out[key] = append(out[key], i)
		}
	}
	return out
}

func (d *diffRun) compareFacts(ctx context.Context, category string, service *string, left, right []semanticFact, complete bool) ([]SemanticChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matched := matchFacts(left, right)
	changes := make([]SemanticChange, 0)
	appendChange := func(change string, match string, before, after *DiffItem) error {
		if err := d.takeChange(); err != nil {
			return err
		}
		changes = append(changes, SemanticChange{ServiceID: service, Category: category, Change: change,
			Match: match, Before: before, After: after})
		return nil
	}
	for i, current := range left {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if j, ok := matched.left[i]; ok {
			other := right[j]
			if current.exact == other.exact {
				d.result.ExactMatches++
				continue
			}
			confidence := "probable"
			if category == "contract" && current.keys[0] == other.keys[0] {
				confidence = "exact"
			}
			if err := appendChange("changed", confidence, &current.item, &other.item); err != nil {
				return nil, err
			}
			continue
		}
		change := d.unmatched("removed", matched.leftAmbig[i], complete)
		if err := appendChange(change, "unmatched", &current.item, nil); err != nil {
			return nil, err
		}
	}
	for i, current := range right {
		if _, ok := matched.right[i]; ok {
			continue
		}
		change := d.unmatched("added", matched.rightAmbig[i], complete)
		if err := appendChange(change, "unmatched", nil, &current.item); err != nil {
			return nil, err
		}
	}
	d.result.Changes = append(d.result.Changes, changes...)
	return changes, nil
}

func (d *diffRun) unmatched(change string, ambiguous, complete bool) string {
	if ambiguous {
		d.gap("DIFF_MATCH_AMBIGUOUS")
		return "ambiguous"
	}
	if !complete {
		return "unknown"
	}
	return change
}

func (d *diffRun) collectConsumers(ctx context.Context, side string, view DiffMember, facts map[string][]semanticFact, changed map[string]bool) error {
	known := map[string]DiffItem{}
	for _, category := range []string{"symbol", "contract", "relation"} {
		for _, fact := range facts[category] {
			known[semanticKey(category, fact.item.ID)] = fact.item
		}
	}
	seen := map[string]bool{}
	add := func(category, id string, source model.SourceRef) error {
		key := semanticKey(category, id, source)
		if seen[key] {
			return nil
		}
		item, ok := known[semanticKey(category, id)]
		if !ok {
			return nil
		}
		if err := d.takeChange(); err != nil {
			return err
		}
		seen[key] = true
		d.result.Consumers = append(d.result.Consumers, KnownConsumer{Side: side, ServiceID: view.Member.ServiceID, Ref: item.Ref, Source: source})
		return nil
	}
	for _, r := range view.Relations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.TargetSymbolID == nil || !changed[semanticKey("symbol", *r.TargetSymbolID)] ||
			(r.EvidenceKind != "syntax_exact" && r.EvidenceKind != "type_resolved" && r.EvidenceKind != "explicit_document") {
			continue
		}
		site, ok := known[semanticKey("relation", r.RelationID)]
		if ok {
			if err := add("symbol", r.SourceSymbolID, site.Source); err != nil {
				return err
			}
		}
	}
	for _, link := range view.Domain.Links {
		if err := ctx.Err(); err != nil {
			return err
		}
		if link.ResolutionStatus != "exact" {
			continue
		}
		toCategory, toID := consumerRef(link.Target, view.Member)
		if toID == "" || !changed[semanticKey(toCategory, toID)] {
			continue
		}
		fromCategory, fromID := consumerRef(link.Source, view.Member)
		if fromID == "" {
			continue
		}
		for _, ref := range link.Evidence.SourceRefs {
			if belongs(view.Member, ref) && hasSource(view.Capture, ref) {
				if err := add(fromCategory, fromID, ref); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func consumerRef(ref model.GraphRef, member model.ScopeMember) (string, string) {
	switch v := ref.(type) {
	case *model.SymbolRef:
		if v != nil && v.GraphIdentity == memberIdentity(member) {
			return "symbol", v.SymbolID
		}
	case *model.EntityRef:
		if v != nil && v.GraphIdentity == memberIdentity(member) {
			return "contract", v.EntityID
		}
	}
	return "", ""
}
