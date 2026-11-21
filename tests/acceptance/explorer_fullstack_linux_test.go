//go:build linux

package acceptance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/federation"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/sql"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const uiFixture = `import { Route, Routes } from "react-router-dom";
function Orders() { return <main><button onClick={placeOrder}>Place order</button></main>; }
export const Screen = <Routes><Route path="/orders" element={<Orders />} /></Routes>;
export function placeOrder(retry: boolean) {
  if (retry) return fetch("/backend/orders", { method: "POST" });
  return fetch("/backend/orders", { method: "POST" });
}
`

const goFixture = `package orders

import ("database/sql"; "net/http")

var db *sql.DB
func init() { panic("the target is never executed in this fixture") }
func Routes(m *http.ServeMux) { m.HandleFunc("POST /svc/orders", PlaceOrder) }
func PlaceOrder(http.ResponseWriter, *http.Request) {
 if rejected() { return }
 save()
}
func rejected() bool { return false }
func save() {
 tx, err := db.Begin()
 if err != nil { return }
 tx.Exec("INSERT INTO orders (quantity) VALUES (?)", 1)
 tx.Exec("UPDATE orders SET quantity = ?", 2)
 tx.Rollback()
}
`

const orderDeclaration = "CREATE TABLE orders (quantity INT NOT NULL) ENGINE=InnoDB;"
const orderContract = `{"openapi":"3.1.1","info":{"version":"v1"},"servers":[{"url":"/svc"}],
  "paths":{"/orders":{"post":{"operationId":"placeOrder","responses":{"200":{"description":"OK"}}}}}}`

func stackUUID(id int) string { return fmt.Sprintf("8f2a0000-0000-4bad-8ace-%012d", id) }

func pointer[T any](v T) *T { return &v }

func requireFullstack(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestT100AuthoredFullstackStaticChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	frontendSources := map[string]string{
		"app/tsconfig.json": `{"compilerOptions":{"target":"es2022","module":"esnext","moduleResolution":"bundler",
		  "jsx":"preserve","types":[],"lib":["es2022","dom"]},"files":["orders.tsx","router.d.ts"]}`,
		"app/orders.tsx": uiFixture,
		"app/router.d.ts": `declare module "react-router-dom" {
		  export function Route(value: object): JSX.Element; export function Routes(value: object): JSX.Element;
		}
		declare namespace JSX { interface Element {} interface IntrinsicElements {
		  main: { children?: unknown }; button: { onClick?: unknown; children?: unknown };
		} }`,
		"app/proxy.json":   `{"/backend":"/svc"}`,
		"app/package.json": `{"scripts":{"prepare":"node ./canary.cjs"}}`,
		"app/canary.cjs":   `require("node:fs").writeFileSync("ran-canary", "unexpected");`,
	}
	backendSources := map[string]string{
		"go.mod": "module example.invalid/orders\n\ngo 1.27.4\n", "handler.go": goFixture,
		"schema.sql": orderDeclaration, "api.json": orderContract,
		"event-bindings.json": ordersWorkerBinding,
	}
	web, frontend, owner := makeFrontendOwner(t, ctx, frontendSources)
	if len(frontend.Routes) != 1 || frontend.Routes[0].Path != "/orders" ||
		len(frontend.Clients) != 2 || owner.SymbolID == "" || len(frontend.Gaps) != 0 {
		t.Fatalf("frontend compiler result does not contain the fixture chain: %+v", frontend)
	}
	vite := exec.CommandContext(ctx, os.Getenv("MCE4D_T055_NODE"), "explorer_fullstack_build.mjs",
		web.source, web.sourceRef("app/orders.tsx").ContentHash)
	buildLog, err := vite.CombinedOutput()
	if err != nil {
		t.Fatalf("Vite build of the captured frontend: %v\n%s", err, buildLog)
	}
	t.Log(strings.TrimSpace(string(buildLog)))
	api, program := makeGoOwner(t, ctx, backendSources)
	routes := program.Routes()
	if len(routes) != 1 || routes[0].Pattern.Method != "POST" || routes[0].Pattern.Path != "/svc/orders" {
		t.Fatalf("Go typed route incorrect: %+v", routes)
	}
	route := routes[0]
	exact, err := api.readSource(ctx, route.Site)
	requireFullstack(t, err)
	if exact != `m.HandleFunc("POST /svc/orders", PlaceOrder)` {
		t.Fatalf("registration bytes differ: %q", exact)
	}
	sqlProfile := sql.Profile{Dialect: "mysql", Version: "8.4", Engine: "innodb", SQLMode: "default",
		Environment: "local", Namespace: "orders", Schema: "sales", ServiceID: api.member.ServiceID}
	projected, err := program.Project(ctx, route.Handler, flowview.Options{Steps: 60, Edges: 180, CallDepth: 2,
		Registration: &route.Site, SQL: &flowview.SQLInput{Profile: sqlProfile, Schemas: []model.SourceRef{api.sourceRef("schema.sql")}}})
	requireFullstack(t, err)
	for _, step := range projected.Data.Steps {
		if step.OrderSemantics == "causal_trace" || strings.HasSuffix(step.OutcomeStatus, "_observed") {
			t.Fatalf("static step claims live execution: %+v", step)
		}
	}
	docRef := api.sourceRef("api.json")
	docBytes, err := api.readSource(ctx, docRef)
	requireFullstack(t, err)
	schemaBound, err := schema.AnalyzeCaptured(ctx, schema.Profile{ServiceID: "orders", Environment: "local", Namespace: "orders"},
		[]schema.Capture{{Kind: "openapi", Version: "v1", Ref: docRef, Bytes: []byte(docBytes)}})
	requireFullstack(t, err)
	if len(schemaBound.Operations) != 1 || !schemaBound.Operations[0].Bindable {
		t.Fatalf("missing OpenAPI operation: %+v", schemaBound)
	}

	const frontOrigin, backOrigin, version = "http://ui.invalid", "http://orders.invalid", "v1"
	catalogID, groupID := stackUUID(31), stackUUID(32)
	group := model.GroupSummary{Status: "active", Revision: 1, Config: model.GroupConfig{GroupID: groupID, Name: "Fixture",
		Members: []model.GroupMemberConfig{
			{ServiceID: "web", WorktreeID: web.member.WorktreeID, ProfileID: web.member.ProfileID,
				Namespace: model.ServiceNamespace{Protocol: "http", Environment: "local", BaseOrigin: pointer(frontOrigin), RoutePrefix: pointer("/")}},
			{ServiceID: "orders", WorktreeID: api.member.WorktreeID, ProfileID: api.member.ProfileID,
				Namespace: model.ServiceNamespace{Protocol: "http", Environment: "local", BaseOrigin: pointer(backOrigin),
					RoutePrefix: pointer("/svc"), ContractVersion: pointer(version)}},
		}}}
	members := map[string]*fullstackMember{"web": web, "orders": api}
	denied := false
	authority := app.CorePrincipal{ID: "fixture-owner"}
	groupReader := compose.GroupScopeResolver{
		ReadGroup: func(ctx context.Context, p app.CorePrincipal, id string) (model.GroupSummary, string, error) {
			if p != authority || id != groupID {
				return model.GroupSummary{}, "", compose.ErrAuthority
			}
			return group, catalogID, ctx.Err()
		},
		AuthorizeMember: func(ctx context.Context, p app.CorePrincipal, cfg model.GroupMemberConfig) error {
			if p != authority || members[cfg.ServiceID] == nil || denied && cfg.ServiceID == "orders" {
				return compose.ErrAuthority
			}
			return ctx.Err()
		},
		ReadMember: func(ctx context.Context, p app.CorePrincipal, cfg model.GroupMemberConfig, generation string) (model.ScopeVector, error) {
			owner := members[cfg.ServiceID]
			if p != authority || owner == nil || cfg.WorktreeID != owner.member.WorktreeID || cfg.ProfileID != owner.member.ProfileID {
				return model.ScopeVector{}, compose.ErrAuthority
			}
			seen, err := owner.facts.ReadGeneration(ctx, cfg.ProfileID, generation, owner.authorizeGeneration, fullstackBytes)
			if err != nil {
				return model.ScopeVector{}, err
			}
			member := owner.member
			member.ServiceID, member.GenerationID, member.SnapshotDigest = nil, seen.GenerationID, seen.SnapshotDigest
			result := model.ScopeVector{Kind: "single", CatalogIncarnation: catalogID,
				Members: []model.ScopeMember{member}, UnavailableMembers: []model.MemberUnavailable{}}
			result.VectorDigest, err = model.ComputeScopeVectorDigest(result, fullstackBytes)
			return result, err
		},
	}
	bound, err := groupReader.Resolve(ctx, authority, groupID)
	requireFullstack(t, err)
	graphOwner := model.SymbolRef{Kind: "symbol", SymbolID: owner.SymbolID,
		GraphIdentity: model.GraphIdentity{WorktreeID: web.member.WorktreeID, ProfileID: web.member.ProfileID,
			GenerationID: web.member.GenerationID}}
	var clients []contracts.FrontendClient
	for _, call := range frontend.Clients {
		site, err := web.readSource(ctx, call.Site)
		requireFullstack(t, err)
		if call.Method != "POST" || call.Path != "/backend/orders" || site != `fetch("/backend/orders", { method: "POST" })` {
			t.Fatalf("client callsite is not verified: %+v %q", call, site)
		}
		clients = append(clients, contracts.FrontendClient{Caller: graphOwner, ServiceID: web.member.ServiceID, Site: call.Site,
			Target: contracts.Key{ServiceID: api.member.ServiceID, Environment: "local", Protocol: "http", Namespace: "orders",
				Identifier: "POST /backend/orders", Version: version}})
	}
	readCalls := 0
	resolver := federation.Resolver{
		ReadGroup: func(ctx context.Context, id string) (model.GroupSummary, string, error) {
			return groupReader.ReadGroup(ctx, authority, id)
		},
		Observe: func(ctx context.Context, want model.ScopeVector) (model.ScopeVector, error) {
			return groupReader.Recheck(ctx, authority, want)
		},
		CheckSource: func(ctx context.Context, member model.ScopeMember, ref model.SourceRef) error {
			readCalls++
			if member.ServiceID == nil || members[*member.ServiceID] == nil {
				return compose.ErrAuthority
			}
			_, err := members[*member.ServiceID].readSource(ctx, ref)
			return err
		},
	}
	input := federation.Request{Pinned: bound, Clients: clients, Limit: 20,
		Backends: []federation.Backend{{Operation: schemaBound.Operations[0], Route: route, SQL: projected.SQL}},
		Proxies: []federation.ProxyRule{{FrontendService: "web", BackendService: "orders", PublicPrefix: "/backend",
			BackendPrefix: "/svc", Declaration: web.sourceRef("app/proxy.json")}}}
	forward, err := resolver.Resolve(ctx, input)
	requireFullstack(t, err)
	if len(forward.Paths) != 2 || readCalls == 0 {
		t.Fatalf("static forward paths missing: %+v", forward)
	}
	var table *model.EntityRef
	for _, path := range forward.Paths {
		if path.Route.Handler != route.Handler || path.Request.Path != "/backend/orders" || path.Proxy == nil {
			t.Fatalf("scope or route changed: %+v", path)
		}
		for _, effect := range path.SQL {
			if effect.OutcomeStatus != "outcome_unknown" {
				t.Fatalf("static SQL claims persistence: %+v", effect)
			}
			if effect.Operation == "INSERT" && len(effect.Targets) == 1 {
				found := effect.Targets[0]
				table = &found.Ref
				got, err := api.readSource(ctx, found.Declaration)
				requireFullstack(t, err)
				if got != strings.TrimSuffix(orderDeclaration, ";") {
					t.Fatalf("SQL declaration bytes differ: %q", got)
				}
			}
		}
	}
	if table == nil {
		t.Fatal("fixture did not bind an INSERT to the declared table")
	}
	input.Reverse = &model.RefSelector{Kind: "ref", Ref: table, ServiceID: api.member.ServiceID}
	reverse, err := resolver.Resolve(ctx, input)
	requireFullstack(t, err)
	if len(reverse.Paths) != 2 || reverse.Paths[0].Binding.Client.Site == reverse.Paths[1].Binding.Client.Site {
		t.Fatalf("reverse path lost a frontend occurrence: %+v", reverse)
	}

	readCalls, denied = 0, true
	removed, err := groupReader.Resolve(ctx, authority, groupID)
	requireFullstack(t, err)
	partialRequest := input
	partialRequest.Pinned, partialRequest.Reverse = removed, nil
	partial, err := resolver.Resolve(ctx, partialRequest)
	requireFullstack(t, err)
	if len(partial.Paths) != 0 || !partial.Partial || len(removed.UnavailableMembers) != 1 ||
		removed.UnavailableMembers[0].ServiceID != nil || readCalls != 0 {
		t.Fatalf("unavailable member escaped scope guard: %+v", partial)
	}
	if _, err := resolver.Resolve(ctx, input); err == nil || readCalls != 0 {
		t.Fatalf("old authorized pin reused after deny: %v", err)
	}
	denied = false
	stale := input
	stale.Pinned.Members = append([]model.ScopeMember{}, input.Pinned.Members...)
	for i := range stale.Pinned.Members {
		if *stale.Pinned.Members[i].ServiceID == "orders" {
			stale.Pinned.Members[i].GenerationID = stackUUID(91)
		}
	}
	stale.Pinned.VectorDigest, err = model.ComputeScopeVectorDigest(stale.Pinned, fullstackBytes)
	requireFullstack(t, err)
	if _, err := resolver.Resolve(ctx, stale); err == nil || readCalls != 0 {
		t.Fatalf("expired member was re-resolved against active: %v", err)
	}
	for _, owner := range []*fullstackMember{web, api} {
		requireFullstack(t, owner.checkCapture(ctx, owner.capture))
	}
	if _, err := os.Stat(filepath.Join(web.source, "app", "ran-canary")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("frontend target script canary: %v", err)
	}
	if os.Getenv("MCE4D_T057_PYTHON") != "" && os.Getenv("MCE4D_T057_STDLIB") != "" {
		verifyPublishedLanguageFlow(t, ctx, web, api, graphOwner, route.Handler, frontend.Clients[0].Site, schemaBound.Operations[0].Entity.Key)
	}
	t.Logf("C103/C106: %d forward, %d reverse, SQLite source read; denied and expired reads=0, authored source unchanged",
		len(forward.Paths), len(reverse.Paths))
}
