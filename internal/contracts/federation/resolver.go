// Package federation joins explicit static frontend and backend evidence
// within a verified, already authorized group snapshot.
package federation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/gohttp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrInput        = errors.New("invalid federated flow request")
	ErrScopeChanged = errors.New("federated flow scope changed")
)

const (
	scopeBytes  = 65536
	resultBytes = 1 << 20
)

// ProxyRule comes from an owner-verified static configuration in the
// frontend generation. The resolver checks its exact source before use.
type ProxyRule struct {
	FrontendService string
	BackendService  string
	PublicPrefix    string
	BackendPrefix   string
	Declaration     model.SourceRef
}

// Backend comes from authorized OpenAPI and guarded Go provider evidence.
// SQL effects are the bounded static projection of this precise route.
type Backend struct {
	Operation schema.Operation
	Route     flowview.Route
	SQL       []flowview.SQLSite
}

type Request struct {
	Pinned   model.ScopeVector
	Clients  []contracts.FrontendClient
	Backends []Backend
	Proxies  []ProxyRule
	Reverse  *model.RefSelector
	Limit    int
}

type Path struct {
	Binding contracts.FrontendBinding
	Request gohttp.Pattern
	Route   flowview.Route
	Proxy   *model.SourceRef
	SQL     []flowview.SQLSite
}

type Result struct {
	Scope   model.ScopeVector
	Paths   []Path
	Gaps    []model.Gap
	Partial bool
}

// Resolver is fed by the existing group catalog and protected member readers.
// CheckSource verifies current path authority, captured bytes and the supplied
// range. Observe rechecks the exact retained scope via the group resolver.
type Resolver struct {
	ReadGroup   func(context.Context, string) (model.GroupSummary, string, error)
	Observe     func(context.Context, model.ScopeVector) (model.ScopeVector, error)
	CheckSource func(context.Context, model.ScopeMember, model.SourceRef) error
}

type session struct {
	resolver Resolver
	ctx      context.Context
	group    model.GroupSummary
	pinned   model.ScopeVector
	configs  map[string]model.GroupMemberConfig
	members  map[string]model.ScopeMember
	input    Request
	out      Result
}

func (r Resolver) Resolve(ctx context.Context, req Request) (Result, error) {
	if ctx == nil || r.ReadGroup == nil || r.Observe == nil || r.CheckSource == nil ||
		req.Pinned.Kind != "federated" || model.VerifyScopeVectorDigest(req.Pinned, scopeBytes) != nil ||
		req.Limit < 1 || req.Limit > 100 || len(req.Clients) > 512 || len(req.Backends) > 512 || len(req.Proxies) > 64 {
		return Result{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	pinned, err := model.NormalizeScopeVector(req.Pinned, scopeBytes)
	if err != nil {
		return Result{}, err
	}
	s := session{resolver: r, ctx: ctx, pinned: pinned, input: req,
		configs: make(map[string]model.GroupMemberConfig), members: make(map[string]model.ScopeMember),
		out: Result{Scope: pinned, Paths: []Path{}, Gaps: []model.Gap{}}}
	if err := s.checkInitial(); err != nil {
		return Result{}, err
	}
	encodedScope, err := json.Marshal(pinned)
	if err != nil {
		return Result{}, err
	}
	// Leave space for the result envelope, separators, and deduplicated gaps.
	remaining := resultBytes - len(encodedScope) - 8192
	if req.Reverse != nil {
		if err := s.validateReverse(*req.Reverse); err != nil {
			return Result{}, err
		}
	}
	clients := slices.Clone(req.Clients)
	slices.SortStableFunc(clients, compareClients)
	for i, client := range clients {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if i != 0 && compareClients(clients[i-1], client) == 0 {
			return Result{}, ErrInput
		}
		path, gap, err := s.resolveClient(client)
		if err != nil {
			return Result{}, err
		}
		if gap != "" {
			s.gap(gap)
			continue
		}
		if req.Reverse != nil && !supportsReverse(path, *req.Reverse) {
			continue
		}
		if len(s.out.Paths) >= req.Limit {
			s.gap("BRIDGE_RESULT_LIMIT")
			break
		}
		raw, err := json.Marshal(path)
		if err != nil {
			return Result{}, err
		}
		if len(raw)+1 > remaining {
			s.gap("BRIDGE_RESULT_LIMIT")
			break
		}
		remaining -= len(raw) + 1
		s.out.Paths = append(s.out.Paths, path)
	}
	if err := s.recheck(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return s.out, nil
}

func (s *session) checkInitial() error {
	if err := s.observe(); err != nil {
		return err
	}
	group, catalog, err := s.resolver.ReadGroup(s.ctx, *s.pinned.GroupID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(group.Config)
	if err != nil {
		return err
	}
	cfg, err := model.DecodeGroupConfigJSON(raw, scopeBytes)
	if err != nil || group.Status != "active" || cfg.GroupID != *s.pinned.GroupID ||
		group.Revision != *s.pinned.GroupRevision || catalog != s.pinned.CatalogIncarnation ||
		len(cfg.Members) != len(s.pinned.Members)+len(s.pinned.UnavailableMembers) {
		return ErrScopeChanged
	}
	group.Config = cfg
	s.group = group
	for _, c := range cfg.Members {
		if _, exists := s.configs[c.ServiceID]; exists {
			return ErrScopeChanged
		}
		s.configs[c.ServiceID] = c
	}
	for _, member := range s.pinned.Members {
		if member.ServiceID == nil {
			return ErrScopeChanged
		}
		cfg, found := s.configs[*member.ServiceID]
		if !found || cfg.WorktreeID != member.WorktreeID || cfg.ProfileID != member.ProfileID {
			return ErrScopeChanged
		}
		s.members[cfg.ServiceID] = member
	}
	for _, gap := range s.pinned.UnavailableMembers {
		if gap.ServiceID == nil {
			continue
		}
		cfg, found := s.configs[*gap.ServiceID]
		_, available := s.members[*gap.ServiceID]
		if !found || available || value(gap.WorktreeID) != cfg.WorktreeID || value(gap.ProfileID) != cfg.ProfileID {
			return ErrScopeChanged
		}
	}
	if len(s.pinned.UnavailableMembers) != 0 {
		s.gap("BRIDGE_MEMBER_UNAVAILABLE")
	}
	return nil
}

func (s *session) observe() error {
	observed, err := s.resolver.Observe(s.ctx, s.pinned)
	if err != nil {
		return err
	}
	if err := model.ComparePinnedScope(s.pinned, observed, scopeBytes); err != nil {
		return ErrScopeChanged
	}
	return nil
}

func (s *session) recheck() error {
	if err := s.observe(); err != nil {
		return err
	}
	group, catalog, err := s.resolver.ReadGroup(s.ctx, *s.pinned.GroupID)
	if err != nil {
		return err
	}
	if catalog != s.pinned.CatalogIncarnation || !reflect.DeepEqual(group, s.group) {
		return ErrScopeChanged
	}
	return nil
}

func (s *session) gap(code string) {
	for _, prior := range s.out.Gaps {
		if prior.Code == code {
			return
		}
	}
	s.out.Partial = true
	s.out.Gaps = append(s.out.Gaps, model.Gap{Code: code,
		Reason: "An available verified match could not be established for the entire requested scope.", SourceRefs: []model.SourceRef{}})
}

func compareClients(l, r contracts.FrontendClient) int {
	for _, pair := range [][2]string{{value(l.ServiceID), value(r.ServiceID)}, {l.Site.Path, r.Site.Path},
		{l.Caller.SymbolID, r.Caller.SymbolID}} {
		if n := strings.Compare(pair[0], pair[1]); n != 0 {
			return n
		}
	}
	if l.Site.Range.StartByte < r.Site.Range.StartByte {
		return -1
	}
	if l.Site.Range.StartByte > r.Site.Range.StartByte {
		return 1
	}
	return 0
}

func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *session) validateReverse(selector model.RefSelector) error {
	ref, ok := selector.Ref.(*model.EntityRef)
	if !ok || ref == nil || selector.Kind != "ref" || selector.ServiceID == nil {
		return ErrInput
	}
	_, err := model.MatchPinnedMember(s.pinned, ref, selector.ServiceID)
	return err
}

func supportsReverse(path Path, selector model.RefSelector) bool {
	ref := selector.Ref.(*model.EntityRef)
	if value(selector.ServiceID) != value(path.Binding.Handler.Operation.Key.ServiceID) {
		return false
	}
	if path.Binding.Handler.Operation.Ref == *ref {
		return true
	}
	for _, step := range path.SQL {
		for _, target := range step.Targets {
			if target.Ref == *ref {
				return true
			}
		}
	}
	return false
}
