//go:build linux

package tsprovider

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	domain "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestFrontendWorkerBindsCapturedClientToApprovedService(t *testing.T) {
	files := map[string]string{
		"app/tsconfig.json": `{"compilerOptions":{"target":"es2022","module":"esnext","moduleResolution":"bundler","jsx":"preserve","types":[],"lib":["es2022","dom"]},"files":["routes.tsx","router.d.ts"]}`,
		"app/router.d.ts": `declare module "react-router-dom" { export function Route(value: object): JSX.Element; export function Routes(value: object): JSX.Element; }
declare namespace JSX { interface Element {} interface IntrinsicElements { main: {} } }`,
		"app/routes.tsx": `import { Route, Routes } from "react-router-dom";
function Page() { return <main />; }
export const Screen = <Routes><Route path="/items" element={<Page />} /></Routes>;
export function load() { return fetch("/api/items"); }`,
	}
	config, job, captured := workerFixture(t, "typed_when_available", files, frontendProgram())
	provider, profile, err := NewFrontend(Installation{
		Node: config.NodePath, ScratchParent: config.ScratchParent, CompilerRoot: config.Compiler.Root,
		CompilerArchive: config.Compiler.ArchivePath, CompilerManifest: config.Compiler.ManifestPath,
	}, []string{"app/tsconfig.json"}, "typed_when_available")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile, job.Start.Profile) {
		t.Fatal("frontend profile does not bind the combined worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	job.Start.DeadlineAt = deadline.UTC().Format(time.RFC3339Nano)
	result, err := provider.Scan(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Routes) != 1 || len(result.Clients) != 1 || len(result.Gaps) != 0 {
		t.Fatalf("unexpected frontend output: %+v", result)
	}
	route, call := result.Routes[0], result.Clients[0]
	if route.Path != "/items" || route.Component.Path != "app/routes.tsx" ||
		files[route.Component.Path][route.Component.Range.StartByte:route.Component.Range.EndByte] != "Page" {
		t.Fatal("route/component proof does not match the capture")
	}
	var caller string
	_, err = provider.Analyze(ctx, job, func(_ context.Context, batch model.ProviderBatch) error {
		for _, symbol := range batch.Symbols {
			if symbol.Name == "load" && symbol.Path != nil && *symbol.Path == call.Site.Path && symbol.Kind == "function" {
				caller = symbol.SymbolID
			}
		}
		return nil
	})
	if err != nil || caller == "" {
		t.Fatalf("combined program could not emit the client symbol: %v", err)
	}
	service := "web"
	const otherProfile = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	member := model.ScopeMember{WorktreeID: call.Site.WorktreeID, ProfileID: call.Site.ProfileID, GenerationID: call.Site.GenerationID,
		SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: captured.Manifest().PolicyDigest, PolicyEpoch: 1, BindingRevision: 1,
		IndexIncarnation: call.Site.WorktreeID, RecordsIncarnation: call.Site.WorktreeID, ServiceID: &service}
	makeMember := func(id, scope string) model.ScopeMember {
		output := member
		output.WorktreeID, output.GenerationID, output.ProfileID, output.ServiceID = id, id, otherProfile, &scope
		return output
	}
	catalog := makeMember("44444444-4444-4444-8444-444444444444", "catalog")
	billing := makeMember("55555555-5555-4555-8555-555555555555", "billing")
	group, revision := member.WorktreeID, int64(1)
	pinned := model.ScopeVector{Kind: "federated", Members: []model.ScopeMember{member, catalog, billing}, GroupID: &group,
		GroupRevision: &revision, CatalogIncarnation: group, UnavailableMembers: []model.MemberUnavailable{}}
	pinned.VectorDigest, err = model.ComputeScopeVectorDigest(pinned, 32768)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.Key{ServiceID: catalog.ServiceID, Environment: "fixture", Protocol: "http", Namespace: "public",
		Identifier: call.Method + " " + call.Path, Version: "v1"}
	httpSource, err := snapshot.NewFile("handler.go", []byte("func serve() {}\n"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	makeHandler := func(member model.ScopeMember) domain.FrontendHandler {
		id := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
		declaration := model.SourceRef{WorktreeID: id.WorktreeID, ProfileID: id.ProfileID, GenerationID: id.GenerationID,
			Path: httpSource.Path(), ContentHash: httpSource.ContentHash(), Range: model.ByteRange{StartByte: 5, EndByte: 10}}
		operation := domain.Entity{Ref: model.EntityRef{Kind: "entity", GraphIdentity: id, EntityID: "items"},
			Kind: "http_operation", Key: key, Plane: "observed_static", Declaration: declaration}
		operation.Key.ServiceID = member.ServiceID
		return domain.FrontendHandler{Operation: operation, Handler: model.SymbolRef{Kind: "symbol", GraphIdentity: id, SymbolID: "serve"},
			Declaration: declaration}
	}
	client := domain.FrontendClient{Caller: model.SymbolRef{Kind: "symbol", SymbolID: caller,
		GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}},
		Site: call.Site, ServiceID: member.ServiceID, Target: key}
	bound, err := domain.MatchFrontendHandler(ctx, pinned, client, []domain.FrontendHandler{makeHandler(billing), makeHandler(catalog)},
		func(_ context.Context, observed model.ScopeMember, source model.SourceRef) error {
			file := httpSource
			if observed.WorktreeID == member.WorktreeID {
				existing, ok := captured.File(source.Path)
				if !ok {
					return domain.ErrDomainRecord
				}
				file = existing
			}
			if file.ContentHash() != source.ContentHash || file.Path() != source.Path {
				return domain.ErrDomainRecord
			}
			return file.CheckRange(int(source.Range.StartByte), int(source.Range.EndByte))
		},
		func(context.Context, model.ScopeVector) (model.ScopeVector, error) { return pinned, nil })
	if err != nil || bound.Handler.Handler.WorktreeID != catalog.WorktreeID ||
		!strings.Contains(files[call.Site.Path][call.Site.Range.StartByte:call.Site.Range.EndByte], "/api/items") {
		t.Fatalf("no exact source-bound C056 link: %+v %v", bound, err)
	}
}
