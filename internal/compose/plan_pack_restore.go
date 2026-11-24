package compose

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// PlanRestoreFactory reopens the pinned catalog and the current complete
// declaration set. It validates facts and authority, never reselects a plan.
// Its caller must supply an independently authenticated original request.
func (b *CoreBackend) PlanRestoreFactory(principal app.CorePrincipal) (app.PlanRestoreFactory, error) {
	return b.planRestoreFactory(principal, nil)
}

func (b *CoreBackend) planRestoreFactory(principal app.CorePrincipal, captured *index.PublishedGraph) (app.PlanRestoreFactory, error) {
	if !b.valid() || b.planSource == nil || principal.ID == "" {
		return nil, app.ErrCoreUnsupported
	}
	return func(ctx context.Context, binding contextpack.CursorBinding, request model.PackRequest, revisionDigest string) (app.FreshnessPlanRestoreInput, error) {
		var empty app.FreshnessPlanRestoreInput
		if ctx == nil || !declarationRevisionValid(revisionDigest) {
			return empty, app.ErrCoreInput
		}
		rawRequest, err := json.Marshal(request)
		if err != nil || len(rawRequest) > 65536 {
			return empty, app.ErrCoreInput
		}
		requestCopy, err := model.DecodePackRequestJSON(rawRequest, 65536)
		if err != nil || requestCopy.View == nil || *requestCopy.View != "overview" || requestCopy.TaskKind != "plan" && requestCopy.TaskKind != "explore" {
			return empty, app.ErrCoreInput
		}
		digest, err := model.RequestDigest("context_pack", principal.ID, "PackRequest", requestCopy, 65536, 4)
		if err != nil || digest != binding.RequestDigest {
			return empty, ErrAuthority
		}
		rawScope, err := json.Marshal(binding.Scope)
		if err != nil || len(rawScope) > 32768 {
			return empty, app.ErrCoreInput
		}
		pinned, err := model.DecodeScopeVectorJSON(rawScope, 32768)
		if err != nil || pinned.Kind != "single" || len(pinned.Members) != 1 || len(pinned.UnavailableMembers) != 0 {
			return empty, ErrScopeChanged
		}
		var graph *index.PublishedGraph
		symbolFactory, err := b.symbolRestoreFactory(principal, captured, func(value *index.PublishedGraph) { graph = value })
		if err != nil {
			return empty, err
		}
		base, err := symbolFactory(ctx, binding)
		if err != nil || graph == nil {
			if err != nil {
				return empty, err
			}
			return empty, app.ErrCoreBackend
		}
		member := pinned.Members[0]
		if err := graph.VerifyCatalog(ctx, member); err != nil {
			return empty, coreStoreError(err)
		}
		_, _, _, expectedFacts, err := projectOverviewFacts(ctx, graph)
		if err != nil {
			return empty, err
		}
		// The provider receives independent copies of the pinned scope and
		// authenticated request, so it cannot change the values retained here.
		providerScope, err := model.DecodeScopeVectorJSON(rawScope, 32768)
		if err != nil {
			return empty, app.ErrCoreInput
		}
		declarations, err := b.planSource(ctx, principal, providerScope, requestCopy)
		if err != nil {
			return empty, coreStoreError(err)
		}
		if len(declarations.Items) > 512 || declarations.Recheck == nil || declarations.AuthorizeDocument == nil || declarations.RetainedBytes < 0 || declarations.RetainedBytes > 32*1024*1024 || declarations.RevisionDigest != revisionDigest {
			return empty, app.ErrCoreInput
		}
		items := append([]overview.DocumentDeclaration{}, declarations.Items...)
		keys, err := declarationMultiset(items, member)
		if err != nil {
			return empty, err
		}
		recheckDeclarations, authorizeDocument := declarations.Recheck, declarations.AuthorizeDocument
		declarations = PlanDeclarations{}
		ownerGuard := b.planAuthorityGuard(principal, pinned, binding.RequestDigest, recheckDeclarations)
		if err := ownerGuard(ctx, binding); err != nil {
			return empty, err
		}
		expectedScope := overview.Scope{Identity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SnapshotDigest: member.SnapshotDigest, PolicyDigest: member.PolicyDigest, PolicyEpoch: member.PolicyEpoch}
		scopeAuth := func(ctx context.Context, scope overview.Scope) error {
			if scope != expectedScope {
				return ErrScopeChanged
			}
			return base.Pack.AuthorizeScope(ctx, pinned, member)
		}
		pathAuth := func(ctx context.Context, scope overview.Scope, name string) error {
			if scope != expectedScope {
				return ErrScopeChanged
			}
			return base.Pack.AuthorizePath(ctx, member, name)
		}
		verifyFacts := func(ctx context.Context, scope overview.Scope, actual string) error {
			if actual != expectedFacts || scope != expectedScope {
				return ErrScopeChanged
			}
			return graph.VerifyCatalog(ctx, member)
		}
		verifyDeclarations := func(ctx context.Context, scope overview.Scope, actual []overview.DocumentDeclaration) error {
			if scope != expectedScope {
				return ErrScopeChanged
			}
			if err := recheckDeclarations(ctx); err != nil {
				return err
			}
			other, err := declarationMultiset(actual, member)
			if err != nil || len(other) != len(keys) {
				return ErrAuthority
			}
			for key, count := range keys {
				if other[key] != count {
					return ErrAuthority
				}
			}
			return recheckDeclarations(ctx)
		}
		docAuth := func(ctx context.Context, scope overview.Scope, declaration overview.DocumentDeclaration) error {
			one, err := declarationMultiset([]overview.DocumentDeclaration{declaration}, member)
			if err != nil || scope != expectedScope {
				return ErrAuthority
			}
			for key := range one {
				if keys[key] == 0 {
					return ErrAuthority
				}
			}
			if err := recheckDeclarations(ctx); err != nil {
				return err
			}
			if err := pathAuth(ctx, scope, declaration.Source.Path); err != nil {
				return err
			}
			if err := authorizeDocument(ctx, scope, declaration); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return ErrAuthority
			}
			return recheckDeclarations(ctx)
		}
		if err := ownerGuard(ctx, binding); err != nil {
			return empty, err
		}
		return app.FreshnessPlanRestoreInput{Plan: overview.PlanRestoreInput{Guard: ownerGuard, Snapshot: graph.Snapshot, AuthorizeScope: scopeAuth, AuthorizePath: pathAuth, VerifyFacts: verifyFacts, AuthorizeDocument: docAuth, VerifyDeclarations: verifyDeclarations},
			PrincipalID: principal.ID, Scanner: base.Pack.Scanner, Validation: base.Validation, CaptureLive: base.CaptureLive, RequestLimit: 65536, DeclarationRevisionDigest: revisionDigest}, nil
	}, nil
}

func declarationRevisionValid(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return true
}

// declarationMultiset compares the complete bounded provider set, including
// multiplicity. It checks the source identity before allocating serialized
// keys and never treats archive data as permission.
func declarationMultiset(items []overview.DocumentDeclaration, member model.ScopeMember) (map[string]int, error) {
	if len(items) > 512 {
		return nil, app.ErrCoreLimit
	}
	keys := make(map[string]int, len(items))
	total := 0
	for _, item := range items {
		ref := item.Source
		if item.Kind != "objective" && item.Kind != "constraint" && item.Kind != "component" || len(item.EntityID) > 768 || ref.WorktreeID != member.WorktreeID || ref.ProfileID != member.ProfileID || ref.GenerationID != member.GenerationID || len(ref.ContentHash) != 64 || len(ref.Path) > 16384 || model.ValidateRelativePath(ref.Path, false) != nil || ref.Range.StartByte < 0 || ref.Range.EndByte <= ref.Range.StartByte {
			return nil, ErrAuthority
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, ErrAuthority
		}
		total += len(raw)
		if total > 8*1024*1024 {
			return nil, app.ErrCoreLimit
		}
		keys[string(raw)]++
	}
	return keys, nil
}
