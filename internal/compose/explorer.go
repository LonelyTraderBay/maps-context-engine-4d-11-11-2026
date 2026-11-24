package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// ExplorerReader uses the same authorized catalog, index, path checks and
// response accounting as the core application. One instance belongs to one
// foreground Explorer session.
type ExplorerReader struct {
	core         *CoreBackend
	registry     *contracts.Registry
	limits       model.ExplorerRuntimeLimits
	codec        *contextpack.CursorCodec
	projection   chan struct{}
	graphs       explorerGraphs
	selection    *app.CoreService
	staticFlow   staticFlowOwner
	observedFlow ObservedRunAuthority
}

func NewExplorerReader(core *CoreBackend, cfg model.RuntimeConfig) (*ExplorerReader, error) {
	return newExplorerReader(core, cfg, nil)
}

// NewExplorerReaderWithObservedRuns enables the existing flow route only for
// runs provided by the current owner. It does not grant Observe or target access.
func NewExplorerReaderWithObservedRuns(core *CoreBackend, cfg model.RuntimeConfig, owner ObservedRunAuthority) (*ExplorerReader, error) {
	if owner == nil {
		return nil, app.ErrCoreInput
	}
	return newExplorerReader(core, cfg, owner)
}

func newExplorerReader(core *CoreBackend, cfg model.RuntimeConfig, owner ObservedRunAuthority) (*ExplorerReader, error) {
	if !core.valid() {
		return nil, app.ErrCoreInput
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		return nil, err
	}
	key, err := contextpack.GenerateCursorKey()
	if err != nil {
		return nil, err
	}
	codec, err := contextpack.NewCursorCodec(key)
	clear(key[:])
	if err != nil {
		return nil, err
	}
	// These are process-local graph result limits, independent of SQLite
	// worktree quotas and build tooling output budgets.
	return &ExplorerReader{core: core, registry: registry, limits: cfg.Explorer, codec: codec,
		observedFlow: owner,
		projection:   make(chan struct{}, 1), graphs: explorerGraphs{entries: make(map[string]explorerGraph),
			ttl: time.Duration(min(cfg.Core.ResultTtlSeconds, 900)) * time.Second}}, nil
}

func (e *ExplorerReader) Check(ctx context.Context) error {
	p, err := e.core.Authority.Authenticate(ctx)
	if err != nil {
		return coreStoreError(err)
	}
	_, err = e.core.Reader.Resolve(ctx, p, "")
	if errors.Is(err, index.ErrSnapshotUnavailable) {
		_, err = e.core.Reader.ObserveMissingIndex(ctx, p)
	}
	return coreStoreError(err)
}

func (e *ExplorerReader) SupportsObservedFlow() bool {
	return e.observedFlow != nil
}

func (e *ExplorerReader) Read(ctx context.Context, request model.Request, options app.CoreCallOptions) (explorer.ReadResult, error) {
	principal, err := e.core.Authority.Authenticate(ctx)
	if err != nil {
		return explorer.ReadResult{}, coreStoreError(err)
	}
	var op string
	var blocks [][]byte
	var observedRecheck func(context.Context) error
	switch value := request.(type) {
	case model.StatusRequest:
		op = "GET /workspace"
		blocks, err = e.core.WorkspaceStatus(ctx, principal, value, options)
	case model.SourceReadRequest:
		op = "POST /source/read"
		blocks, err = e.readSource(ctx, principal, value, options)
	case model.GraphQueryRequest:
		op = "POST /graph/query"
		blocks, err = e.readGraph(ctx, principal, value, options)
	case model.FlowQueryRequest:
		op = "POST /flow/query"
		if value.Mode != nil && *value.Mode == "observed" {
			blocks, observedRecheck, err = e.readObservedFlow(ctx, principal, value, options)
		} else {
			blocks, err = e.readFlow(ctx, principal, value, options)
		}
	case model.PackRequest:
		return e.selectionPack(ctx, principal, value, options)
	default:
		return explorer.ReadResult{}, app.ErrCoreUnsupported
	}
	if err != nil {
		return explorer.ReadResult{}, coreStoreError(err)
	}
	if len(blocks) != 1 {
		return explorer.ReadResult{}, app.ErrCoreBackend
	}
	response, err := e.registry.DecodeResponse(contracts.HTTP, op, blocks[0], int(options.MaxOutputBytes), 1)
	if err != nil {
		return explorer.ReadResult{}, app.ErrCoreBackend
	}
	recheck := func(ctx context.Context) error {
		if observedRecheck != nil {
			if err := observedRecheck(ctx); err != nil {
				return coreStoreError(err)
			}
		}
		return coreStoreError(e.core.Authority.AuthorizeResponse(ctx, principal, op, request, response))
	}
	return explorer.ReadResult{Body: blocks[0], Recheck: recheck}, nil
}

func (e *ExplorerReader) publishedRead(principal app.CorePrincipal, observed ScopeObservation) (index.PublishedFileRequest, int64, error) {
	b := e.core
	member := observed.Vector.Members[0]
	maxFile := minInt64(observed.Binding.Policy.MaxFileBytes, b.limits.MaxFileBytes, packSourceLimit)
	maxSource := minInt64(observed.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot, packSourceLimit)
	if maxFile < 1 || maxSource < 1 {
		return index.PublishedFileRequest{}, 0, app.ErrCoreLimit
	}
	scanner, err := policy.NewScanner(int(maxInt64(maxFile, int64(b.Reader.MaxManifestBytes), packFactLimit)))
	if err != nil {
		return index.PublishedFileRequest{}, 0, err
	}
	authorizeGeneration := func(ctx context.Context, worktree, profile, digest string, epoch int64) error {
		if worktree != member.WorktreeID || profile != member.ProfileID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		return b.Authority.samePrincipal(ctx, principal)
	}
	authorizePath := func(ctx context.Context, worktree, name, digest string, epoch int64) error {
		if worktree != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		// The store brackets the read with authorizeGeneration and the
		// response guard rechecks the full scope before delivery.
		if b.Authority.AuthorizeBoundPath != nil {
			return b.Authority.boundPath(ctx, principal, observed.Binding.Policy, name)
		}
		if err := b.Authority.samePrincipal(ctx, principal); err != nil {
			return err
		}
		return b.Authority.path(ctx, principal, observed.Binding.Policy, name)
	}
	return index.PublishedFileRequest{ProfileID: member.ProfileID, GenerationID: member.GenerationID,
		Scanner: scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile),
		AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath}, maxSource, nil
}

func (e *ExplorerReader) readSource(ctx context.Context, p app.CorePrincipal, request model.SourceReadRequest, options app.CoreCallOptions) ([][]byte, error) {
	ref := request.SourceRef
	if ref.WorktreeID != e.core.Reader.WorktreeID || ref.ProfileID != e.core.Reader.ProfileID {
		return nil, ErrAuthority
	}
	observed, err := e.core.Reader.Resolve(ctx, p, ref.GenerationID)
	if err != nil {
		return nil, err
	}
	member, err := model.MatchPinnedSource(observed.Vector, ref, nil, 32768)
	if err != nil {
		return nil, ErrAuthority
	}
	input, _, err := e.publishedRead(p, observed)
	if err != nil {
		return nil, err
	}
	input.Path = ref.Path
	file, err := e.core.Reader.Index.ReadPublishedFile(ctx, input)
	if err != nil {
		if errors.Is(err, index.ErrSnapshotUnavailable) {
			return nil, ErrScopeSnapshotExpired
		}
		if errors.Is(err, index.ErrPublishedFileUnavailable) {
			return nil, app.ErrValidationPrecondition
		}
		return nil, err
	}
	if file.IndexIncarnation != member.IndexIncarnation || file.SnapshotDigest != member.SnapshotDigest ||
		file.PolicyDigest != member.PolicyDigest || file.PolicyEpoch != member.PolicyEpoch {
		return nil, ErrScopeChanged
	}
	if file.ContentHash != ref.ContentHash || ref.ValidateCapturedBytes(file.Bytes) != nil {
		return nil, app.ErrValidationPrecondition
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, e.core.limits.MaxOutputBytes, observed.Binding.Policy.MaxOutputBytes)
	if request.MaxOutputBytes != nil {
		options.MaxOutputBytes = min(options.MaxOutputBytes, *request.MaxOutputBytes)
	}
	response := baseResponse(options, observed.Vector)
	if ref.Range.EndByte-ref.Range.StartByte <= options.MaxOutputBytes {
		response.Data = model.SourceData{Operation: "source_read", SourceRef: ref,
			Content: string(file.Bytes[int(ref.Range.StartByte):int(ref.Range.EndByte)])}
		response.Pagination.ReturnedCount = 1
		blocks, err := encodeResponse(response)
		if err == nil {
			return blocks, nil
		}
	}
	info := app.CoreErrorInfo("POST /source/read", contextpack.ErrItemTooLarge)
	info.Details = []model.Gap{{Code: "SOURCE_RANGE_TOO_LARGE", Reason: "Request a smaller explicit UTF-8 byte range.", SourceRefs: []model.SourceRef{ref}}}
	response.Status, response.Readiness, response.Data, response.Error = "error", "blocked", nil, &info
	response.Pagination.ReturnedCount = 0
	return encodeResponse(response)
}
