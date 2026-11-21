package federation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrFlowEndpointUnavailable = errors.New("federated endpoint is unavailable")

var bridgeIdentifier = regexp.MustCompile(`^[A-Za-z0-9._-]{1,192}$`)

type FlowRequest struct {
	Pinned    model.ScopeVector
	Root      model.RefSelector
	Direction string
	MaxDepth  int
	Limit     int
}

type ContractEndpoint struct {
	Selector model.RefSelector `json:"selector"`
	Contract contracts.Key     `json:"contract"`
}

// DeclaredLink is owner-produced from an authorized explicit bridge
// declaration. It is never decoded from caller-provided flow query inputs.
type DeclaredLink struct {
	ID          string
	From        ContractEndpoint
	To          ContractEndpoint
	Declaration model.SourceRef
}

type FlowEndpointEvidence struct {
	Ref         model.GraphRef
	Contract    contracts.Key
	Declaration model.SourceRef
}

type FlowLink struct {
	DeclaredLink
	FromSource model.SourceRef
	ToSource   model.SourceRef
}

type FlowResult struct {
	Scope      model.ScopeVector
	Root       *model.RefSelector
	RootSource *model.SourceRef
	Links      []FlowLink
	Gaps       []model.Gap
	Partial    bool
}

// FlowResolver reads approved bridge metadata and verified compiler/index
// endpoints. The authority callbacks use the existing group resolver and
// recheck exact members and policy before exposing result source references.
type FlowResolver struct {
	Resolver
	ReadLinks    func(context.Context, model.ScopeVector) ([]DeclaredLink, error)
	ReadEndpoint func(context.Context, model.ScopeMember, model.GraphRef) (FlowEndpointEvidence, error)
}

type flowEdge struct {
	link DeclaredLink
	to   string
}

func (r FlowResolver) ResolveFlow(ctx context.Context, req FlowRequest) (FlowResult, error) {
	if ctx == nil || r.ReadGroup == nil || r.Observe == nil || r.CheckSource == nil || r.ReadLinks == nil ||
		r.ReadEndpoint == nil || req.Pinned.Kind != "federated" || model.VerifyScopeVectorDigest(req.Pinned, scopeBytes) != nil ||
		(req.Direction != "forward" && req.Direction != "reverse") || req.MaxDepth < 1 || req.MaxDepth > 8 ||
		req.Limit < 1 || req.Limit > 100 {
		return FlowResult{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return FlowResult{}, err
	}
	root, rootKey, err := flowSelector(req.Root)
	if err != nil {
		return FlowResult{}, err
	}
	pinned, err := model.NormalizeScopeVector(req.Pinned, scopeBytes)
	if err != nil {
		return FlowResult{}, err
	}
	s := session{resolver: r.Resolver, ctx: ctx, pinned: pinned, configs: make(map[string]model.GroupMemberConfig),
		members: make(map[string]model.ScopeMember), out: Result{Gaps: []model.Gap{}}}
	if err := s.checkInitial(); err != nil {
		return FlowResult{}, err
	}
	out := FlowResult{Scope: pinned, Links: []FlowLink{}}
	if _, ok := s.members[*root.ServiceID]; !ok {
		s.gap("BRIDGE_MEMBER_UNAVAILABLE")
		return finishFlow(&s, out)
	}
	rootMember, err := model.MatchPinnedMember(pinned, root.Ref, root.ServiceID)
	if err != nil {
		return FlowResult{}, err
	}
	rootProof, err := r.endpoint(&s, root, rootMember)
	if errors.Is(err, ErrFlowEndpointUnavailable) {
		s.gap("BRIDGE_ENDPOINT_UNAVAILABLE")
		return finishFlow(&s, out)
	}
	if err != nil {
		return FlowResult{}, err
	}
	out.Root, out.RootSource = &root, &rootProof.Declaration
	links, err := r.ReadLinks(ctx, pinned)
	if err != nil {
		return FlowResult{}, err
	}
	edges, err := flowAdjacency(links, req.Direction)
	if err != nil {
		return FlowResult{}, err
	}
	if err := r.walk(&s, rootKey, req, edges, &out); err != nil {
		return FlowResult{}, err
	}
	return finishFlow(&s, out)
}

func finishFlow(s *session, out FlowResult) (FlowResult, error) {
	if err := s.recheck(); err != nil {
		return FlowResult{}, err
	}
	if err := s.ctx.Err(); err != nil {
		return FlowResult{}, err
	}
	out.Gaps, out.Partial = s.out.Gaps, s.out.Partial
	return out, nil
}

func flowSelector(sel model.RefSelector) (model.RefSelector, string, error) {
	raw, err := json.Marshal(sel)
	if err != nil {
		return model.RefSelector{}, "", ErrInput
	}
	selector, err := model.DecodeRefSelectorJSON(raw, scopeBytes)
	if err != nil || selector.Kind != "ref" || selector.ServiceID == nil {
		return model.RefSelector{}, "", ErrInput
	}
	switch selector.Ref.(type) {
	case *model.SymbolRef, *model.EntityRef:
	default:
		return model.RefSelector{}, "", ErrInput
	}
	canonical, err := json.Marshal(selector)
	return selector, string(canonical), err
}

func flowAdjacency(input []DeclaredLink, direction string) (map[string][]flowEdge, error) {
	if len(input) > 512 {
		return nil, ErrInput
	}
	links := slices.Clone(input)
	slices.SortFunc(links, func(a, b DeclaredLink) int { return strings.Compare(a.ID, b.ID) })
	edges := make(map[string][]flowEdge)
	for i, link := range links {
		if !bridgeIdentifier.MatchString(link.ID) || (i > 0 && links[i-1].ID == link.ID) {
			return nil, ErrInput
		}
		from, a, err := flowSelector(link.From.Selector)
		if err != nil {
			return nil, err
		}
		to, b, err := flowSelector(link.To.Selector)
		if err != nil {
			return nil, err
		}
		link.From.Selector, link.To.Selector = from, to
		if direction == "reverse" {
			a, b = b, a
		}
		edges[a] = append(edges[a], flowEdge{link: link, to: b})
	}
	return edges, nil
}

func (r FlowResolver) walk(s *session, root string, req FlowRequest, edges map[string][]flowEdge, out *FlowResult) error {
	type hop struct {
		node  string
		depth int
	}
	queue := []hop{{node: root}}
	seen := map[string]bool{root: true}
	encoded, err := json.Marshal(out)
	if err != nil {
		return err
	}
	remaining := resultBytes - len(encoded) - 8192
	for i := 0; i < len(queue); i++ {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		current := queue[i]
		if current.depth == req.MaxDepth {
			if len(edges[current.node]) != 0 {
				s.gap("BRIDGE_RESULT_LIMIT")
			}
			continue
		}
		for _, edge := range edges[current.node] {
			if len(out.Links) >= req.Limit {
				s.gap("BRIDGE_RESULT_LIMIT")
				return nil
			}
			found, gap, err := r.verifyLink(s, edge.link)
			if err != nil {
				return err
			}
			if gap != "" {
				s.gap(gap)
				continue
			}
			raw, err := json.Marshal(found)
			if err != nil {
				return err
			}
			if len(raw)+1 > remaining {
				s.gap("BRIDGE_RESULT_LIMIT")
				return nil
			}
			remaining -= len(raw) + 1
			out.Links = append(out.Links, found)
			if !seen[edge.to] {
				seen[edge.to] = true
				queue = append(queue, hop{node: edge.to, depth: current.depth + 1})
			}
		}
	}
	return nil
}

func (r FlowResolver) verifyLink(s *session, link DeclaredLink) (FlowLink, string, error) {
	left, right := link.From.Selector, link.To.Selector
	if _, ok := s.members[*left.ServiceID]; !ok {
		return FlowLink{}, "BRIDGE_MEMBER_UNAVAILABLE", nil
	}
	if _, ok := s.members[*right.ServiceID]; !ok {
		return FlowLink{}, "BRIDGE_MEMBER_UNAVAILABLE", nil
	}
	from, errFrom := model.MatchPinnedMember(s.pinned, left.Ref, left.ServiceID)
	to, errTo := model.MatchPinnedMember(s.pinned, right.Ref, right.ServiceID)
	if errFrom != nil || errTo != nil {
		return FlowLink{}, "BRIDGE_ENDPOINT_UNAVAILABLE", nil
	}
	if !configuredContract(s.configs[*left.ServiceID], link.From.Contract) ||
		!configuredContract(s.configs[*right.ServiceID], link.To.Contract) {
		return FlowLink{}, "BRIDGE_CONTRACT_UNVERIFIED", nil
	}
	if err := r.checkFlowSource(s, from, link.Declaration); err != nil {
		if errors.Is(err, ErrFlowEndpointUnavailable) {
			return FlowLink{}, "BRIDGE_DECLARATION_UNAVAILABLE", nil
		}
		return FlowLink{}, "", err
	}
	fromProof, err := r.endpoint(s, left, from)
	if errors.Is(err, ErrFlowEndpointUnavailable) {
		return FlowLink{}, "BRIDGE_ENDPOINT_UNAVAILABLE", nil
	}
	if err != nil {
		return FlowLink{}, "", err
	}
	toProof, err := r.endpoint(s, right, to)
	if errors.Is(err, ErrFlowEndpointUnavailable) {
		return FlowLink{}, "BRIDGE_ENDPOINT_UNAVAILABLE", nil
	}
	if err != nil {
		return FlowLink{}, "", err
	}
	if !reflect.DeepEqual(fromProof.Contract, link.From.Contract) || !reflect.DeepEqual(toProof.Contract, link.To.Contract) {
		return FlowLink{}, "BRIDGE_CONTRACT_UNVERIFIED", nil
	}
	return FlowLink{DeclaredLink: link, FromSource: fromProof.Declaration, ToSource: toProof.Declaration}, "", nil
}

func (r FlowResolver) endpoint(s *session, endpoint model.RefSelector, member model.ScopeMember) (FlowEndpointEvidence, error) {
	proof, err := r.ReadEndpoint(s.ctx, member, endpoint.Ref)
	if err != nil {
		return FlowEndpointEvidence{}, err
	}
	if !reflect.DeepEqual(endpoint.Ref, proof.Ref) || !configuredContract(s.configs[*endpoint.ServiceID], proof.Contract) {
		return FlowEndpointEvidence{}, ErrFlowEndpointUnavailable
	}
	if err := r.checkFlowSource(s, member, proof.Declaration); err != nil {
		return FlowEndpointEvidence{}, err
	}
	return proof, nil
}

func (r FlowResolver) checkFlowSource(s *session, member model.ScopeMember, source model.SourceRef) error {
	matched, err := model.MatchPinnedSource(s.pinned, source, member.ServiceID, scopeBytes)
	if err != nil || !reflect.DeepEqual(matched, member) {
		return ErrFlowEndpointUnavailable
	}
	return r.CheckSource(s.ctx, member, source)
}

func configuredContract(cfg model.GroupMemberConfig, key contracts.Key) bool {
	protocol := key.Protocol
	switch protocol {
	case "frontend":
		protocol = "http"
	case "asyncapi":
		protocol = "event"
	case "mysql":
		protocol = "sql"
	}
	n := cfg.Namespace
	return value(key.ServiceID) == cfg.ServiceID && key.Environment == n.Environment && protocol == n.Protocol &&
		n.ContractVersion != nil && key.Version == *n.ContractVersion && bridgeIdentifier.MatchString(key.Namespace) &&
		len(key.Identifier) > 0 && len(key.Identifier) <= 1024 &&
		utf8.ValidString(key.Identifier) && strings.IndexFunc(key.Identifier, unicode.IsControl) == -1
}
