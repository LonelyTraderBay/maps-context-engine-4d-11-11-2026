package compose

import (
	"context"
	"encoding/json"
	"errors"
	"path"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

// CoreBackend composes explicitly authorized single-worktree operations.
// Availability depends on the supplied owners.
type CoreBackend struct {
	Reader          *ScopeReader
	Authority       *BoundCoreAuthority
	Cursor          *contextpack.CursorCodec
	packCache       *app.FreshnessPackCache
	cliPackStore    app.SymbolPackStorage
	limits          model.CoreRuntimeLimits
	verifyWorkspace WorkspaceVerifier
	planSource      PlanDeclarationProvider
	topicSource     TopicConfigProvider
	buildGate       chan struct{}
	renameOwner     *RenameOwner
}

// WorkspaceVerifier must independently re-inventory and rehash the complete
// workspace and dependency inputs; a caller may pass nil to leave checked
// freshness unavailable. It must never execute or modify the target.
type WorkspaceVerifier func(context.Context, app.CorePrincipal, model.ScopeMember, *snapshot.Snapshot) (app.WorkspaceObservation, error)

// PlanDeclarations is supplied by an independently authorized owner. Recheck
// must verify the actor, the complete declaration set and its revision even
// when Items is empty. AuthorizeDocument rechecks each exact declaration.
// RetainedBytes conservatively accounts for data held by owner callbacks.
type PlanDeclarations struct {
	Items             []overview.DocumentDeclaration
	Recheck           func(context.Context) error
	AuthorizeDocument func(context.Context, overview.Scope, overview.DocumentDeclaration) error
	RetainedBytes     int64
	// Nonsecret digest of authority identity and revision as constructed by
	// DeclarationRevisionDigest. Empty permits only process-local plans.
	// The provider independently verifies the identity and revision and its
	// Recheck must detect any later change, even if Items remain unchanged.
	RevisionDigest string
}

type PlanDeclarationProvider func(context.Context, app.CorePrincipal, model.ScopeVector, model.PackRequest) (PlanDeclarations, error)

// TopicConfig names one explicitly approved captured alias configuration.
// Recheck must bind the actor, approval and config selection on every use.
type TopicConfig struct {
	Path            string
	Recheck         func(context.Context) error
	AuthorizeConfig func(context.Context, overview.Scope, string) error
	AuthorizeAlias  func(context.Context, overview.Scope, overview.AliasDeclaration) error
}

type TopicConfigProvider func(context.Context, app.CorePrincipal, model.ScopeVector) (TopicConfig, error)

// NewCoreBackend captures the same effective startup limits used by the
// service. The process owner supplies one shared cache for all bound readers.
func NewCoreBackend(config *app.RuntimeConfigService, reader *ScopeReader, authority *BoundCoreAuthority, codec *contextpack.CursorCodec, cache *app.FreshnessPackCache, verify WorkspaceVerifier) (*CoreBackend, error) {
	if config == nil || reader == nil || authority == nil || authority.Reader != reader || codec == nil || cache == nil {
		return nil, app.ErrCoreInput
	}
	effective, digest, err := config.StartupConfig()
	if err != nil {
		return nil, app.ErrCoreInput
	}
	computed, err := model.RuntimeConfigDigest(effective, 65536)
	if err != nil || computed != digest || !cache.WithinLimits(codec, effective.Core.MaxRetainedQueryResults, effective.Core.QueryResultCacheBytes, effective.Core.PackTtlSeconds) {
		return nil, app.ErrCoreInput
	}
	return &CoreBackend{Reader: reader, Authority: authority, Cursor: codec, packCache: cache, limits: effective.Core, verifyWorkspace: verify, buildGate: make(chan struct{}, 1)}, nil
}

// NewCoreBackendWithPlan enables overview composition only when its trusted owner
// supplies explicit declarations and a live, revision-bound authority check.
func NewCoreBackendWithPlan(config *app.RuntimeConfigService, reader *ScopeReader, authority *BoundCoreAuthority, codec *contextpack.CursorCodec, cache *app.FreshnessPackCache, verify WorkspaceVerifier, source PlanDeclarationProvider) (*CoreBackend, error) {
	if source == nil {
		return nil, app.ErrCoreInput
	}
	backend, err := NewCoreBackend(config, reader, authority, codec, cache, verify)
	if err != nil {
		return nil, err
	}
	backend.planSource = source
	return backend, nil
}

// NewCoreBackendWithSources enables topic lookup only with an explicit
// configuration owner. Plan declarations remain independently optional.
func NewCoreBackendWithSources(config *app.RuntimeConfigService, reader *ScopeReader, authority *BoundCoreAuthority, codec *contextpack.CursorCodec, cache *app.FreshnessPackCache, verify WorkspaceVerifier, plan PlanDeclarationProvider, topic TopicConfigProvider) (*CoreBackend, error) {
	if topic == nil {
		return nil, app.ErrCoreInput
	}
	backend, err := NewCoreBackend(config, reader, authority, codec, cache, verify)
	if err != nil {
		return nil, err
	}
	backend.planSource, backend.topicSource = plan, topic
	return backend, nil
}

var _ app.CoreBackend = (*CoreBackend)(nil)

func (b *CoreBackend) valid() bool {
	return b != nil && b.Reader != nil && b.Authority != nil && b.Authority.Reader == b.Reader && b.Cursor != nil
}

func (b *CoreBackend) WorkspaceStatus(ctx context.Context, principal app.CorePrincipal, request model.StatusRequest, options app.CoreCallOptions) ([][]byte, error) {
	if !b.valid() || ctx == nil || request.Verify != nil && *request.Verify != "none" && *request.Verify != "workspace" {
		return nil, app.ErrCoreInput
	}
	verify := request.Verify != nil && *request.Verify == "workspace"
	observation, err := b.resolveRequestScope(ctx, principal, request.Scope)
	if err != nil {
		if errors.Is(err, index.ErrSnapshotUnavailable) && (request.Scope == nil || request.Scope.GenerationID == nil && request.Scope.PinnedVector == nil) {
			binding, checkErr := b.Reader.ObserveMissingIndex(ctx, principal)
			if checkErr != nil {
				return nil, coreStoreError(checkErr)
			}
			if options.MaxOutputBytes > binding.Policy.MaxOutputBytes {
				options.MaxOutputBytes = binding.Policy.MaxOutputBytes
			}
			response := baseResponse(options, model.ScopeVector{})
			response.Scope = nil
			if verify {
				response.Status = "partial"
				response.Freshness.Status = "unknown"
			}
			profile := binding.ProfileID
			response.Data = model.StatusData{Operation: "status", Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "missing", RegisteredScopes: []model.RegisteredScopeStatus{{WorktreeID: binding.WorktreeID, ProfileID: &profile, RootStatus: "registered", IndexState: "missing"}}}
			return encodeResponse(response)
		}
		return nil, coreStoreError(err)
	}
	if options.MaxOutputBytes > observation.Binding.Policy.MaxOutputBytes {
		options.MaxOutputBytes = observation.Binding.Policy.MaxOutputBytes
	}
	member := observation.Vector.Members[0]
	profile := member.ProfileID
	response := baseResponse(options, observation.Vector)
	response.Data = model.StatusData{Operation: "status", Scopes: []model.ScopeMember{member}, Capabilities: []model.Capability{}, ProjectStage: "unknown", IndexState: "published", RegisteredScopes: []model.RegisteredScopeStatus{{WorktreeID: member.WorktreeID, ProfileID: &profile, RootStatus: "registered", IndexState: "published"}}}
	if verify {
		freshness, err := b.statusWorkspaceFreshness(ctx, principal, observation)
		if err != nil {
			return nil, err
		}
		response.Freshness = freshness
		if freshness.Status == "changed" {
			response.Status = "partial"
			response.Warnings = append(response.Warnings, model.Gap{Code: "WORKSPACE_CHANGED", Reason: "The workspace differs from the pinned snapshot.", SourceRefs: []model.SourceRef{}})
		}
	}
	return encodeResponse(response)
}

func (b *CoreBackend) Locate(ctx context.Context, principal app.CorePrincipal, request model.LocateRequest, options app.CoreCallOptions) ([][]byte, error) {
	if !b.valid() {
		return nil, app.ErrCoreInput
	}
	guard := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if binding.PrincipalDigest != principal.Digest || binding.BindingDigest != principal.BindingDigest || binding.OrderVersion != "locate-id-v1" {
			return ErrAuthority
		}
		if err := b.Authority.samePrincipal(ctx, principal); err != nil {
			return err
		}
		_, err := b.Reader.Recheck(ctx, principal, binding.Scope)
		return err
	}
	var observation ScopeObservation
	var binding contextpack.CursorBinding
	var err error
	if request.Cursor != nil {
		claims, e := b.Cursor.VerifyPrincipal(ctx, *request.Cursor, "list", principal.Digest, guard)
		if e != nil {
			return nil, coreStoreError(e)
		}
		binding = claims.Binding
		observation, err = b.Reader.Recheck(ctx, principal, binding.Scope)
	} else {
		observation, err = b.resolveRequestScope(ctx, principal, request.Scope)
		if err == nil {
			binding = contextpack.CursorBinding{Purpose: "list", PrincipalDigest: principal.Digest, BindingDigest: principal.BindingDigest, OrderVersion: "locate-id-v1", Scope: observation.Vector}
		}
	}
	if err != nil {
		return nil, coreStoreError(err)
	}
	if options.MaxOutputBytes > observation.Binding.Policy.MaxOutputBytes {
		options.MaxOutputBytes = observation.Binding.Policy.MaxOutputBytes
	}
	member := observation.Vector.Members[0]
	maxFile := observation.Binding.Policy.MaxFileBytes
	if maxFile > 64*1024*1024 {
		maxFile = 64 * 1024 * 1024
	}
	maxScan := maxFile
	if maxScan < 131072 {
		maxScan = 131072
	}
	scanner, err := policy.NewScanner(int(maxScan))
	if err != nil {
		return nil, err
	}
	base := search.Request{MaxOutputBytes: 1024 * 1024, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), Scanner: scanner,
		AuthorizeGeneration: func(ctx context.Context, worktree, profile, digest string, epoch int64) error {
			if worktree != member.WorktreeID || profile != member.ProfileID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
				return ErrScopeChanged
			}
			return b.Authority.samePrincipal(ctx, principal)
		},
		AuthorizePath: func(ctx context.Context, worktree, name, digest string, epoch int64) error {
			if worktree != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
				return ErrScopeChanged
			}
			if err := b.Authority.samePrincipal(ctx, principal); err != nil {
				return err
			}
			return b.Authority.path(ctx, principal, observation.Binding.Policy, name)
		},
	}
	if request.Mode == "module" {
		return b.locateModules(ctx, principal, request, options, observation, binding, guard, base)
	}
	if request.Mode == "topic" {
		return b.locateTopic(ctx, principal, request, options, observation, binding, guard, base)
	}
	// If the actual wire envelope is too large, retry the same pinned keyset
	// with a smaller limit. No page bytes or cursor are released until it fits.
	for limit := request.Limit; limit >= 1; limit-- {
		pageRequest := request
		pageRequest.Limit = limit
		page, e := search.LocateKeysetPage(ctx, b.Reader.Index, b.Cursor, binding, guard, principal.ID, pageRequest, base)
		if e != nil {
			return nil, coreStoreError(e)
		}
		response, e := locateResponse(options, observation.Vector, page)
		if e != nil {
			return nil, e
		}
		blocks, e := encodeResponse(response)
		if e == nil {
			return blocks, nil
		}
		if !errors.Is(e, contextpack.ErrBudgetTooSmall) {
			return nil, e
		}
	}
	return nil, contextpack.ErrBudgetTooSmall
}

func (b *CoreBackend) resolveRequestScope(ctx context.Context, principal app.CorePrincipal, scope *model.ScopeRequest) (ScopeObservation, error) {
	if scope == nil {
		return b.Reader.Resolve(ctx, principal, "")
	}
	if scope.GroupID != nil || scope.WorktreeID != nil && *scope.WorktreeID != b.Reader.WorktreeID || scope.ProfileID != nil && *scope.ProfileID != b.Reader.ProfileID {
		return ScopeObservation{}, ErrAuthority
	}
	if scope.PinnedVector != nil {
		return b.Reader.Recheck(ctx, principal, *scope.PinnedVector)
	}
	generation := ""
	if scope.GenerationID != nil {
		generation = *scope.GenerationID
	}
	return b.Reader.Resolve(ctx, principal, generation)
}

func baseResponse(options app.CoreCallOptions, scope model.ScopeVector) model.Response {
	zero := int64(0)
	return model.Response{SchemaVersion: 1, RequestID: options.RequestID, Status: "ok", Scope: &scope,
		Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"},
		Coverage:  model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "Published index; coverage is not established."},
		Readiness: "needs_context", Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero},
		Budget: model.Budget{MaxOutputBytes: options.MaxOutputBytes, DeliveryMode: options.DeliveryMode}, Warnings: []model.Gap{}}
}

func locateResponse(options app.CoreCallOptions, scope model.ScopeVector, page search.LocatePage) (model.Response, error) {
	response := baseResponse(options, scope)
	member := scope.Members[0]
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	candidates := make([]model.Candidate, 0, len(page.Result.Symbols)+len(page.Result.Text))
	for _, hit := range page.Result.Symbols {
		refs := []model.SourceRef{}
		if hit.Source != nil {
			refs = append(refs, *hit.Source)
		}
		candidates = append(candidates, model.Candidate{Ref: &model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: hit.Symbol.SymbolID}, Name: hit.Symbol.Name, QualifiedName: hit.Symbol.QualifiedName, SourceRefs: refs, EvidenceRefs: []model.EvidenceRef{}})
	}
	for _, hit := range page.Result.Text {
		hash := hit.ContentHash
		candidates = append(candidates, model.Candidate{Ref: &model.FileRef{Kind: "file", GraphIdentity: identity, Path: hit.Path, ContentHash: &hash}, Name: path.Base(hit.Path), QualifiedName: hit.Path, SourceRefs: []model.SourceRef{}, EvidenceRefs: []model.EvidenceRef{}})
	}
	response.Data = model.LocateData{Operation: "locate", Candidates: candidates}
	if page.Result.Coverage == "partial" {
		response.Status = "partial"
		response.Coverage.Status = "partial"
		response.Coverage.Gaps = append(response.Coverage.Gaps, model.Gap{Code: "UNCAPTURED_FILE_MATCHES", Reason: "Some matching files lack captured source bytes.", SourceRefs: []model.SourceRef{}})
	}
	response.Pagination.ReturnedCount = int64(len(candidates))
	if page.NextCursor != nil {
		reason := "limit"
		response.Pagination.Truncated, response.Pagination.EnumerationComplete, response.Pagination.OptionalPending, response.Pagination.NextCursor, response.Pagination.LimitReason = true, false, nil, page.NextCursor, &reason
		response.Status = "partial"
	}
	if page.Result.Ambiguous {
		response.Status = "partial"
		response.Warnings = append(response.Warnings, model.Gap{Code: "AMBIGUOUS_SYMBOL", Reason: "Multiple candidates require explicit selection.", SourceRefs: []model.SourceRef{}})
	}
	return response, nil
}

func encodeResponse(response model.Response) ([][]byte, error) {
	budget := response.Budget
	settled, blocks, err := model.SettleSerializedBudget(budget, func(size int64) ([][]byte, error) {
		response.Budget.SerializedBytes = size
		raw, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		if budget.DeliveryMode == "mcp_dual" {
			return [][]byte{raw, append([]byte(nil), raw...)}, nil
		}
		return [][]byte{raw}, nil
	})
	if err != nil || model.ValidateDeliveredBytes(settled, blocks) != nil {
		return nil, contextpack.ErrBudgetTooSmall
	}
	return blocks, nil
}

func coreStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrScopeChanged), errors.Is(err, model.ErrScopeChanged):
		return app.ErrValidationScopeChanged
	case errors.Is(err, ErrAuthority), errors.Is(err, overview.ErrAuthority):
		return app.ErrValidationAuthority
	case errors.Is(err, overview.ErrInput):
		return app.ErrCoreInput
	case errors.Is(err, index.ErrInvalidSearch):
		return app.ErrCoreInput
	case errors.Is(err, overview.ErrLimit):
		return app.ErrCoreLimit
	case errors.Is(err, index.ErrSnapshotUnavailable):
		return app.ErrCoreIndexMissing
	case errors.Is(err, packstore.ErrMissing):
		return app.ErrCoreResultExpired
	case errors.Is(err, packstore.ErrCapacity):
		return app.ErrCoreLimit
	case errors.Is(err, index.ErrStoreChanged), errors.Is(err, records.ErrStoreChanged), errors.Is(err, catalog.ErrCatalogChanged):
		return app.ErrCoreStoreChanged
	case errors.Is(err, index.ErrCaptureLimit), errors.Is(err, records.ErrLimit):
		return app.ErrCoreLimit
	default:
		return err
	}
}
