package indexer

import (
	"context"
	"sort"
)

// partitionGraph unions both generations, so removed dependencies cannot
// hide their former consumers. Unknown endpoints remain explicit boundaries.
func partitionGraph(ctx context.Context, before, after []Partition, maxPartitions, maxEdges int) (map[string][]string, map[string][]string, []string, bool, error) {
	set := map[string]bool{}
	for _, parts := range [][]Partition{before, after} {
		for _, p := range parts {
			set[p.ID] = true
		}
	}
	if len(set) > maxPartitions {
		return nil, nil, nil, false, ErrPartitionLimit
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	unique := map[string]map[string]bool{}
	count := 0
	unknown := false
	for _, parts := range [][]Partition{before, after} {
		present := make(map[string]bool, len(parts))
		for _, part := range parts {
			present[part.ID] = true
		}
		for _, p := range parts {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, false, err
			}
			if unique[p.ID] == nil {
				unique[p.ID] = map[string]bool{}
			}
			for _, dep := range p.Imports {
				if !present[dep] {
					unknown = true
				}
				if !set[dep] {
					continue
				}
				if !unique[p.ID][dep] {
					if count >= maxEdges {
						return nil, nil, nil, false, ErrPartitionLimit
					}
					unique[p.ID][dep] = true
					count++
				}
			}
		}
	}
	graph := map[string][]string{}
	reverse := map[string][]string{}
	for _, id := range ids {
		for dep := range unique[id] {
			graph[id] = append(graph[id], dep)
			reverse[dep] = append(reverse[dep], id)
		}
		sort.Strings(graph[id])
	}
	for _, id := range ids {
		sort.Strings(reverse[id])
	}
	return graph, reverse, ids, unknown, nil
}

// Iterative Kosaraju avoids recursion proportional to a repository's depth.
func partitionComponents(ctx context.Context, graph, reverse map[string][]string, ids []string) ([][]string, error) {
	type frame struct {
		id   string
		next int
	}
	visited := map[string]bool{}
	order := make([]string, 0, len(ids))
	for _, root := range ids {
		if visited[root] {
			continue
		}
		visited[root] = true
		stack := []frame{{id: root}}
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			top := &stack[len(stack)-1]
			neighbors := graph[top.id]
			if top.next < len(neighbors) {
				next := neighbors[top.next]
				top.next++
				if !visited[next] {
					visited[next] = true
					stack = append(stack, frame{id: next})
				}
				continue
			}
			order = append(order, top.id)
			stack = stack[:len(stack)-1]
		}
	}
	visited = map[string]bool{}
	components := [][]string{}
	for i := len(order) - 1; i >= 0; i-- {
		root := order[i]
		if visited[root] {
			continue
		}
		visited[root] = true
		stack := []string{root}
		part := []string{}
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			last := len(stack) - 1
			id := stack[last]
			stack = stack[:last]
			part = append(part, id)
			for _, next := range reverse[id] {
				if !visited[next] {
					visited[next] = true
					stack = append(stack, next)
				}
			}
		}
		sort.Strings(part)
		components = append(components, part)
	}
	sort.Slice(components, func(i, j int) bool { return components[i][0] < components[j][0] })
	return components, nil
}
