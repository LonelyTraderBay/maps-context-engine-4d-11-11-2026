package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type Change struct {
	NodeID string
	Action string
	Fields []string
	Before *Node
	After  *Node
}

type LinkChange struct {
	Action string
	Link   Link
}

type Affected struct {
	Side   string
	NodeID string
	Source model.SourceRef
}

type Comparison struct {
	Before, After Report
	Changes       []Change
	LinkChanges   []LinkChange
	Affected      []Affected
	Partial       bool
}

// Compare finds changes in declared metadata and its explicit downstream
// dependencies. Affected entries are review obligations, not runtime outcomes.
func (r Reader) Compare(ctx context.Context, before, after Selection) (Comparison, error) {
	if before.Source.WorktreeID != after.Source.WorktreeID ||
		!reflect.DeepEqual(before.ServiceID, after.ServiceID) || before.Scope.Kind != after.Scope.Kind ||
		!reflect.DeepEqual(before.Scope.GroupID, after.Scope.GroupID) {
		return Comparison{}, ErrScope
	}
	left, err := r.Resolve(ctx, before)
	if err != nil {
		return Comparison{}, err
	}
	right, err := r.Resolve(ctx, after)
	if err != nil {
		return Comparison{}, err
	}
	out := Comparison{Before: left, After: right, Changes: []Change{}, LinkChanges: []LinkChange{}, Affected: []Affected{},
		Partial: left.Partial || right.Partial}
	l, rr := byID(left.Nodes), byID(right.Nodes)
	all := map[string]bool{}
	for id := range l {
		all[id] = true
	}
	for id := range rr {
		all[id] = true
	}
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	lroots, rroots := map[string]bool{}, map[string]bool{}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return Comparison{}, err
		}
		a, old := l[id]
		b, current := rr[id]
		item := Change{NodeID: id, Fields: []string{}}
		switch {
		case !old:
			item.Action, item.After = "added", &b
			rroots[id] = true
		case !current:
			item.Action, item.Before = "removed", &a
			lroots[id] = true
		default:
			item.Fields = metadataChanges(a, b)
			if len(item.Fields) == 0 {
				continue
			}
			item.Action, item.Before, item.After = "changed", &a, &b
			lroots[id], rroots[id] = true, true
		}
		out.Changes = append(out.Changes, item)
	}
	lLinks, rLinks := make(map[Link]bool, len(left.Links)), make(map[Link]bool, len(right.Links))
	for _, link := range left.Links {
		lLinks[link] = true
	}
	for _, link := range right.Links {
		rLinks[link] = true
	}
	for _, link := range left.Links {
		if !rLinks[link] {
			out.LinkChanges = append(out.LinkChanges, LinkChange{Action: "removed", Link: link})
			lroots[link.From] = true
		}
	}
	for _, link := range right.Links {
		if !lLinks[link] {
			out.LinkChanges = append(out.LinkChanges, LinkChange{Action: "added", Link: link})
			rroots[link.From] = true
		}
	}
	out.Affected = append(out.Affected, downstream("before", left, lroots)...)
	out.Affected = append(out.Affected, downstream("after", right, rroots)...)
	if err := r.recheck(ctx, left.Scope); err != nil {
		return Comparison{}, err
	}
	if err := r.recheck(ctx, right.Scope); err != nil {
		return Comparison{}, err
	}
	return out, nil
}

func byID(nodes []Node) map[string]Node {
	values := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		values[node.ID] = node
	}
	return values
}

func metadataChanges(a, b Node) []string {
	before, _ := json.Marshal(a.Metadata)
	after, _ := json.Marshal(b.Metadata)
	var x, y map[string]json.RawMessage
	_ = json.Unmarshal(before, &x)
	_ = json.Unmarshal(after, &y)
	diff := []string{}
	if a.Kind != b.Kind {
		diff = append(diff, "kind")
	}
	for key, old := range x {
		if !bytes.Equal(old, y[key]) {
			diff = append(diff, key)
		}
	}
	for key := range y {
		if _, ok := x[key]; !ok {
			diff = append(diff, key)
		}
	}
	slices.Sort(diff)
	return diff
}

func downstream(side string, report Report, changed map[string]bool) []Affected {
	edges := map[string][]string{}
	for _, link := range report.Links {
		edges[link.From] = append(edges[link.From], link.To)
	}
	queue := make([]string, 0, len(report.Nodes))
	for _, node := range report.Nodes {
		if changed[node.ID] {
			queue = append(queue, node.ID)
		}
	}
	visited := map[string]bool{}
	for i := 0; i < len(queue); i++ {
		for _, next := range edges[queue[i]] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	out := make([]Affected, 0, len(visited))
	for _, node := range report.Nodes {
		if visited[node.ID] {
			out = append(out, Affected{Side: side, NodeID: node.ID, Source: report.Source})
		}
	}
	return out
}
