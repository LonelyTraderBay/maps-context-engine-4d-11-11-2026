//go:build linux

package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/events"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/federation"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/python-provider"
)

const ordersWorkerBinding = `{"producer":"PlaceOrder","consumer_service":"worker","consumer":"consume","address":"orders.created","version":"v1"}`

const authoredWorkerContract = `{
  "asyncapi":"3.0.0","info":{"title":"Fixture receiver","version":"v1"},
  "servers":{"events":{"host":"orders.invalid:5672","protocol":"amqp","protocolVersion":"0.9.1","pathname":"/fixture"}},
  "defaultContentType":"application/json",
  "channels":{"received":{"address":"orders.created","servers":[{"$ref":"#/servers/events"}],
    "messages":{"received":{"$ref":"#/components/messages/received"}}}},
  "operations":{"receiveOrder":{"action":"receive","channel":{"$ref":"#/channels/received"}}},
  "components":{"messages":{"received":{"x-mce4d-schema-version":"v1","payload":{"type":"object","properties":{"id":{"type":"string"}}}}}}
}`

func publishedPythonWorker(t *testing.T, ctx context.Context) *fullstackMember {
	t.Helper()
	root := t.TempDir()
	process, profile, err := pythonprovider.NewTyped(pythonprovider.Installation{
		Interpreter: os.Getenv("MCE4D_T057_PYTHON"), Stdlib: os.Getenv("MCE4D_T057_STDLIB"), ScratchParent: root}, []string{"app"})
	requireFullstack(t, err)
	files := map[string]string{
		"app/worker.py":     "from pathlib import Path\nPath('ran-worker').touch()\ndef consume(event):\n    return event\n",
		"app/asyncapi.json": authoredWorkerContract,
	}
	m := makeFullstackMember(t, ctx, root, "worker", stackUUID(3), stackUUID(13), stackUUID(23), profile, profile.ProviderDigest, []string{}, files)
	raw, err := json.Marshal(m.capture.Manifest())
	requireFullstack(t, err)
	manifest := filepath.Join(m.source, "manifest.json")
	requireFullstack(t, os.WriteFile(manifest, raw, 0600))
	deadline, _ := ctx.Deadline()
	job := ports.ProviderJob{Start: model.ProviderStart{ProviderProtocol: 1, Type: "analyze_start",
		JobID: m.job, GenerationID: m.member.GenerationID, ProviderID: pythonprovider.ProviderID, ProviderDigest: profile.ProviderDigest,
		Profile: profile, ProfileID: m.member.ProfileID, SnapshotRoot: m.source, ManifestPath: manifest, ManifestDigest: bufferSHA(raw),
		MaxFrameBytes: fullstackBytes, DeadlineAt: deadline.UTC().Format(time.RFC3339Nano)}, Capture: providers.SnapshotInput(m.capture),
		Recheck: func(ctx context.Context) error { return m.checkCapture(ctx, m.capture) }}
	m.publish(t, ctx, func(sink func(context.Context, model.ProviderBatch) error) (model.ProviderDone, error) {
		return process.Analyze(ctx, job, sink)
	})
	return m
}

func verifyPublishedLanguageFlow(t *testing.T, ctx context.Context, web, api *fullstackMember, webSymbol, apiSymbol model.SymbolRef,
	webSite model.SourceRef, apiKey contracts.Key) {
	t.Helper()
	worker := publishedPythonWorker(t, ctx)
	groupID, catalogID := stackUUID(41), stackUUID(42)
	members := map[string]*fullstackMember{"web": web, "orders": api, "worker": worker}
	group := model.GroupSummary{Status: "active", Revision: 1, Config: model.GroupConfig{GroupID: groupID, Name: "Language fixture"}}
	for _, item := range []struct {
		service, protocol string
	}{{"web", "http"}, {"orders", "http"}, {"worker", "event"}} {
		member := members[item.service].member
		group.Config.Members = append(group.Config.Members, model.GroupMemberConfig{ServiceID: item.service,
			WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, Namespace: model.ServiceNamespace{
				Protocol: item.protocol, Environment: "local", ContractVersion: pointer("v1")}})
	}
	actor := app.CorePrincipal{ID: "fixture-language-owner"}
	workerDenied := false
	readGroup := func(ctx context.Context, key string) (model.GroupSummary, string, error) {
		if key != groupID {
			return model.GroupSummary{}, "", compose.ErrAuthority
		}
		return group, catalogID, ctx.Err()
	}
	authority := compose.GroupScopeResolver{
		ReadGroup: func(ctx context.Context, p app.CorePrincipal, key string) (model.GroupSummary, string, error) {
			if p != actor {
				return model.GroupSummary{}, "", compose.ErrAuthority
			}
			return readGroup(ctx, key)
		},
		AuthorizeMember: func(ctx context.Context, p app.CorePrincipal, cfg model.GroupMemberConfig) error {
			if p != actor || members[cfg.ServiceID] == nil || workerDenied && cfg.ServiceID == "worker" {
				return compose.ErrAuthority
			}
			return ctx.Err()
		},
		ReadMember: func(ctx context.Context, _ app.CorePrincipal, config model.GroupMemberConfig, generation string) (model.ScopeVector, error) {
			owner := members[config.ServiceID]
			seen, err := owner.facts.ReadGeneration(ctx, config.ProfileID, generation, owner.authorizeGeneration, fullstackBytes)
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
	pinned, err := authority.Resolve(ctx, actor, groupID)
	requireFullstack(t, err)
	graph, err := worker.facts.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: worker.member,
		VectorDigest: pinned.VectorDigest, AuthorizeGeneration: worker.authorizeGeneration, AuthorizePath: worker.authorizePath,
		Scanner: worker.scanner, MaxManifestBytes: fullstackBytes, MaxFileBytes: fullstackBytes,
		MaxSourceBytes: fullstackBytes, MaxFactBytes: fullstackBytes, MaxFacts: 100})
	if err != nil {
		t.Fatalf("read published Python provider facts: %v", err)
	}
	var workerSymbol model.SymbolRef
	var workerSite model.SourceRef
	for _, symbol := range graph.Symbols {
		if symbol.QualifiedName == "worker.consume" && symbol.Kind == "function" && symbol.Path != nil && symbol.Range != nil {
			workerSite = worker.sourceRef(*symbol.Path)
			workerSite.Range = *symbol.Range
			workerSymbol = model.SymbolRef{Kind: "symbol", SymbolID: symbol.SymbolID, GraphIdentity: model.GraphIdentity{
				WorktreeID: workerSite.WorktreeID, ProfileID: workerSite.ProfileID, GenerationID: workerSite.GenerationID}}
		}
	}
	if workerSymbol.SymbolID == "" {
		t.Fatal("published Python owner missed the receiver")
	}
	contractSite := worker.sourceRef("app/asyncapi.json")
	contractRaw, err := worker.readSource(ctx, contractSite)
	requireFullstack(t, err)
	event, err := events.AnalyzeCaptured(ctx, schema.Profile{ServiceID: "worker", Environment: "local", Namespace: "orders"},
		[]events.Capture{{Ref: contractSite, Bytes: []byte(contractRaw), Kind: "asyncapi", Version: "v1"}}, nil, nil)
	requireFullstack(t, err)
	if len(event.Operations) != 1 || !event.Operations[0].Bindable || event.Operations[0].Action != "receive" {
		t.Fatalf("no exact receiver contract: %+v", event.Coverage)
	}
	bindingSite := api.sourceRef("event-bindings.json")
	bindingRaw, err := api.readSource(ctx, bindingSite)
	requireFullstack(t, err)
	var binding struct {
		Producer, Consumer, Address, Version string
		Service                              string `json:"consumer_service"`
	}
	requireFullstack(t, json.Unmarshal([]byte(bindingRaw), &binding))
	if binding.Producer != "PlaceOrder" || binding.Consumer != "consume" || binding.Service != "worker" ||
		binding.Address != event.Operations[0].Address || binding.Version != event.Operations[0].Entity.Key.Version {
		t.Fatalf("explicit declaration did not match receiver: %+v", binding)
	}
	webKey := contracts.Key{ServiceID: web.member.ServiceID, Environment: "local", Protocol: "frontend",
		Namespace: "orders", Identifier: "POST /backend/orders", Version: "v1"}
	toEndpoint := func(ref model.GraphRef, key contracts.Key) federation.ContractEndpoint {
		return federation.ContractEndpoint{Selector: model.RefSelector{Kind: "ref", Ref: ref, ServiceID: key.ServiceID}, Contract: key}
	}
	from, handler, target := toEndpoint(&webSymbol, webKey), toEndpoint(&apiSymbol, apiKey), toEndpoint(&workerSymbol, event.Operations[0].Entity.Key)
	proofs := map[string]federation.FlowEndpointEvidence{
		"web":    {Ref: &webSymbol, Contract: webKey, Declaration: webSite},
		"orders": {Ref: &apiSymbol, Contract: apiKey, Declaration: api.sourceRef("api.json")},
		"worker": {Ref: &workerSymbol, Contract: target.Contract, Declaration: workerSite},
	}
	workerReads := 0
	resolver := federation.FlowResolver{
		Resolver: federation.Resolver{
			ReadGroup: readGroup,
			Observe: func(ctx context.Context, seen model.ScopeVector) (model.ScopeVector, error) {
				return authority.Recheck(ctx, actor, seen)
			},
			CheckSource: func(ctx context.Context, member model.ScopeMember, ref model.SourceRef) error {
				if *member.ServiceID == "worker" {
					workerReads++
				}
				_, err := members[*member.ServiceID].readSource(ctx, ref)
				return err
			},
		},
		ReadEndpoint: func(_ context.Context, member model.ScopeMember, ref model.GraphRef) (federation.FlowEndpointEvidence, error) {
			proof := proofs[*member.ServiceID]
			if !reflect.DeepEqual(proof.Ref, ref) {
				return federation.FlowEndpointEvidence{}, federation.ErrFlowEndpointUnavailable
			}
			return proof, nil
		},
		ReadLinks: func(ctx context.Context, _ model.ScopeVector) ([]federation.DeclaredLink, error) {
			return []federation.DeclaredLink{
				{ID: "web-to-handler", From: from, To: handler, Declaration: web.sourceRef("app/proxy.json")},
				{ID: "handler-to-worker", From: handler, To: target, Declaration: bindingSite},
			}, ctx.Err()
		},
	}
	request := federation.FlowRequest{Pinned: pinned, Root: from.Selector, Direction: "forward", MaxDepth: 4, Limit: 20}
	flow, err := resolver.ResolveFlow(ctx, request)
	requireFullstack(t, err)
	if len(flow.Links) != 2 || flow.Links[1].ToSource != workerSite || workerReads == 0 {
		t.Fatalf("published forward chain: %+v", flow)
	}
	request.Root, request.Direction = target.Selector, "reverse"
	reverse, err := resolver.ResolveFlow(ctx, request)
	requireFullstack(t, err)
	if len(reverse.Links) != 2 || reverse.Links[1].FromSource != webSite {
		t.Fatalf("published reverse chain: %+v", reverse)
	}
	workerDenied, workerReads = true, 0
	denied, err := authority.Resolve(ctx, actor, groupID)
	requireFullstack(t, err)
	request.Root, request.Direction, request.Pinned = from.Selector, "forward", denied
	partial, err := resolver.ResolveFlow(ctx, request)
	requireFullstack(t, err)
	if len(partial.Links) != 1 || !partial.Partial || workerReads != 0 {
		t.Fatalf("denied receiver source read: %+v", partial)
	}
	requireFullstack(t, worker.checkCapture(ctx, worker.capture))
	if _, err := os.Stat(filepath.Join(worker.source, "ran-worker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worker target canary: %v", err)
	}
	t.Log("C063: verified TypeScript → Go/OpenAPI → Python/AsyncAPI owner flow across three published SQLite captures; reverse and denied member checked")
}
