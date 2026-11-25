package compose

import (
	"context"
	"errors"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

var ErrAuthority = errors.New("scope authority denied")

// PathAccess must enforce the actor's current allowlist and all policy deny
// globs. It receives a defensive policy copy, never grants rights by default,
// and must not reenter a database pool held by the caller.
type PathAccess func(context.Context, app.CorePrincipal, model.PolicyConfig, string) error

// BoundCoreAuthority uses an explicitly supplied authentication source and
// path authorizer; it does not create a user, token or allowlist. The reader
// verifies the actual single scope through the three independent stores.
type BoundCoreAuthority struct {
	Reader                *ScopeReader
	AuthenticatePrincipal func(context.Context) (app.CorePrincipal, error)
	AuthorizePath         PathAccess
	// CheckMetadataPath checks policy on byte-free capture metadata. The
	// enclosing graph read separately authorizes the full generation.
	CheckMetadataPath func(name string, deniedGlobs []string) error
	// AuthorizeBoundPath checks a live file within an already authorized
	// scope. Callers must recheck the principal and scope at their read
	// or delivery boundaries.
	AuthorizeBoundPath PathAccess
}

var _ app.CoreAuthority = (*BoundCoreAuthority)(nil)

func (a *BoundCoreAuthority) Authenticate(ctx context.Context) (app.CorePrincipal, error) {
	if a == nil || a.Reader == nil || a.AuthenticatePrincipal == nil || a.AuthorizePath == nil || ctx == nil {
		return app.CorePrincipal{}, ErrAuthority
	}
	p, err := a.AuthenticatePrincipal(ctx)
	if err != nil || !a.Reader.valid(p) {
		return app.CorePrincipal{}, ErrAuthority
	}
	if err := a.Reader.Access(ctx, p, a.Reader.WorktreeID, a.Reader.ProfileID); err != nil {
		return app.CorePrincipal{}, err
	}
	return p, ctx.Err()
}

func (a *BoundCoreAuthority) samePrincipal(ctx context.Context, p app.CorePrincipal) error {
	current, err := a.Authenticate(ctx)
	if err != nil || current != p {
		return ErrAuthority
	}
	return nil
}

func (a *BoundCoreAuthority) AuthorizeRequest(ctx context.Context, p app.CorePrincipal, name string, request model.Request) error {
	if err := a.samePrincipal(ctx, p); err != nil {
		return err
	}
	var scope *model.ScopeRequest
	var inputPath string
	var ref model.GraphRef
	switch value := request.(type) {
	case model.StatusRequest:
		if name != "mce4d_workspace_status" {
			return ErrAuthority
		}
		scope = value.Scope
	case model.LocateRequest:
		if name != "mce4d_locate" {
			return ErrAuthority
		}
		scope = value.Scope
		if value.Position != nil {
			inputPath = value.Position.Path
		} else if value.Path != nil {
			inputPath = *value.Path
		}
	case model.PackRequest:
		if name != "mce4d_context_pack" {
			return ErrAuthority
		}
		scope = value.Scope
		switch selector := value.Selector.(type) {
		case *model.PositionSelector:
			if selector == nil {
				return ErrAuthority
			}
			inputPath = selector.Path
		case *model.DocumentSelector:
			if selector == nil {
				return ErrAuthority
			}
			inputPath = selector.Path
		case *model.RefSelector:
			if selector == nil || selector.ServiceID != nil {
				return ErrAuthority
			}
			ref = selector.Ref
			if file, ok := ref.(*model.FileRef); ok {
				if file == nil {
					return ErrAuthority
				}
				inputPath = file.Path
			}
		}
	case model.ExpandRequest:
		if name != "mce4d_expand_pack" {
			return ErrAuthority
		}
	case model.ValidateRequest:
		if name != "mce4d_validate_pack" {
			return ErrAuthority
		}
	case *model.RenameInitialRequest:
		if name != "mce4d_rename_preview" || value == nil {
			return ErrAuthority
		}
		scope, inputPath = value.Scope, value.Selector.Path
	case *model.RenameContinueRequest:
		if name != "mce4d_rename_preview" || value == nil {
			return ErrAuthority
		}
	default:
		return ErrAuthority
	}
	if scope != nil {
		if scope.GroupID != nil || scope.WorktreeID != nil && *scope.WorktreeID != a.Reader.WorktreeID || scope.ProfileID != nil && *scope.ProfileID != a.Reader.ProfileID {
			return ErrAuthority
		}
		if scope.PinnedVector != nil {
			if _, err := a.Reader.Recheck(ctx, p, *scope.PinnedVector); err != nil {
				return err
			}
		}
	}
	if ref != nil {
		identity, ok := refIdentity(ref)
		if !ok || identity.WorktreeID != a.Reader.WorktreeID || identity.ProfileID != a.Reader.ProfileID {
			return ErrAuthority
		}
		if scope != nil && scope.GenerationID != nil && *scope.GenerationID != identity.GenerationID {
			return ErrAuthority
		}
		if scope != nil && scope.PinnedVector != nil {
			if _, err := model.MatchPinnedMember(*scope.PinnedVector, ref, nil); err != nil {
				return ErrAuthority
			}
		}
	}
	if inputPath != "" {
		binding, err := a.Reader.Catalog.ReadBinding(ctx, a.Reader.WorktreeID, a.Reader.ProfileID, func(ctx context.Context, worktree, profile string) error {
			return a.Reader.Access(ctx, p, worktree, profile)
		})
		if err != nil {
			return err
		}
		if err := binding.VerifyRoots(a.Reader.WorktreeRoot, a.Reader.CommonRoot, a.Reader.LinkedRelative); err != nil {
			return err
		}
		if err := a.path(ctx, p, binding.Policy, inputPath); err != nil {
			return err
		}
	}
	return a.samePrincipal(ctx, p)
}

func (a *BoundCoreAuthority) AuthorizeResponse(ctx context.Context, p app.CorePrincipal, name string, request model.Request, response model.Response) error {
	if err := a.samePrincipal(ctx, p); err != nil {
		return err
	}
	var observation ScopeObservation
	if response.Scope != nil {
		var err error
		observation, err = a.Reader.Recheck(ctx, p, *response.Scope)
		if err != nil {
			return err
		}
	} else {
		data, ok := response.Data.(model.StatusData)
		if !ok || data.IndexState != "missing" || len(data.Scopes) != 0 || len(data.RegisteredScopes) != 1 || data.RegisteredScopes[0].IndexState != "missing" || len(response.Coverage.Gaps) != 0 || len(response.Warnings) != 0 {
			return ErrAuthority
		}
		if _, err := a.Reader.ObserveMissingIndex(ctx, p); err != nil {
			return err
		}
	}
	checkRef := func(ref model.SourceRef) error {
		if response.Scope == nil {
			return ErrAuthority
		}
		if _, err := model.MatchPinnedSource(*response.Scope, ref, nil, 32768); err != nil {
			return ErrAuthority
		}
		return a.boundPath(ctx, p, observation.Binding.Policy, ref.Path)
	}
	checkRefs := func(refs []model.SourceRef) error {
		for _, ref := range refs {
			if err := checkRef(ref); err != nil {
				return err
			}
		}
		return nil
	}
	checkGaps := func(gaps []model.Gap) error {
		for _, gap := range gaps {
			if err := checkRefs(gap.SourceRefs); err != nil {
				return err
			}
		}
		return nil
	}
	checkEvidence := func(items []model.EvidenceRef) error {
		for _, item := range items {
			if err := checkRefs(item.SourceRefs); err != nil {
				return err
			}
		}
		return nil
	}
	checkGraph := func(ref model.GraphRef) error {
		if response.Scope == nil {
			return ErrAuthority
		}
		if _, err := model.MatchPinnedMember(*response.Scope, ref, nil); err != nil {
			return ErrAuthority
		}
		if file, ok := ref.(*model.FileRef); ok {
			return a.boundPath(ctx, p, observation.Binding.Policy, file.Path)
		}
		return nil
	}
	if err := checkGaps(response.Coverage.Gaps); err != nil {
		return err
	}
	if err := checkGaps(response.Warnings); err != nil {
		return err
	}
	if response.Error != nil {
		if err := checkGaps(response.Error.Details); err != nil {
			return err
		}
	}
	switch data := response.Data.(type) {
	case nil:
		if wanted, ok := request.(model.SourceReadRequest); ok && name == "POST /source/read" {
			if response.Status != "error" || response.Error == nil || response.Error.Code != "RESULT_TOO_LARGE" ||
				len(response.Error.Details) != 1 || response.Error.Details[0].Code != "SOURCE_RANGE_TOO_LARGE" ||
				len(response.Error.Details[0].SourceRefs) != 1 || response.Error.Details[0].SourceRefs[0] != wanted.SourceRef {
				return ErrAuthority
			}
			break
		}
		if response.Scope == nil || response.Status != "error" || response.Error == nil || response.Error.Code != "RESULT_TOO_LARGE" || len(response.Error.Details) != 1 || response.Error.Details[0].Code != "EVIDENCE_ITEM_TOO_LARGE" {
			return ErrAuthority
		}
		switch request.(type) {
		case model.PackRequest:
			if name != "mce4d_context_pack" {
				return ErrAuthority
			}
		case model.ExpandRequest:
			if name != "mce4d_expand_pack" {
				return ErrAuthority
			}
		default:
			return ErrAuthority
		}
	case model.StatusData:
		for _, item := range data.RegisteredScopes {
			if item.WorktreeID != a.Reader.WorktreeID || item.ProfileID != nil && *item.ProfileID != a.Reader.ProfileID || item.RootStatus != "registered" {
				return ErrAuthority
			}
		}
		for _, item := range data.Scopes {
			if response.Scope == nil || len(response.Scope.Members) != 1 || !reflect.DeepEqual(item, response.Scope.Members[0]) {
				return ErrAuthority
			}
		}
	case model.LocateData:
		query, ok := request.(model.LocateRequest)
		if !ok || len(data.Excerpts) > 0 && query.Mode != "evidence" {
			return ErrAuthority
		}
		for _, excerpt := range data.Excerpts {
			if err := checkRef(excerpt.SourceRef); err != nil {
				return err
			}
		}
		for _, item := range data.Candidates {
			if err := checkGraph(item.Ref); err != nil {
				return err
			}
			if err := checkRefs(item.SourceRefs); err != nil {
				return err
			}
			if err := checkEvidence(item.EvidenceRefs); err != nil {
				return err
			}
		}
	case model.GraphData:
		wanted, ok := request.(model.GraphQueryRequest)
		if !ok || name != "POST /graph/query" || (wanted.Directory == nil) != (data.Directory == nil) {
			return ErrAuthority
		}
		if data.Directory != nil {
			if *wanted.Directory != data.Directory.Path || response.Scope == nil {
				return ErrAuthority
			}
			checkPath := a.CheckMetadataPath
			if checkPath == nil {
				checkPath = policy.CheckConfiguredPath
			}
			for _, entry := range data.Directory.Entries {
				if err := checkPath(entry.Path, observation.Binding.Policy.DeniedGlobs); err != nil {
					return err
				}
			}
		}
		for _, node := range data.Nodes {
			if err := checkGraph(node.Ref); err != nil {
				return err
			}
			if err := checkRefs(node.SourceRefs); err != nil {
				return err
			}
			if err := checkEvidence(node.EvidenceRefs); err != nil {
				return err
			}
		}
		for _, edge := range data.Edges {
			if err := checkEvidence(edge.EvidenceRefs); err != nil {
				return err
			}
		}
	case model.SourceData:
		wanted, ok := request.(model.SourceReadRequest)
		if !ok || name != "POST /source/read" || data.SourceRef != wanted.SourceRef {
			return ErrAuthority
		}
		if err := checkRef(data.SourceRef); err != nil {
			return err
		}
	case model.FlowData:
		wanted, ok := request.(model.FlowQueryRequest)
		if !ok || name != "POST /flow/query" || !reflect.DeepEqual(wanted.RootRef, data.RootRef) {
			return ErrAuthority
		}
		mode := "static"
		if wanted.Mode != nil {
			mode = *wanted.Mode
		}
		if mode != data.Mode || mode == "observed" && (wanted.ArtifactID == nil || len(data.Steps) == 0) {
			return ErrAuthority
		}
		if mode == "observed" {
			bound := false
			for _, ref := range data.Steps[0].EvidenceRefs {
				if ref.Origin == "runtime_observation" && ref.Kind == "runtime_observed" &&
					ref.ArtifactID != nil && *ref.ArtifactID == *wanted.ArtifactID {
					bound = true
				}
			}
			if !bound {
				return ErrAuthority
			}
		}
		if err := checkGraph(data.RootRef); err != nil {
			return err
		}
		for _, step := range data.Steps {
			if mode == "observed" && (step.PossibleOrObserved != "observed" || step.OrderSemantics != "causal_trace" ||
				len(step.EvidenceRefs) == 0) {
				return ErrAuthority
			}
			for _, ref := range step.CanonicalRefs {
				if err := checkGraph(ref); err != nil {
					return err
				}
			}
			if err := checkRefs(step.SourceRefs); err != nil {
				return err
			}
			if err := checkEvidence(step.EvidenceRefs); err != nil {
				return err
			}
			if err := checkGaps(step.Limitations); err != nil {
				return err
			}
		}
		for _, edge := range data.Edges {
			if edge.ConditionSourceRef != nil {
				if err := checkRef(*edge.ConditionSourceRef); err != nil {
					return err
				}
			}
			if err := checkEvidence(edge.EvidenceRefs); err != nil {
				return err
			}
		}
	case model.PackData:
		for _, ref := range data.TargetRefs {
			if err := checkGraph(ref); err != nil {
				return err
			}
		}
		for _, item := range data.Items {
			if err := checkRefs(item.SourceRefs); err != nil {
				return err
			}
			if err := checkEvidence(item.EvidenceRefs); err != nil {
				return err
			}
			if err := checkGaps(item.Limitations); err != nil {
				return err
			}
			if item.FragmentOf != nil {
				if err := checkRef(*item.FragmentOf); err != nil {
					return err
				}
			}
		}
	case model.ValidateData:
		if err := checkRefs(data.ChangedSources); err != nil {
			return err
		}
	case model.RenameData:
		for _, edit := range data.Edits {
			if err := checkRef(edit.SourceRef); err != nil {
				return err
			}
		}
		if err := checkGaps(data.Conflicts); err != nil {
			return err
		}
	default:
		return ErrAuthority
	}
	if response.Scope != nil {
		if _, err := a.Reader.Recheck(ctx, p, *response.Scope); err != nil {
			return err
		}
	}
	return a.samePrincipal(ctx, p)
}

func (a *BoundCoreAuthority) path(ctx context.Context, p app.CorePrincipal, config model.PolicyConfig, name string) error {
	return checkSourcePath(ctx, p, config, name, a.AuthorizePath)
}

func (a *BoundCoreAuthority) boundPath(ctx context.Context, p app.CorePrincipal, config model.PolicyConfig, name string) error {
	check := a.AuthorizeBoundPath
	if check == nil {
		check = a.AuthorizePath
	}
	return checkSourcePath(ctx, p, config, name, check)
}

func checkSourcePath(ctx context.Context, p app.CorePrincipal, config model.PolicyConfig, name string, check PathAccess) error {
	if err := policy.CheckPath(name); err != nil {
		return err
	}
	config.AllowedRoots = append([]string(nil), config.AllowedRoots...)
	config.DeniedGlobs = append([]string(nil), config.DeniedGlobs...)
	config.TrustedRulePaths = append([]string(nil), config.TrustedRulePaths...)
	config.DependencyRoots = append([]string(nil), config.DependencyRoots...)
	config.AllowedExecutables = append([]model.AllowedExecutable(nil), config.AllowedExecutables...)
	if err := check(ctx, p, config, name); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrAuthority
	}
	return ctx.Err()
}

func refIdentity(ref model.GraphRef) (model.GraphIdentity, bool) {
	switch value := ref.(type) {
	case *model.SymbolRef:
		if value != nil {
			return value.GraphIdentity, true
		}
	case *model.ModuleRef:
		if value != nil {
			return value.GraphIdentity, true
		}
	case *model.EntityRef:
		if value != nil {
			return value.GraphIdentity, true
		}
	case *model.AnchorRef:
		if value != nil {
			return value.GraphIdentity, true
		}
	case *model.FileRef:
		if value != nil {
			return value.GraphIdentity, true
		}
	}
	return model.GraphIdentity{}, false
}
