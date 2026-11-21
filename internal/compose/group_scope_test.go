package compose

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func TestGroupScopeKeepsExactServiceAndUnavailableMembers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const groupID = "b1111111-1111-4111-8111-111111111111"
	const catalogID = "c1111111-1111-4111-8111-111111111111"
	const gen1 = "a1111111-1111-4111-8111-111111111111"
	const gen2 = "a2222222-2222-4222-8222-222222222222"
	const worktree = "d1111111-1111-4111-8111-111111111111"
	const missingWorktree = "e1111111-1111-4111-8111-111111111111"
	const deniedWorktree = "f1111111-1111-4111-8111-111111111111"
	profile := strings.Repeat("1", 64)
	config := model.GroupConfig{GroupID: groupID, Name: "test group", Members: []model.GroupMemberConfig{
		{ServiceID: "a", WorktreeID: worktree, ProfileID: profile},
		{ServiceID: "b", WorktreeID: worktree, ProfileID: profile},
		{ServiceID: "missing", WorktreeID: missingWorktree, ProfileID: profile},
		{ServiceID: "denied", WorktreeID: deniedWorktree, ProfileID: profile},
	}}
	actor := app.CorePrincipal{ID: "test"}
	group := model.GroupSummary{Config: config, Status: "active", Revision: 1}
	active := gen1
	missing := true
	resolver := GroupScopeResolver{
		ReadGroup: func(_ context.Context, principal app.CorePrincipal, id string) (model.GroupSummary, string, error) {
			if principal != actor || id != groupID {
				return model.GroupSummary{}, "", ErrAuthority
			}
			return group, catalogID, nil
		},
		AuthorizeMember: func(_ context.Context, _ app.CorePrincipal, member model.GroupMemberConfig) error {
			if member.WorktreeID == deniedWorktree {
				return ErrAuthority
			}
			return nil
		},
		ReadMember: func(_ context.Context, _ app.CorePrincipal, member model.GroupMemberConfig, generation string) (model.ScopeVector, error) {
			if member.WorktreeID == deniedWorktree {
				t.Fatal("denied member was opened")
			}
			if member.WorktreeID == missingWorktree && missing {
				return model.ScopeVector{}, index.ErrSnapshotUnavailable
			}
			if generation == "" {
				generation = active
			}
			if generation != gen1 && generation != gen2 {
				return model.ScopeVector{}, index.ErrSnapshotUnavailable
			}
			fact := model.ScopeMember{WorktreeID: member.WorktreeID, ProfileID: profile, GenerationID: generation,
				IndexIncarnation: gen1, RecordsIncarnation: gen2, SnapshotDigest: strings.Repeat("2", 64),
				PolicyDigest: strings.Repeat("3", 64), PolicyEpoch: 1, BindingRevision: 1}
			single := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{fact},
				CatalogIncarnation: catalogID, UnavailableMembers: []model.MemberUnavailable{}}
			digest, err := model.ComputeScopeVectorDigest(single, 32768)
			single.VectorDigest = digest
			return single, err
		},
	}
	if _, err := resolver.Resolve(ctx, app.CorePrincipal{}, groupID); !errors.Is(err, ErrAuthority) {
		t.Fatalf("unauthorized group: %v", err)
	}
	first, err := resolver.Resolve(ctx, actor, groupID)
	if err != nil || len(first.Members) != 2 || len(first.UnavailableMembers) != 2 {
		t.Fatalf("initial vector: %+v %v", first, err)
	}
	for _, gap := range first.UnavailableMembers {
		if gap.Status == "access_denied" && (gap.ServiceID != nil || gap.WorktreeID != nil || gap.ProfileID != nil) {
			t.Fatalf("denied identity disclosed: %+v", gap)
		}
	}
	member := first.Members[0]
	ref := &model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: worktree,
		ProfileID: profile, GenerationID: gen1}, SymbolID: strings.Repeat("4", 64)}
	if _, err := model.MatchPinnedMember(first, ref, nil); err == nil {
		t.Fatal("unqualified service accepted")
	}
	if found, err := model.MatchPinnedMember(first, ref, member.ServiceID); err != nil || found != member {
		t.Fatalf("exact service: %+v %v", found, err)
	}
	active, missing = gen2, false
	pinned, err := resolver.Recheck(ctx, actor, first)
	if err != nil || pinned.VectorDigest != first.VectorDigest || pinned.Members[0].GenerationID != gen1 ||
		len(pinned.UnavailableMembers) != 2 {
		t.Fatalf("pinned vector silently advanced: %+v %v", pinned, err)
	}
	live, err := resolver.Resolve(ctx, actor, groupID)
	if err != nil || len(live.Members) != 3 || len(live.UnavailableMembers) != 1 || live.VectorDigest == first.VectorDigest {
		t.Fatalf("live reopen: %+v %v", live, err)
	}
	group.Revision++
	if _, err := resolver.Recheck(ctx, actor, first); !errors.Is(err, ErrScopeChanged) {
		t.Fatalf("old group revision accepted: %v", err)
	}
}
