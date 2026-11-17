package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
)

func TestModuleLocateKeysetBindsRequestAndScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	member := model.ScopeMember{WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64), GenerationID: "33333333-3333-4333-8333-333333333333", SnapshotDigest: strings.Repeat("b", 64), PolicyDigest: strings.Repeat("c", 64), PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "33333333-3333-4333-8333-333333333333"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "33333333-3333-4333-8333-333333333333", UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	source := func(name string) model.SourceRef {
		return model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: name, ContentHash: strings.Repeat("d", 64), Range: model.ByteRange{StartByte: 0, EndByte: 3}}
	}
	modules := []overview.Module{
		{Ref: model.ModuleRef{Kind: "module", GraphIdentity: identity, ModuleID: strings.Repeat("b", 64)}, Root: "nested", ImportPath: "example.invalid/b", Source: source("nested/go.mod")},
		{Ref: model.ModuleRef{Kind: "module", GraphIdentity: identity, ModuleID: strings.Repeat("a", 64)}, Root: ".", ImportPath: "example.invalid/a", Source: source("go.mod")},
	}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("d", 64), BindingDigest: strings.Repeat("e", 64), OrderVersion: "locate-id-v1", Scope: scope}
	query := "example.invalid"
	request := model.LocateRequest{Mode: "module", Query: &query, Limit: 1}
	options := app.CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "mcp_dual"}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	decode := func(blocks [][]byte) model.Response {
		t.Helper()
		if len(blocks) != 2 || string(blocks[0]) != string(blocks[1]) {
			t.Fatal("invalid dual payload")
		}
		response, err := registry.DecodeResponse(contracts.MCP, "mce4d_locate", blocks[0], 16384, 4)
		if err != nil || model.ValidateDeliveredBytes(response.Budget, blocks) != nil {
			t.Fatalf("invalid module envelope: %v", err)
		}
		return response
	}
	blocks, err := moduleLocatePage(ctx, codec, binding, guard, "fixture", request, options, modules)
	if err != nil {
		t.Fatal(err)
	}
	first := decode(blocks)
	candidates := first.Data.(model.LocateData).Candidates
	if len(candidates) != 1 || candidates[0].Ref.(*model.ModuleRef).ModuleID != strings.Repeat("a", 64) || first.Pagination.NextCursor == nil {
		t.Fatal("module keyset did not order the first page")
	}
	claims, err := codec.VerifyPrincipal(ctx, *first.Pagination.NextCursor, "list", binding.PrincipalDigest, guard)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor, request.Limit = first.Pagination.NextCursor, 10
	blocks, err = moduleLocatePage(ctx, codec, claims.Binding, guard, "fixture", request, options, modules)
	if err != nil {
		t.Fatal(err)
	}
	second := decode(blocks)
	candidates = second.Data.(model.LocateData).Candidates
	if len(candidates) != 1 || candidates[0].Ref.(*model.ModuleRef).ModuleID != strings.Repeat("b", 64) || second.Pagination.NextCursor != nil {
		t.Fatal("module keyset omitted or repeated a result")
	}
	other := "different"
	request.Query = &other
	if _, err := moduleLocatePage(ctx, codec, claims.Binding, guard, "fixture", request, options, modules); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("different query reused module cursor: %v", err)
	}
	request.Query = &query
	wrongGeneration := "44444444-4444-4444-8444-444444444444"
	request.Scope = &model.ScopeRequest{GenerationID: &wrongGeneration}
	if _, err := moduleLocatePage(ctx, codec, claims.Binding, guard, "fixture", request, options, modules); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("different scope reused module cursor: %v", err)
	}
	request.Scope = nil
	tampered := *first.Pagination.NextCursor + "x"
	request.Cursor = &tampered
	if blocks, err := moduleLocatePage(ctx, codec, claims.Binding, guard, "fixture", request, options, modules); err == nil || len(blocks) != 0 {
		t.Fatal("tampered module cursor was accepted")
	}
	request.Cursor = nil
	if blocks, err := moduleLocatePage(ctx, codec, binding, guard, "fixture", request, options, []overview.Module{modules[0], modules[0]}); !errors.Is(err, app.ErrCoreBackend) || len(blocks) != 0 {
		t.Fatalf("duplicate module IDs were accepted: %v", err)
	}
	revoked := func(context.Context, contextpack.CursorBinding) error { return errors.New("revoked") }
	if blocks, err := moduleLocatePage(ctx, codec, binding, revoked, "fixture", request, options, modules); err == nil || len(blocks) != 0 {
		t.Fatal("revoked principal received module metadata")
	}
	many := make([]overview.Module, 100)
	for i := range many {
		many[i] = modules[0]
		many[i].Ref.ModuleID = fmt.Sprintf("%064x", i+1)
	}
	request.Limit = 100
	options.MaxOutputBytes = 8192
	if _, err := moduleLocatePage(ctx, codec, binding, guard, "fixture", request, options, many); !errors.Is(err, contextpack.ErrBudgetTooSmall) {
		t.Fatalf("undersized dual envelope was accepted: %v", err)
	}
	options.MaxOutputBytes = 16384
	blocks, err = moduleLocatePage(ctx, codec, binding, guard, "fixture", request, options, many)
	if err != nil {
		t.Fatalf("module result did not adapt to wire budget: %v", err)
	}
	bounded := decode(blocks)
	if len(bounded.Data.(model.LocateData).Candidates) < 1 || len(bounded.Data.(model.LocateData).Candidates) >= len(many) || bounded.Pagination.NextCursor == nil || bounded.Budget.SerializedBytes > options.MaxOutputBytes {
		t.Fatal("module page exceeded budget or lost continuation")
	}
}
