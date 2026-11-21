package federation

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/gohttp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func (s *session) resolveClient(client contracts.FrontendClient) (Path, string, error) {
	if client.ServiceID == nil || client.Target.ServiceID == nil {
		return Path{}, "", ErrInput
	}
	frontID, backID := value(client.ServiceID), value(client.Target.ServiceID)
	_, frontAvailable := s.members[frontID]
	_, backAvailable := s.members[backID]
	if !frontAvailable || !backAvailable {
		return Path{}, "BRIDGE_MEMBER_UNAVAILABLE", nil
	}
	f, b := s.configs[frontID].Namespace, s.configs[backID].Namespace
	frontOrigin, frontOK := exactOrigin(value(f.BaseOrigin))
	backOrigin, backOK := exactOrigin(value(b.BaseOrigin))
	pattern, valid := gohttp.ParsePattern(client.Target.Identifier)
	backPrefix := value(b.RoutePrefix)
	if !frontOK || !backOK || !valid || strings.Contains(pattern.Path, "{") || !validPrefix(backPrefix) ||
		f.Protocol != "http" || b.Protocol != "http" || client.Target.Protocol != "http" ||
		f.Environment != b.Environment || b.Environment != client.Target.Environment || b.ContractVersion == nil ||
		value(b.ContractVersion) != client.Target.Version {
		return Path{}, "BRIDGE_NAMESPACE_UNRESOLVED", nil
	}
	path, proxy, gap, err := s.routePath(frontID, backID, pattern.Path, frontOrigin, backOrigin, backPrefix)
	if gap != "" || err != nil {
		return Path{}, gap, err
	}
	backend := make([]Backend, 0)
	handlers := make([]contracts.FrontendHandler, 0)
	for _, record := range s.input.Backends {
		if err := s.ctx.Err(); err != nil {
			return Path{}, "", err
		}
		op := record.Operation
		if !op.Bindable || op.Entity.Kind != "http_operation" || op.Entity.Plane != "intended" ||
			value(op.Entity.Key.ServiceID) != backID || op.Entity.Key.Environment != b.Environment ||
			op.Entity.Key.Namespace != client.Target.Namespace || op.Entity.Key.Version != value(b.ContractVersion) ||
			op.Entity.Key.Protocol != "http" || op.Method != pattern.Method ||
			op.Entity.Key.Identifier != op.Method+" "+op.Server+" "+op.Path ||
			!serverMatches(op.Server, backOrigin, backPrefix) ||
			joinPrefix(backPrefix, op.Path) != path || record.Route.Pattern.Method != op.Method ||
			record.Route.Pattern.Path != path || record.Route.Handler.GraphIdentity != op.Entity.Ref.GraphIdentity ||
			record.Route.RegistrationOwner.GraphIdentity != op.Entity.Ref.GraphIdentity {
			continue
		}
		if _, err := model.MatchPinnedMember(s.pinned, &op.Entity.Ref, client.Target.ServiceID); err != nil {
			// A stale projection is a boundary; never search its source.
			continue
		}
		backend = append(backend, record)
		handlers = append(handlers, contracts.FrontendHandler{Operation: op.Entity, Handler: record.Route.Handler, Declaration: record.Route.Site})
	}
	switch len(backend) {
	case 0:
		return Path{}, "BRIDGE_OPERATION_UNRESOLVED", nil
	case 1:
	default:
		return Path{}, "BRIDGE_OPERATION_AMBIGUOUS", nil
	}
	match := backend[0]
	client.Target.Identifier = match.Operation.Entity.Key.Identifier
	binding, err := contracts.MatchFrontendHandler(s.ctx, s.pinned, client, handlers, s.resolver.CheckSource,
		func(context.Context, model.ScopeVector) (model.ScopeVector, error) { return s.pinned, nil })
	if errors.Is(err, contracts.ErrFrontendBindingUnavailable) || errors.Is(err, model.ErrMemberNotInScope) {
		return Path{}, "BRIDGE_OPERATION_UNRESOLVED", nil
	}
	if err != nil {
		return Path{}, "", err
	}
	if err := s.check(&match.Route.RegistrationOwner, match.Route.Site, client.Target.ServiceID); err != nil {
		return Path{}, "", err
	}
	effects, err := s.sql(match, client.Target.ServiceID)
	if err != nil {
		return Path{}, "", err
	}
	return Path{Binding: binding, Request: pattern, Route: match.Route, Proxy: proxy, SQL: effects}, "", nil
}

func (s *session) check(ref model.GraphRef, source model.SourceRef, service *string) error {
	member, err := model.MatchPinnedMember(s.pinned, ref, service)
	if err != nil {
		return err
	}
	sourceMember, err := model.MatchPinnedSource(s.pinned, source, service, scopeBytes)
	if err != nil {
		return err
	}
	if member.WorktreeID != sourceMember.WorktreeID || member.ProfileID != sourceMember.ProfileID ||
		member.GenerationID != sourceMember.GenerationID || source.Range.StartByte >= source.Range.EndByte {
		return ErrInput
	}
	return s.resolver.CheckSource(s.ctx, member, source)
}

func (s *session) sql(b Backend, service *string) ([]flowview.SQLSite, error) {
	if len(b.SQL) > 100 {
		return nil, ErrInput
	}
	sites := make([]flowview.SQLSite, 0, len(b.SQL))
	for _, site := range b.SQL {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		if site.OutcomeStatus != "outcome_unknown" || len(site.Targets) > 32 {
			return nil, ErrInput
		}
		if err := s.check(&b.Route.Handler, site.Source, service); err != nil {
			return nil, err
		}
		for _, target := range site.Targets {
			if target.Key.Protocol != "mysql" || (target.Key.ServiceID != nil && value(target.Key.ServiceID) != value(service)) ||
				target.Kind != "table" && target.Kind != "view" && target.Kind != "procedure" {
				return nil, ErrInput
			}
			if err := s.check(&target.Ref, target.Declaration, service); err != nil {
				return nil, err
			}
		}
		site.Targets = slices.Clone(site.Targets)
		sites = append(sites, site)
	}
	return sites, nil
}

func exactOrigin(raw string) (string, bool) {
	if strings.ContainsAny(raw, "\\%?#") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		u.Path != "" || u.Opaque != "" || raw != u.Scheme+"://"+u.Host {
		return "", false
	}
	return raw, true
}

func validPrefix(v string) bool {
	if v == "/" {
		return true
	}
	if strings.Contains(v, "{") {
		return false
	}
	_, ok := gohttp.ParsePattern("GET " + v)
	return ok
}

func containsPath(prefix, requested string) bool {
	return prefix == "/" || requested == prefix || strings.HasPrefix(requested, prefix+"/")
}

func joinPrefix(prefix, tail string) string {
	if prefix == "/" {
		return tail
	}
	if tail == "/" {
		return prefix
	}
	return prefix + tail
}

func serverMatches(server, origin, prefix string) bool {
	if prefix == "/" {
		return server == "/" || server == origin || server == origin+"/"
	}
	return server == prefix || server == origin+prefix
}

func (s *session) routePath(front, back, requestPath, frontOrigin, backOrigin, backPrefix string) (string, *model.SourceRef, string, error) {
	var selected *ProxyRule
	for _, proxy := range s.input.Proxies {
		if proxy.FrontendService != front || proxy.BackendService != back {
			continue
		}
		if !validPrefix(proxy.PublicPrefix) || proxy.BackendPrefix != backPrefix {
			return "", nil, "BRIDGE_PROXY_UNRESOLVED", nil
		}
		if !containsPath(proxy.PublicPrefix, requestPath) {
			continue
		}
		if selected != nil {
			return "", nil, "BRIDGE_PROXY_AMBIGUOUS", nil
		}
		selected = &proxy
	}
	if selected == nil {
		if frontOrigin != backOrigin || !containsPath(backPrefix, requestPath) {
			return "", nil, "BRIDGE_PROXY_UNRESOLVED", nil
		}
		return requestPath, nil, "", nil
	}
	member := s.members[front]
	if bound, err := model.MatchPinnedSource(s.pinned, selected.Declaration, &front, scopeBytes); err != nil ||
		bound.GenerationID != member.GenerationID || selected.Declaration.Range.StartByte >= selected.Declaration.Range.EndByte {
		return "", nil, "BRIDGE_PROXY_UNRESOLVED", nil
	}
	if err := s.resolver.CheckSource(s.ctx, member, selected.Declaration); err != nil {
		return "", nil, "", err
	}
	if selected.PublicPrefix == "/" {
		return joinPrefix(backPrefix, requestPath), &selected.Declaration, "", nil
	}
	suffix := strings.TrimPrefix(requestPath, selected.PublicPrefix)
	if suffix == "" {
		suffix = "/"
	}
	return joinPrefix(backPrefix, suffix), &selected.Declaration, "", nil
}
