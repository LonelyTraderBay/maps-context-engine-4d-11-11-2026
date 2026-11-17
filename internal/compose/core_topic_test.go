package compose

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
)

func TestTopicResultRecipeAndKeyset(t *testing.T) {
	if digest, err := topicResultDigest("vocab.json", strings.Repeat("f", 64), "x", overview.AliasResult{Candidates: []overview.AliasCandidate{}, Ambiguous: false}); err != nil || digest != "e400f13577743f7cfeb128969503338a84d9ad1ee73446c3cc323fab1ab436a7" {
		t.Fatalf("topic recipe differs from Python reference: %s %v", digest, err)
	}
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
	ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: "spec.md", ContentHash: strings.Repeat("d", 64), Range: model.ByteRange{StartByte: 0, EndByte: 6}}
	result := overview.AliasResult{Ambiguous: true, Candidates: []overview.AliasCandidate{{Entity: model.EntityRef{Kind: "entity", GraphIdentity: identity, EntityID: "balance"}, SourceRefs: []model.SourceRef{ref}}, {Entity: model.EntityRef{Kind: "entity", GraphIdentity: identity, EntityID: "credit"}, SourceRefs: []model.SourceRef{ref}}}}
	query := "số dư"
	material := &topicMaterial{query: query, configPath: "vocab.json", configHash: strings.Repeat("f", 64), result: result}
	material.digest, err = topicResultDigest(material.configPath, material.configHash, query, result)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("d", 64), BindingDigest: strings.Repeat("e", 64), OrderVersion: "locate-id-v1", Scope: scope}
	request := model.LocateRequest{Mode: "topic", Query: &query, Limit: 1}
	options := app.CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "mcp_dual"}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	decode := func(blocks [][]byte) model.Response {
		t.Helper()
		if len(blocks) != 2 || string(blocks[0]) != string(blocks[1]) {
			t.Fatal("invalid topic delivery")
		}
		response, err := registry.DecodeResponse(contracts.MCP, "mce4d_locate", blocks[0], 16384, 4)
		if err != nil || model.ValidateDeliveredBytes(response.Budget, blocks) != nil {
			t.Fatalf("invalid topic envelope: %v", err)
		}
		return response
	}
	blocks, err := topicLocatePage(ctx, codec, binding, guard, "fixture", request, options, material)
	if err != nil {
		t.Fatal(err)
	}
	first := decode(blocks)
	if first.Pagination.NextCursor == nil || len(first.Data.(model.LocateData).Candidates) != 1 || first.Data.(model.LocateData).Candidates[0].Ref.(*model.EntityRef).EntityID != "balance" || len(first.Warnings) != 1 || first.Warnings[0].Code != "AMBIGUOUS_TOPIC" {
		t.Fatal("topic page lost ambiguity, identity or continuation")
	}
	principal := app.CorePrincipal{ID: "fixture", Digest: binding.PrincipalDigest, BindingDigest: binding.BindingDigest}
	if err := verifyTopicPage(ctx, codec, guard, principal, request, first, material); err != nil {
		t.Fatalf("valid first topic page rejected: %v", err)
	}
	forged := first
	forgedData := first.Data.(model.LocateData)
	forgedData.Candidates = append([]model.Candidate{}, forgedData.Candidates...)
	forgedData.Candidates[0].QualifiedName = "forged"
	forged.Data = forgedData
	if err := verifyTopicPage(ctx, codec, guard, principal, request, forged, material); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("forged candidate accepted: %v", err)
	}
	forged = first
	wrongPending := int64(0)
	forged.Pagination.OptionalPending = &wrongPending
	if err := verifyTopicPage(ctx, codec, guard, principal, request, forged, material); !errors.Is(err, app.ErrCoreBackend) {
		t.Fatalf("incorrect pending count accepted: %v", err)
	}
	forged = first
	forged.Warnings = []model.Gap{}
	if err := verifyTopicPage(ctx, codec, guard, principal, request, forged, material); !errors.Is(err, app.ErrCoreBackend) {
		t.Fatalf("missing ambiguity warning accepted: %v", err)
	}
	wrongGeneration := "22222222-2222-4222-8222-222222222222"
	wrongScope := request
	wrongScope.Scope = &model.ScopeRequest{GenerationID: &wrongGeneration}
	if err := verifyTopicPage(ctx, codec, guard, principal, wrongScope, first, material); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("different generation accepted: %v", err)
	}
	claims, err := codec.VerifyPrincipal(ctx, *first.Pagination.NextCursor, "list", binding.PrincipalDigest, guard)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor, request.Limit = first.Pagination.NextCursor, 10
	blocks, err = topicLocatePage(ctx, codec, claims.Binding, guard, "fixture", request, options, material)
	if err != nil {
		t.Fatal(err)
	}
	second := decode(blocks)
	if second.Pagination.NextCursor != nil || len(second.Data.(model.LocateData).Candidates) != 1 || second.Data.(model.LocateData).Candidates[0].Ref.(*model.EntityRef).EntityID != "credit" || len(second.Warnings) != 1 {
		t.Fatal("topic continuation lost a candidate or ambiguity")
	}
	if err := verifyTopicPage(ctx, codec, guard, principal, request, second, material); err != nil {
		t.Fatalf("valid topic continuation rejected: %v", err)
	}
	changed := *material
	changed.configHash = strings.Repeat("0", 64)
	changed.digest, err = topicResultDigest(changed.configPath, changed.configHash, query, result)
	if err != nil {
		t.Fatal(err)
	}
	if blocks, err := topicLocatePage(ctx, codec, claims.Binding, guard, "fixture", request, options, &changed); !errors.Is(err, contextpack.ErrCursorInvalid) || len(blocks) != 0 {
		t.Fatalf("changed config reused a topic cursor: %v", err)
	}
	bad := *first.Pagination.NextCursor + "x"
	request.Cursor = &bad
	if blocks, err := topicLocatePage(ctx, codec, claims.Binding, guard, "fixture", request, options, material); err == nil || len(blocks) != 0 {
		t.Fatal("tampered topic cursor was accepted")
	}
}
