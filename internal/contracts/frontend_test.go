package contracts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestFrontendClientMatchesApprovedHandlerScope(t *testing.T) {
	hash := strings.Repeat("a", 64)
	services := []string{"web", "inventory", "billing"}
	generations := []string{worktree, generation, otherGeneration}
	members := make([]model.ScopeMember, 0, 3)
	for i, service := range services {
		members = append(members, model.ScopeMember{WorktreeID: generations[i], ProfileID: hash,
			GenerationID: generations[i], SnapshotDigest: hash, PolicyDigest: hash, PolicyEpoch: 1, BindingRevision: 1,
			IndexIncarnation: generations[i], RecordsIncarnation: generations[i], ServiceID: &service})
	}
	group, revision := worktree, int64(1)
	pinned := model.ScopeVector{Kind: "federated", Members: members, GroupID: &group, GroupRevision: &revision, CatalogIncarnation: worktree,
		UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	pinned.VectorDigest, err = model.ComputeScopeVectorDigest(pinned, 32768)
	if err != nil {
		t.Fatal(err)
	}
	source := func(member model.ScopeMember, path string) model.SourceRef {
		return model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
			Path: path, ContentHash: hash, Range: model.ByteRange{StartByte: 1, EndByte: 5}}
	}
	clientSite := source(members[0], "client.tsx")
	client := FrontendClient{Caller: model.SymbolRef{Kind: "symbol", GraphIdentity: identity(clientSite), SymbolID: "load"},
		Site: clientSite, ServiceID: members[0].ServiceID, Target: Key{ServiceID: members[1].ServiceID, Environment: "fixture",
			Protocol: "http", Namespace: "public-api", Identifier: "GET /items", Version: "v1"}}
	handler := func(member model.ScopeMember) FrontendHandler {
		declaration := source(member, "handler.go")
		contract := source(member, "openapi.json")
		target := client.Target
		target.ServiceID = member.ServiceID
		return FrontendHandler{
			Operation: Entity{Ref: model.EntityRef{Kind: "entity", GraphIdentity: identity(contract), EntityID: "items"}, Kind: "http_operation",
				Key: target, Plane: "intended", Declaration: contract},
			Handler: model.SymbolRef{Kind: "symbol", GraphIdentity: identity(declaration), SymbolID: "serve"}, Declaration: declaration,
		}
	}
	inventory, billing := handler(members[1]), handler(members[2])
	unauthorized := errors.New("fixture authorization changed")
	check := func(_ context.Context, member model.ScopeMember, ref model.SourceRef) error {
		if member.GenerationID != ref.GenerationID || ref.ContentHash != hash {
			return unauthorized
		}
		return nil
	}
	observe := func(context.Context, model.ScopeVector) (model.ScopeVector, error) { return pinned, nil }
	match := func(client FrontendClient, handlers ...FrontendHandler) (FrontendBinding, error) {
		return MatchFrontendHandler(context.Background(), pinned, client, handlers, check, observe)
	}
	binding, err := match(client, billing, inventory)
	if err != nil || binding.Handler.Declaration.WorktreeID != members[1].WorktreeID {
		t.Fatalf("did not preserve service binding: %v", err)
	}
	if _, err := match(client, billing); !errors.Is(err, ErrFrontendBindingUnavailable) {
		t.Fatalf("same path in another service matched: %v", err)
	}
	if _, err := match(client, inventory, inventory); !errors.Is(err, ErrFrontendBindingAmbiguous) {
		t.Fatalf("ambiguous operation matched: %v", err)
	}
	wrong := inventory
	wrong.Operation.Key.Version = "v2"
	if _, err := match(client, wrong); !errors.Is(err, ErrFrontendBindingUnavailable) {
		t.Fatalf("wrong revision matched: %v", err)
	}
	wrong = inventory
	wrong.Declaration.GenerationID = otherGeneration
	if _, err := match(client, wrong); err == nil {
		t.Fatal("handler proof moved between generations")
	}
	wrong = inventory
	wrong.Declaration.ContentHash = strings.Repeat("b", 64)
	if _, err := match(client, wrong); !errors.Is(err, unauthorized) {
		t.Fatalf("source proof not checked: %v", err)
	}
	if _, err := MatchFrontendHandler(context.Background(), pinned, client, []FrontendHandler{inventory}, check,
		func(context.Context, model.ScopeVector) (model.ScopeVector, error) {
			return model.ScopeVector{}, unauthorized
		}); !errors.Is(err, unauthorized) {
		t.Fatalf("scope recheck not applied: %v", err)
	}
}
