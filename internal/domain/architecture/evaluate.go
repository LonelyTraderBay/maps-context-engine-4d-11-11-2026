package architecture

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const maxDepth = 16

func memberKey(service *string, worktree string) string {
	if service != nil {
		return *service + "\x00" + worktree
	}
	return "\x00" + worktree
}

func moduleKey(module Module) string {
	ref := module.Ref
	return strings.Join([]string{memberKey(module.ServiceID, ref.WorktreeID), ref.ProfileID, ref.GenerationID, ref.ModuleID}, "\x00")
}

func (r Reader) evaluate(ctx context.Context, out *Report, plan definition, seen Observation, now time.Time) error {
	if len(seen.Modules) > maxEdges || len(seen.Dependencies) > maxEdges || len(seen.Coverage) > len(out.Scope.Members) {
		return ErrLimit
	}
	if len(out.Scope.UnavailableMembers) != 0 {
		out.gap("ARCHITECTURE_MEMBER_UNAVAILABLE", "Some registered members are unavailable.")
	}
	members := map[string]model.ScopeMember{}
	for _, m := range out.Scope.Members {
		members[memberKey(m.ServiceID, m.WorktreeID)] = m
	}
	coverage := map[string]string{}
	for _, item := range seen.Coverage {
		key := memberKey(item.Member.ServiceID, item.Member.WorktreeID)
		if expected, ok := members[key]; !ok || !reflect.DeepEqual(expected, item.Member) || coverage[key] != "" {
			return ErrScope
		}
		switch item.Status {
		case "supported_scope_complete", "partial", "unsupported":
			coverage[key] = item.Status
		default:
			return ErrInput
		}
	}
	modules := map[string]Module{}
	for _, item := range seen.Modules {
		if _, err := model.MatchPinnedMember(out.Scope, &item.Ref, item.ServiceID); err != nil {
			return ErrScope
		}
		if state := coverage[memberKey(item.ServiceID, item.Ref.WorktreeID)]; state == "" || state == "unsupported" {
			out.gap("ARCHITECTURE_PROVIDER_UNAVAILABLE", "A component has no qualified static provider evidence in this scope.")
			continue
		}
		key := moduleKey(item)
		if _, dup := modules[key]; dup {
			return ErrInput
		}
		modules[key] = item
	}
	byComponent, byModule, err := out.matchComponents(plan.Components, members, coverage, modules)
	if err != nil {
		return err
	}
	edges, err := r.checkedEdges(ctx, out, seen.Dependencies, modules)
	if err != nil {
		return err
	}
	exemptions := map[[2]string]Exemption{}
	for _, exemption := range plan.Exemptions {
		exemptions[[2]string{exemption.RuleID, exemption.Target}] = exemption
	}
	for _, rule := range plan.Rules {
		if err := ctx.Err(); err != nil {
			return err
		}
		root := byComponent[rule.From]
		if root == "" {
			out.gap("ARCHITECTURE_RULE_SOURCE_UNVERIFIED", "A rule has no observed source component in the pinned scope.")
			continue
		}
		if err := out.checkRule(ctx, rule, root, edges, byModule, exemptions, now); err != nil {
			return err
		}
	}
	return nil
}

func (out *Report) matchComponents(components []Component, members map[string]model.ScopeMember,
	coverage map[string]string, modules map[string]Module) (map[string]string, map[string]string, error) {
	byComponent, byModule := map[string]string{}, map[string]string{}
	for _, declared := range components {
		wanted := memberKey(declared.ServiceID, declared.WorktreeID)
		member, ok := members[wanted]
		if !ok {
			out.gap("ARCHITECTURE_MEMBER_UNAVAILABLE", "Some registered members are unavailable.")
			continue
		}
		status := ComponentStatus{Component: declared, Observed: "unsupported"}
		if coverage[wanted] == "" || coverage[wanted] == "unsupported" ||
			(declared.ProfileID != nil && *declared.ProfileID != member.ProfileID) {
			out.gap("ARCHITECTURE_PROVIDER_UNAVAILABLE", "A component has no qualified static provider evidence in this scope.")
			out.Components = append(out.Components, status)
			continue
		}
		if coverage[wanted] != "supported_scope_complete" {
			out.gap("ARCHITECTURE_PROVIDER_PARTIAL", "Static provider coverage is incomplete for at least one component.")
		}
		expected := Module{ServiceID: member.ServiceID, Ref: model.ModuleRef{Kind: "module", ModuleID: declared.ModuleID,
			GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}}}
		key := moduleKey(expected)
		if actual, found := modules[key]; found {
			if byModule[key] != "" {
				return nil, nil, ErrInput
			}
			status.Observed, status.Module = "observed_static", &actual
			byComponent[declared.ID], byModule[key] = key, declared.ID
		} else {
			status.Observed = "not_seen"
			if declared.Intended == "implemented" {
				out.gap("ARCHITECTURE_COMPONENT_NOT_SEEN", "An intended implemented component is absent from the supplied static evidence.")
			}
		}
		out.Components = append(out.Components, status)
	}
	return byComponent, byModule, nil
}

func (r Reader) checkedEdges(ctx context.Context, out *Report, dependencies []Dependency, modules map[string]Module) (map[string][]Dependency, error) {
	edges := map[string][]Dependency{}
	for _, item := range dependencies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.Relation != "imports" && item.Relation != "depends_on" {
			return nil, ErrInput
		}
		from, to := moduleKey(item.From), moduleKey(item.To)
		_, haveFrom := modules[from]
		_, haveTo := modules[to]
		if !haveFrom || !haveTo {
			out.gap("ARCHITECTURE_DEPENDENCY_UNVERIFIED", "Some dependency occurrences lack exact static evidence.")
			continue
		}
		if item.Evidence != "type_resolved" && item.Evidence != "syntax_exact" && item.Evidence != "explicit_document" {
			out.gap("ARCHITECTURE_DEPENDENCY_UNVERIFIED", "Some dependency occurrences lack exact static evidence.")
			continue
		}
		member, err := model.MatchPinnedSource(out.Scope, item.Site, item.From.ServiceID, scopeBytes)
		if err != nil || member.WorktreeID != item.From.Ref.WorktreeID || member.ProfileID != item.From.Ref.ProfileID ||
			member.GenerationID != item.From.Ref.GenerationID {
			return nil, ErrScope
		}
		if err := r.CheckSource(ctx, member, item.Site); err != nil {
			return nil, err
		}
		edges[from] = append(edges[from], item)
	}
	for _, list := range edges {
		slices.SortFunc(list, func(a, b Dependency) int {
			if cmp := strings.Compare(moduleKey(a.To), moduleKey(b.To)); cmp != 0 {
				return cmp
			}
			if cmp := strings.Compare(a.Site.Path, b.Site.Path); cmp != 0 {
				return cmp
			}
			if a.Site.Range.StartByte < b.Site.Range.StartByte {
				return -1
			}
			if a.Site.Range.StartByte > b.Site.Range.StartByte {
				return 1
			}
			return strings.Compare(a.Relation, b.Relation)
		})
	}
	return edges, nil
}

func (out *Report) checkRule(ctx context.Context, rule Rule, root string, edges map[string][]Dependency,
	components map[string]string, exemptions map[[2]string]Exemption, now time.Time) error {
	type hop struct {
		key   string
		depth int
	}
	queue := []hop{{key: root}}
	seen := map[string]bool{root: true}
	parent := map[string]Dependency{}
	for i := 0; i < len(queue); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		at := queue[i]
		for _, edge := range edges[at.key] {
			target := moduleKey(edge.To)
			if edge.Relation != rule.Relation || seen[target] {
				continue
			}
			if at.depth == maxDepth {
				out.gap("ARCHITECTURE_WITNESS_LIMIT", "Some dependency paths exceed the declared analysis depth.")
				continue
			}
			seen[target], parent[target] = true, edge
			if rule.Transitive {
				queue = append(queue, hop{key: target, depth: at.depth + 1})
			}
			id := components[target]
			if id == "" {
				out.gap("ARCHITECTURE_TARGET_UNMAPPED", "An observed dependency has no approved component identity in the rule scope.")
				continue
			}
			if slices.Contains(rule.Allow, id) {
				continue
			}
			item := Finding{RuleID: rule.ID, Target: id, Severity: rule.Severity, Rationale: rule.Rationale,
				Status: "violation", Declaration: out.Source, Witness: witness(root, target, parent)}
			if exemption, ok := exemptions[[2]string{rule.ID, id}]; ok {
				expiry, _ := time.Parse(time.RFC3339Nano, exemption.ExpiresAt)
				item.Exemption = &exemption
				if now.Before(expiry) {
					item.Status = "exempted"
				}
			}
			out.Findings = append(out.Findings, item)
			if len(out.Findings) > 512 {
				return ErrLimit
			}
		}
	}
	return nil
}

func witness(root, at string, parent map[string]Dependency) []Dependency {
	path := []Dependency{}
	for at != root {
		edge := parent[at]
		path = append(path, edge)
		at = moduleKey(edge.From)
	}
	slices.Reverse(path)
	return path
}
