package federation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type flowFixture struct {
	scope       *fixture
	request     FlowRequest
	resolver    FlowResolver
	endpoints   map[string]FlowEndpointEvidence
	links       []DeclaredLink
	linkReads   int
	lookupReads []string
}

func makeFlowFixture(t *testing.T) *flowFixture {
	t.Helper()
	f := makeFixture(t)
	ui, api, billing := f.req.Pinned.Members[0], f.req.Pinned.Members[1], f.req.Pinned.Members[2]
	python := member("python", 3)
	eventConfig := configured(python, "http://events.invalid", "/")
	eventConfig.Namespace.Protocol = "event"
	f.group.Config.Members = append(f.group.Config.Members, eventConfig)
	f.req.Pinned.Members = append(f.req.Pinned.Members, python)
	f.seal(t)
	owner := &flowFixture{scope: f, endpoints: map[string]FlowEndpointEvidence{}}
	add := func(m model.ScopeMember, symbol, protocol, namespace, identifier, path, source string) ContractEndpoint {
		key := contracts.Key{ServiceID: m.ServiceID, Environment: "local", Protocol: protocol, Namespace: namespace,
			Identifier: identifier, Version: "v1"}
		ref := &model.SymbolRef{Kind: "symbol", GraphIdentity: graphID(m), SymbolID: symbol}
		owner.endpoints[*m.ServiceID] = FlowEndpointEvidence{Ref: ref, Contract: key, Declaration: f.source(m, path, source)}
		return ContractEndpoint{Selector: model.RefSelector{Kind: "ref", Ref: ref, ServiceID: m.ServiceID}, Contract: key}
	}
	frontend := add(ui, "submit", "frontend", "orders", "POST /orders", "orders.tsx", "export const submit = () => '/orders'")
	backend := add(api, "same-handler", "http", "orders", "POST /orders", "orders.go", "func place() {}")
	_ = add(billing, "same-handler", "http", "billing", "POST /orders", "orders.go", "func place() {}")
	consumer := add(python, "consume", "asyncapi", "orders-broker", "orders.created", "consumer.py", "def consume(event): pass")
	for _, entry := range []struct {
		id       string
		producer model.ScopeMember
		from, to ContractEndpoint
	}{{"frontend-to-api", ui, frontend, backend}, {"api-to-consumer", api, backend, consumer}} {
		raw, err := json.Marshal(map[string]any{"format": "mce4d-federated-links-v1", "links": []any{
			map[string]any{"id": entry.id, "from": entry.from, "to": entry.to}}})
		if err != nil {
			t.Fatal(err)
		}
		ref := f.source(entry.producer, entry.id+".bridge.json", string(raw))
		links, err := DecodeDeclaredLinks(ref, raw)
		if err != nil || len(links) != 1 {
			t.Fatalf("explicit declaration: %+v %v", links, err)
		}
		owner.links = append(owner.links, links[0])
	}
	owner.request = FlowRequest{Pinned: f.req.Pinned, Root: frontend.Selector, Direction: "forward", MaxDepth: 4, Limit: 20}
	owner.resolver = FlowResolver{
		Resolver: f.resolver(),
		ReadLinks: func(_ context.Context, seen model.ScopeVector) ([]DeclaredLink, error) {
			owner.linkReads++
			if seen.VectorDigest != owner.request.Pinned.VectorDigest {
				return nil, ErrScopeChanged
			}
			return owner.links, nil
		},
		ReadEndpoint: func(_ context.Context, member model.ScopeMember, ref model.GraphRef) (FlowEndpointEvidence, error) {
			service := value(member.ServiceID)
			owner.lookupReads = append(owner.lookupReads, service)
			proof, ok := owner.endpoints[service]
			if !ok || !reflect.DeepEqual(proof.Ref, ref) {
				return FlowEndpointEvidence{}, ErrFlowEndpointUnavailable
			}
			return proof, nil
		},
	}
	return owner
}

func TestC063FederatedFlowTraversesOnlyExplicitContractDeclarations(t *testing.T) {
	f := makeFlowFixture(t)
	ctx := context.Background()
	out, err := f.resolver.ResolveFlow(ctx, f.request)
	if err != nil || out.Partial || len(out.Links) != 2 || out.RootSource == nil || out.RootSource.Path != "orders.tsx" ||
		out.Links[0].ID != "frontend-to-api" || out.Links[1].ID != "api-to-consumer" ||
		out.Links[1].ToSource.Path != "consumer.py" || out.Scope.VectorDigest != f.request.Pinned.VectorDigest {
		t.Fatalf("forward: %+v %v", out, err)
	}
	reverse := f.request
	reverse.Root = f.links[1].To.Selector
	reverse.Direction = "reverse"
	out, err = f.resolver.ResolveFlow(ctx, reverse)
	if err != nil || len(out.Links) != 2 || out.Links[0].ID != "api-to-consumer" || out.Links[1].ID != "frontend-to-api" {
		t.Fatalf("reverse: %+v %v", out, err)
	}
	limited := f.request
	limited.MaxDepth = 1
	out, err = f.resolver.ResolveFlow(ctx, limited)
	if err != nil || len(out.Links) != 1 || !flowHasGap(out, "BRIDGE_RESULT_LIMIT") {
		t.Fatalf("depth: %+v %v", out, err)
	}
	ambiguous := f.links[0].To.Selector
	ambiguous.ServiceID = nil
	limited.Root = ambiguous
	if _, err := f.resolver.ResolveFlow(ctx, limited); !errors.Is(err, ErrInput) {
		t.Fatalf("C163 unqualified symbol: %v", err)
	}
	limited.Root.ServiceID = ptr("billing")
	out, err = f.resolver.ResolveFlow(ctx, limited)
	if err != nil || len(out.Links) != 0 || out.Root == nil || value(out.Root.ServiceID) != "billing" {
		t.Fatalf("C163 service alias lost identity: %+v %v", out, err)
	}
	proof := f.endpoints["python"]
	proof.Contract.Namespace = "other-broker"
	f.endpoints["python"] = proof
	out, err = f.resolver.ResolveFlow(ctx, f.request)
	if err != nil || len(out.Links) != 1 || !flowHasGap(out, "BRIDGE_CONTRACT_UNVERIFIED") {
		t.Fatalf("C063 namespace mismatch: %+v %v", out, err)
	}
	delete(f.endpoints, "python")
	out, err = f.resolver.ResolveFlow(ctx, f.request)
	if err != nil || len(out.Links) != 1 || !flowHasGap(out, "BRIDGE_ENDPOINT_UNAVAILABLE") {
		t.Fatalf("C063 missing schema: %+v %v", out, err)
	}
}

func TestC164C165C166FederatedFlowRetainsScopeAndWithholdsUnavailableMembers(t *testing.T) {
	f := makeFlowFixture(t)
	ctx := context.Background()
	pinned := f.scope.req.Pinned
	f.scope.req.Pinned.Members = pinned.Members[:3]
	f.scope.req.Pinned.UnavailableMembers = []model.MemberUnavailable{{Status: "access_denied", ReasonCode: "ACCESS_DENIED"}}
	f.scope.seal(t)
	f.request.Pinned = f.scope.req.Pinned
	out, err := f.resolver.ResolveFlow(ctx, f.request)
	body, _ := json.Marshal(out)
	if err != nil || len(out.Links) != 1 || !flowHasGap(out, "BRIDGE_MEMBER_UNAVAILABLE") ||
		strings.Contains(string(body), "consumer.py") || strings.Contains(strings.Join(f.lookupReads, ","), "python") {
		t.Fatalf("C164 denied member leaked: %+v %v", out, err)
	}
	for _, read := range f.scope.reads {
		if read.Path == "api-to-consumer.bridge.json" {
			t.Fatalf("read bridge with denied endpoint: %+v", read)
		}
	}
	f.scope.req.Pinned.Members = []model.ScopeMember{}
	f.scope.req.Pinned.UnavailableMembers = []model.MemberUnavailable{
		{Status: "access_denied", ReasonCode: "ACCESS_DENIED"},
		{Status: "access_denied", ReasonCode: "ACCESS_DENIED"},
		{Status: "access_denied", ReasonCode: "ACCESS_DENIED"},
		{Status: "access_denied", ReasonCode: "ACCESS_DENIED"},
	}
	f.scope.seal(t)
	f.request.Pinned = f.scope.req.Pinned
	f.lookupReads, f.linkReads = nil, 0
	out, err = f.resolver.ResolveFlow(ctx, f.request)
	if err != nil || !out.Partial || out.Root != nil || len(out.Links) != 0 || len(f.lookupReads) != 0 || f.linkReads != 0 {
		t.Fatalf("C164 all unavailable: %+v %v", out, err)
	}
	f = makeFlowFixture(t)
	seen := 0
	f.resolver.Observe = func(_ context.Context, exact model.ScopeVector) (model.ScopeVector, error) {
		seen++
		if seen == 1 {
			return exact, nil
		}
		later := exact
		later.Members = append([]model.ScopeMember{}, exact.Members...)
		later.Members[0].GenerationID = ids(77)
		later.VectorDigest, _ = model.ComputeScopeVectorDigest(later, scopeBytes)
		return later, nil
	}
	out, err = f.resolver.ResolveFlow(ctx, f.request)
	if !errors.Is(err, ErrScopeChanged) || seen != 2 || out.Root != nil || len(out.Links) != 0 {
		t.Fatalf("C165 pinned vector changed before return: %+v %v", out, err)
	}
	f.request.Pinned.VectorDigest = strings.Repeat("0", 64)
	if _, err := f.resolver.ResolveFlow(ctx, f.request); !errors.Is(err, ErrInput) || seen != 2 {
		t.Fatalf("C166 digest mismatch touched authority: %v", err)
	}
}

func flowHasGap(out FlowResult, code string) bool {
	for _, gap := range out.Gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}
