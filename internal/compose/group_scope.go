package compose

import (
	"context"
	"errors"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

var (
	ErrGroupProfileMissing    = errors.New("group member profile is unavailable")
	ErrGroupMemberUnavailable = errors.New("group member store is unavailable")
)

// GroupScopeResolver coordinates authorized observations of independent
// stores. ReadGroup checks access before returning catalog metadata.
// AuthorizeMember governs both access and disclosure of its identity.
type GroupScopeResolver struct {
	ReadGroup       func(context.Context, app.CorePrincipal, string) (model.GroupSummary, string, error)
	AuthorizeMember func(context.Context, app.CorePrincipal, model.GroupMemberConfig) error
	ReadMember      func(context.Context, app.CorePrincipal, model.GroupMemberConfig, string) (model.ScopeVector, error)
}

func (r GroupScopeResolver) Resolve(ctx context.Context, principal app.CorePrincipal, id string) (model.ScopeVector, error) {
	group, catalogID, err := r.group(ctx, principal, id)
	if err != nil {
		return model.ScopeVector{}, err
	}
	vector := model.ScopeVector{Kind: "federated", GroupID: &id, GroupRevision: &group.Revision, CatalogIncarnation: catalogID,
		Members: []model.ScopeMember{}, UnavailableMembers: []model.MemberUnavailable{}}
	for _, config := range group.Config.Members {
		member, err := r.member(ctx, principal, config, "", catalogID)
		if err == nil {
			vector.Members = append(vector.Members, member)
			continue
		}
		gap, known := groupUnavailable(config, err)
		if !known {
			return model.ScopeVector{}, err
		}
		vector.UnavailableMembers = append(vector.UnavailableMembers, gap)
	}
	if err := r.checkGroup(ctx, principal, group, catalogID); err != nil {
		return model.ScopeVector{}, err
	}
	vector.VectorDigest, err = model.ComputeScopeVectorDigest(vector, 32768)
	if err != nil {
		return model.ScopeVector{}, err
	}
	return model.NormalizeScopeVector(vector, 32768)
}

func (r GroupScopeResolver) Recheck(ctx context.Context, principal app.CorePrincipal, pinned model.ScopeVector) (model.ScopeVector, error) {
	if pinned.Kind != "federated" || model.VerifyScopeVectorDigest(pinned, 32768) != nil {
		return model.ScopeVector{}, ErrScopeInput
	}
	group, catalogID, err := r.group(ctx, principal, *pinned.GroupID)
	if err != nil {
		return model.ScopeVector{}, err
	}
	if catalogID != pinned.CatalogIncarnation || group.Revision != *pinned.GroupRevision ||
		len(group.Config.Members) != len(pinned.Members)+len(pinned.UnavailableMembers) {
		return model.ScopeVector{}, ErrScopeChanged
	}
	remaining := make(map[string]model.GroupMemberConfig, len(group.Config.Members))
	for _, config := range group.Config.Members {
		remaining[config.ServiceID] = config
	}
	for _, expected := range pinned.Members {
		if expected.ServiceID == nil {
			return model.ScopeVector{}, ErrScopeChanged
		}
		cfg, found := remaining[*expected.ServiceID]
		if !found || cfg.WorktreeID != expected.WorktreeID || cfg.ProfileID != expected.ProfileID {
			return model.ScopeVector{}, ErrScopeChanged
		}
		member, err := r.member(ctx, principal, cfg, expected.GenerationID, catalogID)
		if errors.Is(err, index.ErrSnapshotUnavailable) {
			return model.ScopeVector{}, ErrScopeSnapshotExpired
		}
		if err != nil {
			return model.ScopeVector{}, err
		}
		if !reflect.DeepEqual(expected, member) {
			return model.ScopeVector{}, ErrScopeChanged
		}
		delete(remaining, cfg.ServiceID)
	}
	anonymous := 0
	for _, gap := range pinned.UnavailableMembers {
		if gap.ServiceID == nil && gap.WorktreeID == nil && gap.ProfileID == nil &&
			gap.Status == "access_denied" && gap.ReasonCode == "ACCESS_DENIED" {
			anonymous++
			continue
		}
		if gap.ServiceID == nil || gap.WorktreeID == nil || gap.ProfileID == nil ||
			gap.Status == "access_denied" || !validGroupGap(gap) {
			return model.ScopeVector{}, ErrScopeChanged
		}
		cfg, found := remaining[*gap.ServiceID]
		if !found || cfg.WorktreeID != *gap.WorktreeID || cfg.ProfileID != *gap.ProfileID {
			return model.ScopeVector{}, ErrScopeChanged
		}
		if err := r.AuthorizeMember(ctx, principal, cfg); err != nil && !errors.Is(err, ErrGroupMemberUnavailable) {
			return model.ScopeVector{}, err
		}
		delete(remaining, cfg.ServiceID)
	}
	if len(remaining) != anonymous {
		return model.ScopeVector{}, ErrScopeChanged
	}
	if err := r.checkGroup(ctx, principal, group, catalogID); err != nil {
		return model.ScopeVector{}, err
	}
	return model.NormalizeScopeVector(pinned, 32768)
}

func (r GroupScopeResolver) group(ctx context.Context, principal app.CorePrincipal, id string) (model.GroupSummary, string, error) {
	if ctx == nil || r.ReadGroup == nil || r.AuthorizeMember == nil || r.ReadMember == nil || !model.ValidUUID(id) {
		return model.GroupSummary{}, "", ErrScopeInput
	}
	group, catalogID, err := r.ReadGroup(ctx, principal, id)
	if err != nil {
		return model.GroupSummary{}, "", err
	}
	if group.Status != "active" || group.Config.GroupID != id || !model.ValidUUID(catalogID) {
		return model.GroupSummary{}, "", ErrAuthority
	}
	return group, catalogID, ctx.Err()
}

func (r GroupScopeResolver) checkGroup(ctx context.Context, principal app.CorePrincipal, seen model.GroupSummary, catalogID string) error {
	current, incarnation, err := r.group(ctx, principal, seen.Config.GroupID)
	if err != nil {
		return err
	}
	if incarnation != catalogID || !reflect.DeepEqual(current, seen) {
		return ErrScopeChanged
	}
	return ctx.Err()
}

func (r GroupScopeResolver) member(ctx context.Context, principal app.CorePrincipal, cfg model.GroupMemberConfig,
	generation, catalogID string) (model.ScopeMember, error) {
	if err := r.AuthorizeMember(ctx, principal, cfg); err != nil {
		return model.ScopeMember{}, err
	}
	scope, err := r.ReadMember(ctx, principal, cfg, generation)
	if err != nil {
		return model.ScopeMember{}, err
	}
	if scope.Kind != "single" || scope.CatalogIncarnation != catalogID || len(scope.Members) != 1 ||
		len(scope.UnavailableMembers) != 0 || model.VerifyScopeVectorDigest(scope, 32768) != nil {
		return model.ScopeMember{}, ErrScopeChanged
	}
	member := scope.Members[0]
	if member.WorktreeID != cfg.WorktreeID || member.ProfileID != cfg.ProfileID || member.ServiceID != nil ||
		generation != "" && member.GenerationID != generation {
		return model.ScopeMember{}, ErrScopeChanged
	}
	if err := r.AuthorizeMember(ctx, principal, cfg); err != nil {
		return model.ScopeMember{}, err
	}
	member.ServiceID = &cfg.ServiceID
	return member, nil
}

func groupUnavailable(cfg model.GroupMemberConfig, err error) (model.MemberUnavailable, bool) {
	gap := model.MemberUnavailable{ServiceID: &cfg.ServiceID, WorktreeID: &cfg.WorktreeID, ProfileID: &cfg.ProfileID}
	switch {
	case errors.Is(err, ErrAuthority), errors.Is(err, app.ErrCoreAuthentication):
		return model.MemberUnavailable{Status: "access_denied", ReasonCode: "ACCESS_DENIED"}, true
	case errors.Is(err, index.ErrSnapshotUnavailable):
		gap.Status, gap.ReasonCode = "index_missing", "INDEX_MISSING"
	case errors.Is(err, ErrGroupProfileMissing):
		gap.Status, gap.ReasonCode = "profile_missing", "PROFILE_MISSING"
	case errors.Is(err, ErrGroupMemberUnavailable):
		gap.Status, gap.ReasonCode = "unavailable", "STORE_UNAVAILABLE"
	default:
		return model.MemberUnavailable{}, false
	}
	return gap, true
}

func validGroupGap(gap model.MemberUnavailable) bool {
	return gap.Status == "index_missing" && gap.ReasonCode == "INDEX_MISSING" ||
		gap.Status == "profile_missing" && gap.ReasonCode == "PROFILE_MISSING" ||
		gap.Status == "unavailable" && gap.ReasonCode == "STORE_UNAVAILABLE"
}
