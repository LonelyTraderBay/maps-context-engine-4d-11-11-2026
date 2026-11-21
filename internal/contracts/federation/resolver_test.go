package federation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/gohttp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/sql"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type fixture struct {
	req   Request
	group model.GroupSummary
	reads []model.SourceRef
	files map[string][]byte
}

func ptr[T any](v T) *T { return &v }

func ids(n int) string {
	return fmt.Sprintf("01234567-0123-4123-8123-%012d", n)
}

func member(service string, i int) model.ScopeMember {
	return model.ScopeMember{WorktreeID: ids(i), ProfileID: strings.Repeat("b", 64),
		GenerationID: ids(i + 5), SnapshotDigest: strings.Repeat("c", 64), PolicyDigest: strings.Repeat("d", 64),
		BindingRevision: 1, RecordEpoch: 1, PolicyEpoch: 1, ServiceID: ptr(service),
		IndexIncarnation: ids(i + 10), RecordsIncarnation: ids(i + 15)}
}

func graphID(m model.ScopeMember) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: m.GenerationID}
}

func (f *fixture) source(m model.ScopeMember, path, raw string) model.SourceRef {
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	f.files[sum] = []byte(raw)
	return model.SourceRef{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: m.GenerationID,
		Path: path, ContentHash: sum, Range: model.ByteRange{EndByte: int64(len(raw))}}
}

func (f *fixture) seal(t *testing.T) {
	t.Helper()
	digest, err := model.ComputeScopeVectorDigest(f.req.Pinned, scopeBytes)
	if err != nil {
		t.Fatal(err)
	}
	f.req.Pinned.VectorDigest = digest
}

func configured(m model.ScopeMember, origin, prefix string) model.GroupMemberConfig {
	return model.GroupMemberConfig{ServiceID: *m.ServiceID, WorktreeID: m.WorktreeID, ProfileID: m.ProfileID,
		Namespace: model.ServiceNamespace{Protocol: "http", Environment: "local", BaseOrigin: &origin,
			RoutePrefix: &prefix, ContractVersion: ptr("v1")}}
}

func makeFixture(t *testing.T) *fixture {
	t.Helper()
	ui, api, billing := member("ui", 1), member("api", 2), member("billing", 2)
	f := &fixture{files: make(map[string][]byte)}
	f.group = model.GroupSummary{Status: "active", Revision: 4,
		Config: model.GroupConfig{GroupID: ids(40), Name: "Fixture", Members: []model.GroupMemberConfig{
			configured(ui, "http://ui.invalid:5173", "/app"),
			configured(api, "http://api.invalid:8080", "/svc"),
			configured(billing, "http://billing.invalid:8080", "/svc"),
		}}}
	f.req.Pinned = model.ScopeVector{Kind: "federated", GroupID: &f.group.Config.GroupID, GroupRevision: &f.group.Revision,
		CatalogIncarnation: ids(30), Members: []model.ScopeMember{ui, api, billing}, UnavailableMembers: []model.MemberUnavailable{}}
	f.req.Limit = 10
	f.seal(t)
	site := f.source(ui, "src/Screen.tsx", `fetch("/backend/orders", { method: "POST" });`)
	client := contracts.FrontendClient{Caller: model.SymbolRef{Kind: "symbol", GraphIdentity: graphID(ui), SymbolID: "load-orders"},
		Site: site, ServiceID: ui.ServiceID, Target: contracts.Key{ServiceID: api.ServiceID, Environment: "local",
			Protocol: "http", Namespace: "orders", Identifier: "POST /backend/orders", Version: "v1"}}
	f.req.Clients = []contracts.FrontendClient{client}
	f.req.Proxies = []ProxyRule{{FrontendService: "ui", BackendService: "api", PublicPrefix: "/backend", BackendPrefix: "/svc",
		Declaration: f.source(ui, "proxy.json", `{"/backend":"/svc"}`)}}
	f.req.Backends = []Backend{f.backend(t, billing), f.backend(t, api)}
	return f
}

func (f *fixture) backend(t *testing.T, b model.ScopeMember) Backend {
	t.Helper()
	ctx := context.Background()
	doc := `{"openapi":"3.1.1","info":{"version":"v1"},"servers":[{"url":"/svc"}],
	  "paths":{"/orders":{"post":{"operationId":"place","responses":{"200":{"description":"OK"}}}}}}`
	config := schema.Profile{ServiceID: *b.ServiceID, Environment: "local", Namespace: "orders"}
	out, err := schema.AnalyzeCaptured(ctx, config, []schema.Capture{
		{Kind: "openapi", Version: "v1", Ref: f.source(b, "api.json", doc), Bytes: []byte(doc)},
	})
	if err != nil || len(out.Operations) != 1 {
		t.Fatalf("backend contract fixture: %v", err)
	}
	sqlText := "CREATE TABLE orders (id INT NOT NULL) ENGINE=InnoDB;"
	sqlRef := f.source(b, "schema.sql", sqlText)
	profile := sql.Profile{ServiceID: b.ServiceID, Dialect: "mysql", Version: "8.4", Engine: "innodb",
		SQLMode: "default", Environment: "local", Namespace: "orders", Schema: "sales"}
	store, err := sql.AnalyzeCaptured(ctx, profile, []sql.Source{{Kind: "schema", Ref: sqlRef, Data: []byte(sqlText)}})
	if err != nil || len(store.Batch.Entities) != 1 {
		t.Fatalf("SQL schema fixture: %v", err)
	}
	route := flowview.Route{Pattern: gohttp.Pattern{Method: "POST", Path: "/svc/orders"},
		Site:              f.source(b, "server.go", `m.HandleFunc("POST /svc/orders", place)`),
		RegistrationOwner: model.SymbolRef{Kind: "symbol", GraphIdentity: graphID(b), SymbolID: "routes"},
		Handler:           model.SymbolRef{Kind: "symbol", GraphIdentity: graphID(b), SymbolID: "place"}}
	return Backend{Operation: out.Operations[0], Route: route, SQL: []flowview.SQLSite{{StepID: "sql-site",
		Operation: "INSERT", Source: f.source(b, "store.go", `store.Exec("INSERT INTO orders (id) VALUES (?)", value)`),
		Targets: store.Batch.Entities, OutcomeStatus: "outcome_unknown"}}}
}

func (f *fixture) resolver() Resolver {
	return Resolver{
		ReadGroup: func(context.Context, string) (model.GroupSummary, string, error) {
			return f.group, f.req.Pinned.CatalogIncarnation, nil
		},
		Observe: func(context.Context, model.ScopeVector) (model.ScopeVector, error) {
			return f.req.Pinned, nil
		},
		CheckSource: func(_ context.Context, member model.ScopeMember, src model.SourceRef) error {
			if graphID(member) != (model.GraphIdentity{WorktreeID: src.WorktreeID, ProfileID: src.ProfileID, GenerationID: src.GenerationID}) {
				return ErrInput
			}
			f.reads = append(f.reads, src)
			return src.ValidateCapturedBytes(f.files[src.ContentHash])
		},
	}
}

func hasGap(out Result, code string) bool {
	for _, gap := range out.Gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}

func TestC100ExplicitProxyBindsOnlySelectedService(t *testing.T) {
	f := makeFixture(t)
	out, err := f.resolver().Resolve(context.Background(), f.req)
	if err != nil || len(out.Paths) != 1 || out.Partial {
		t.Fatalf("expected one exact path: %+v %v", out, err)
	}
	p := out.Paths[0]
	if value(p.Binding.Handler.Operation.Key.ServiceID) != "api" || p.Request.Path != "/backend/orders" ||
		p.Route.Pattern.Path != "/svc/orders" || p.Proxy == nil || *p.Proxy != f.req.Proxies[0].Declaration ||
		len(p.SQL) != 1 || p.SQL[0].OutcomeStatus != "outcome_unknown" {
		t.Fatalf("mixed backend or route: %+v", p)
	}
	r := f.req
	r.Proxies = nil
	out, err = f.resolver().Resolve(context.Background(), r)
	if err != nil || len(out.Paths) != 0 || !hasGap(out, "BRIDGE_PROXY_UNRESOLVED") {
		t.Fatalf("missing proxy guessed: %+v %v", out, err)
	}
	r = f.req
	r.Clients = append([]contracts.FrontendClient{}, r.Clients...)
	r.Clients[0].Target.Identifier = "POST /backend-other/orders"
	out, err = f.resolver().Resolve(context.Background(), r)
	if err != nil || len(out.Paths) != 0 || !hasGap(out, "BRIDGE_PROXY_UNRESOLVED") {
		t.Fatalf("prefix overlap guessed: %+v %v", out, err)
	}
	r = f.req
	r.Proxies = append(r.Proxies, r.Proxies[0])
	out, err = f.resolver().Resolve(context.Background(), r)
	if err != nil || len(out.Paths) != 0 || !hasGap(out, "BRIDGE_PROXY_AMBIGUOUS") {
		t.Fatalf("ambiguous routing guessed: %+v %v", out, err)
	}
}

func TestC103ReverseTableKeepsEveryVerifiedFrontendSite(t *testing.T) {
	f := makeFixture(t)
	another := f.req.Clients[0]
	another.Caller.SymbolID = "update-orders"
	another.Site = f.source(f.req.Pinned.Members[0], "src/Customer.tsx", `fetch("/backend/orders", { method: "POST" });`)
	f.req.Clients = append(f.req.Clients, another)
	target := f.req.Backends[1].SQL[0].Targets[0]
	f.req.Reverse = &model.RefSelector{Kind: "ref", Ref: &target.Ref, ServiceID: ptr("api")}
	out, err := f.resolver().Resolve(context.Background(), f.req)
	if err != nil || out.Partial || len(out.Paths) != 2 ||
		out.Paths[0].Binding.Client.Caller.SymbolID != "update-orders" ||
		out.Paths[1].Binding.Client.Caller.SymbolID != "load-orders" ||
		out.Scope.VectorDigest != f.req.Pinned.VectorDigest {
		t.Fatalf("reverse reachability: %+v %v", out, err)
	}
	f.req.Limit = 1
	out, err = f.resolver().Resolve(context.Background(), f.req)
	if err != nil || !out.Partial || len(out.Paths) != 1 || !hasGap(out, "BRIDGE_RESULT_LIMIT") {
		t.Fatalf("partial reverse limit: %+v %v", out, err)
	}
	f.req.Reverse.ServiceID = nil
	if _, err := f.resolver().Resolve(context.Background(), f.req); !errors.Is(err, ErrInput) {
		t.Fatalf("unqualified member selector accepted: %v", err)
	}
}

func TestC106DeniedAndStaleMembersDoNotReturnSource(t *testing.T) {
	f := makeFixture(t)
	f.req.Pinned.Members = f.req.Pinned.Members[:1]
	hidden := "unavailable-api"
	f.req.Backends[1].Route.Site.Path = hidden
	f.req.Pinned.UnavailableMembers = []model.MemberUnavailable{
		{Status: "access_denied", ReasonCode: "ACCESS_DENIED"},
		{Status: "access_denied", ReasonCode: "ACCESS_DENIED"},
	}
	f.seal(t)
	out, err := f.resolver().Resolve(context.Background(), f.req)
	wire, _ := json.Marshal(out)
	if err != nil || !out.Partial || len(out.Paths) != 0 || len(f.reads) != 0 ||
		!hasGap(out, "BRIDGE_MEMBER_UNAVAILABLE") || strings.Contains(string(wire), hidden) {
		t.Fatalf("unavailable member leaked: %+v %v", out, err)
	}
	f = makeFixture(t)
	f.req.Backends[1].Operation.Entity.Ref.GenerationID = ids(60)
	f.req.Backends[1].Route.Handler.GenerationID = ids(60)
	f.req.Backends[1].Route.RegistrationOwner.GenerationID = ids(60)
	out, err = f.resolver().Resolve(context.Background(), f.req)
	if err != nil || len(out.Paths) != 0 || !hasGap(out, "BRIDGE_OPERATION_UNRESOLVED") {
		t.Fatalf("stale operation joined: %+v %v", out, err)
	}
	for _, ref := range f.reads {
		if ref.Path == "api.json" || ref.Path == "server.go" {
			t.Fatalf("stale backend source read: %+v", ref)
		}
	}
}

func TestC165C166RecheckRejectsChangeBetweenSelectionAndReturn(t *testing.T) {
	f := makeFixture(t)
	r := f.resolver()
	seen := 0
	r.Observe = func(context.Context, model.ScopeVector) (model.ScopeVector, error) {
		seen++
		if seen == 1 {
			return f.req.Pinned, nil
		}
		later := f.req.Pinned
		later.Members = append([]model.ScopeMember{}, later.Members...)
		later.Members[0].GenerationID = ids(77)
		later.VectorDigest, _ = model.ComputeScopeVectorDigest(later, scopeBytes)
		return later, nil
	}
	out, err := r.Resolve(context.Background(), f.req)
	if !errors.Is(err, ErrScopeChanged) || len(out.Paths) != 0 || seen != 2 {
		t.Fatalf("active vector fallback: %+v %v", out, err)
	}
	f.req.Pinned.VectorDigest = strings.Repeat("0", 64)
	if _, err := r.Resolve(context.Background(), f.req); !errors.Is(err, ErrInput) {
		t.Fatalf("bad vector digest admitted: %v", err)
	}
}

func TestC100ContractVersionAndDuplicateRouteAreBoundaries(t *testing.T) {
	f := makeFixture(t)
	f.req.Clients[0].Target.Version = "v2"
	out, err := f.resolver().Resolve(context.Background(), f.req)
	if err != nil || len(out.Paths) != 0 || !hasGap(out, "BRIDGE_NAMESPACE_UNRESOLVED") {
		t.Fatalf("wrong contract revision joined: %+v %v", out, err)
	}
	f = makeFixture(t)
	other := f.req.Backends[1]
	other.Route.Handler.SymbolID = "extra-handler"
	f.req.Backends = append(f.req.Backends, other)
	out, err = f.resolver().Resolve(context.Background(), f.req)
	if err != nil || len(out.Paths) != 0 || !hasGap(out, "BRIDGE_OPERATION_AMBIGUOUS") {
		t.Fatalf("ambiguous operation joined: %+v %v", out, err)
	}
}
