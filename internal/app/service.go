package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/telemetry"
)

// CorePrincipal is produced by an authenticated adapter, never by request
// JSON. The authority must bind both digests to this identity and the allowed
// worktree/profile set on every call.
type CorePrincipal struct {
	ID, Digest, BindingDigest string
}

// CoreAuthority is the trusted application boundary for principal, scope,
// source paths and current policy/record/incarnation epochs. Implementations
// must not log the request or response contents passed to these methods.
type CoreAuthority interface {
	Authenticate(context.Context) (CorePrincipal, error)
	AuthorizeRequest(context.Context, CorePrincipal, string, model.Request) error
	AuthorizeResponse(context.Context, CorePrincipal, string, model.Request, model.Response) error
}

// CoreBackend exposes only the six sealed core operations. Each operation
// must use pinned, authorized adapters and return the exact JSON blocks it
// intends to deliver. A missing capability must return an error, not an empty
// success. Transport and real catalog/store composition are separate owners.
type CoreBackend interface {
	WorkspaceStatus(context.Context, CorePrincipal, model.StatusRequest, CoreCallOptions) ([][]byte, error)
	Locate(context.Context, CorePrincipal, model.LocateRequest, CoreCallOptions) ([][]byte, error)
	ContextPack(context.Context, CorePrincipal, model.PackRequest, CoreCallOptions) ([][]byte, error)
	ExpandPack(context.Context, CorePrincipal, model.ExpandRequest, CoreCallOptions) ([][]byte, error)
	ValidatePack(context.Context, CorePrincipal, model.ValidateRequest, CoreCallOptions) ([][]byte, error)
	RenamePreview(context.Context, CorePrincipal, model.Request, CoreCallOptions) ([][]byte, error)
	// VerifyDelivery rechecks expiring result tokens immediately before the
	// service releases the already-serialized response to its adapter.
	VerifyDelivery(context.Context, CorePrincipal, string, model.Request, model.Response) error
}

type CoreCallOptions struct {
	RequestID            string
	MaxOutputBytes       int64
	DeliveryMode         string
	VerifyStructuredHost func(context.Context) error
}

type CoreCallResult struct {
	Blocks  [][]byte
	IsError bool
}

// CoreService enforces the sealed registry, a bounded startup configuration,
// cancellation, exact output accounting and authority at the call boundary.
// It does not open storage, start a server, index a target or grant authority.
type CoreService struct {
	registry  *contracts.Registry
	authority CoreAuthority
	backend   CoreBackend
	config    model.RuntimeConfig
	metrics   *telemetry.Metrics
}

// SupportsCLIPackRetention is a capability claim from the trusted backend,
// not an authentication or authorization check. Every call and delivery must
// still pass the backend's current storage and scope checks.
func (s *CoreService) SupportsCLIPackRetention() bool {
	if s == nil || s.backend == nil {
		return false
	}
	backend, ok := s.backend.(interface{ SupportsCLIPackRetention() bool })
	return ok && backend.SupportsCLIPackRetention()
}

// SupportsCLIRenameRetention requires a trusted retained-result owner. It
// does not bypass per-request authority, provider or freshness checks.
func (s *CoreService) SupportsCLIRenameRetention() bool {
	if s == nil || s.backend == nil {
		return false
	}
	backend, ok := s.backend.(interface{ SupportsCLIRenameRetention() bool })
	return ok && backend.SupportsCLIRenameRetention()
}

func NewCoreService(config *RuntimeConfigService, authority CoreAuthority, backend CoreBackend, metrics *telemetry.Metrics) (*CoreService, error) {
	if config == nil || authority == nil || backend == nil {
		return nil, ErrCoreInput
	}
	effective, digest, err := config.StartupConfig()
	maxMilliseconds := int64((1<<63 - 1) / int64(time.Millisecond))
	if err != nil || len(digest) != 64 || effective.Core.MaxOutputBytes < 4096 || effective.Core.MaxOutputBytes > 131072 || effective.Core.QueryTimeoutMs < 1 || effective.Core.RenameTimeoutMs < 1 || effective.Core.LspStartupTimeoutMs < 1 || effective.Core.QueryTimeoutMs > maxMilliseconds || effective.Core.RenameTimeoutMs > maxMilliseconds || effective.Core.LspStartupTimeoutMs > maxMilliseconds-effective.Core.RenameTimeoutMs {
		return nil, ErrCoreInput
	}
	computed, err := model.RuntimeConfigDigest(effective, 65536)
	if err != nil || computed != digest {
		return nil, ErrCoreInput
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		return nil, ErrCoreInput
	}
	return &CoreService{registry: registry, authority: authority, backend: backend, config: effective, metrics: metrics}, nil
}

// Call accepts a registered MCP core operation. An adapter can reuse it for
// CLI calls while retaining the same request and response schema. Raw input is
// bounded before decoding; output is not returned until all postchecks pass.
func (s *CoreService) Call(ctx context.Context, name string, raw []byte, options CoreCallOptions) (result CoreCallResult, resultErr error) {
	if s == nil || ctx == nil || s.registry == nil || s.authority == nil || s.backend == nil || !coreUUID(options.RequestID) {
		return CoreCallResult{}, ErrCoreInput
	}
	started := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.Observe(telemetry.QueryLatency, time.Since(started).Milliseconds())
			var total int64
			for _, block := range result.Blocks {
				total += int64(len(block))
			}
			s.metrics.Observe(telemetry.OutputBytes, total)
		}
	}()
	options, err := s.normalizeOptions(options)
	if err != nil {
		return CoreCallResult{}, err
	}
	timeout := s.config.Core.QueryTimeoutMs
	if name == "mce4d_rename_preview" {
		timeout = s.config.Core.LspStartupTimeoutMs + s.config.Core.RenameTimeoutMs
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	if options.DeliveryMode == "cli" {
		ctx = context.WithValue(ctx, symbolPackDeliveryKey{}, new(symbolPackDelivery))
	}
	if options.DeliveryMode == "mcp_structured_verified" {
		if err := s.checkHost(ctx, options); err != nil {
			return CoreCallResult{}, err
		}
	}
	op, err := s.registry.Lookup(contracts.MCP, name)
	if err != nil {
		return CoreCallResult{}, ErrCoreUnsupported
	}
	if len(raw) > op.RequestHardLimit() {
		return s.failure(ctx, name, options, ErrCoreInput)
	}
	raw = append([]byte(nil), raw...)
	if err := ctx.Err(); err != nil {
		return s.failure(ctx, name, options, err)
	}
	principal, err := s.authority.Authenticate(ctx)
	if err != nil || !validCorePrincipal(principal) {
		return s.failure(ctx, name, options, coreContextError(ctx, ErrCoreAuthentication))
	}
	request, err := s.registry.DecodeRequest(contracts.MCP, name, raw, op.RequestHardLimit(), 4)
	if err != nil {
		return s.failure(ctx, name, options, coreContextError(ctx, ErrCoreInput))
	}
	if _, ok := request.(*model.RenameContinueRequest); ok {
		continued, cancelContinue := context.WithDeadline(ctx, started.Add(time.Duration(s.config.Core.RenameTimeoutMs)*time.Millisecond))
		defer cancelContinue()
		ctx = continued
	}
	options.MaxOutputBytes = requestOutputCap(request, options.MaxOutputBytes)
	if err := s.checkHost(ctx, options); err != nil {
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	if err := s.authorizeRequest(ctx, principal, name, raw, op.RequestHardLimit()); err != nil {
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	blocks, err := s.dispatch(ctx, principal, request, options)
	if err != nil {
		var described *oversizedEvidenceError
		if errors.As(err, &described) && described != nil {
			if described.release != nil {
				defer described.release()
			}
			return s.deliverOversized(ctx, name, raw, op.RequestHardLimit(), principal, request, options, described)
		}
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	if err := ctx.Err(); err != nil {
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	blocks, response, err := s.verifyBlocks(name, blocks, options, request)
	if err != nil {
		return s.failure(ctx, name, options, err)
	}
	if response.Status == "error" || response.Status == "blocked" {
		// Backends return typed errors; arbitrary backend messages are never
		// forwarded as public error details by this boundary.
		return s.failure(ctx, name, options, ErrCoreBackend)
	}
	for i := 0; i < 2; i++ {
		copyResponse, err := s.registry.DecodeResponse(contracts.MCP, name, blocks[0], int(options.MaxOutputBytes), 4)
		copyRequest, requestErr := s.registry.DecodeRequest(contracts.MCP, name, raw, op.RequestHardLimit(), 4)
		if err != nil || requestErr != nil || s.authority.AuthorizeResponse(ctx, principal, name, copyRequest, copyResponse) != nil {
			return s.failure(ctx, name, options, coreRequestError(ctx, request, ErrValidationAuthority))
		}
		if err := s.authorizeRequest(ctx, principal, name, raw, op.RequestHardLimit()); err != nil {
			return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
		}
	}
	finalResponse, finalResponseErr := s.registry.DecodeResponse(contracts.MCP, name, blocks[0], int(options.MaxOutputBytes), 4)
	finalRequest, finalRequestErr := s.registry.DecodeRequest(contracts.MCP, name, raw, op.RequestHardLimit(), 4)
	if finalResponseErr != nil || finalRequestErr != nil {
		return s.failure(ctx, name, options, ErrCoreBackend)
	}
	if err := s.backend.VerifyDelivery(ctx, principal, name, finalRequest, finalResponse); err != nil {
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	if err := s.checkHost(ctx, options); err != nil {
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	if err := ctx.Err(); err != nil {
		return s.failure(ctx, name, options, coreRequestError(ctx, request, err))
	}
	return CoreCallResult{Blocks: blocks}, nil
}

func (s *CoreService) normalizeOptions(options CoreCallOptions) (CoreCallOptions, error) {
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = s.config.Core.MaxOutputBytes
	}
	if options.MaxOutputBytes < 4096 || options.MaxOutputBytes > 131072 {
		return CoreCallOptions{}, ErrCoreInput
	}
	if options.MaxOutputBytes > s.config.Core.MaxOutputBytes {
		options.MaxOutputBytes = s.config.Core.MaxOutputBytes
	}
	if options.DeliveryMode == "" {
		options.DeliveryMode = "mcp_dual"
	}
	switch options.DeliveryMode {
	case "http", "cli", "mcp_text", "mcp_dual", "mcp_structured_verified":
	default:
		return CoreCallOptions{}, ErrCoreInput
	}
	return options, nil
}

func (s *CoreService) authorizeRequest(ctx context.Context, p CorePrincipal, name string, raw []byte, cap int) error {
	// Decode afresh for each callback so a callback cannot mutate the request
	// that is later dispatched or passed to a subsequent authority check.
	request, err := s.registry.DecodeRequest(contracts.MCP, name, raw, cap, 4)
	if err != nil {
		return ErrCoreInput
	}
	if err := s.authority.AuthorizeRequest(ctx, p, name, request); err != nil {
		return coreContextError(ctx, ErrValidationAuthority)
	}
	return ctx.Err()
}

func (s *CoreService) dispatch(ctx context.Context, p CorePrincipal, request model.Request, o CoreCallOptions) ([][]byte, error) {
	switch r := request.(type) {
	case model.StatusRequest:
		return s.backend.WorkspaceStatus(ctx, p, r, o)
	case model.LocateRequest:
		return s.backend.Locate(ctx, p, r, o)
	case model.PackRequest:
		return s.backend.ContextPack(ctx, p, r, o)
	case model.ExpandRequest:
		return s.backend.ExpandPack(ctx, p, r, o)
	case model.ValidateRequest:
		return s.backend.ValidatePack(ctx, p, r, o)
	case *model.RenameInitialRequest, *model.RenameContinueRequest:
		return s.backend.RenamePreview(ctx, p, r, o)
	default:
		return nil, ErrCoreUnsupported
	}
}

func (s *CoreService) verifyBlocks(name string, input [][]byte, options CoreCallOptions, request model.Request) ([][]byte, model.Response, error) {
	want := 1
	if options.DeliveryMode == "mcp_dual" {
		want = 2
	}
	if len(input) != want {
		return nil, model.Response{}, ErrCoreBackend
	}
	blocks := make([][]byte, len(input))
	var total int64
	for i, block := range input {
		if len(block) == 0 || int64(len(block)) > options.MaxOutputBytes-total {
			return nil, model.Response{}, ErrCoreBackend
		}
		total += int64(len(block))
		blocks[i] = append([]byte(nil), block...)
	}
	if len(blocks) == 2 && !bytes.Equal(blocks[0], blocks[1]) {
		return nil, model.Response{}, ErrCoreBackend
	}
	response, err := s.registry.DecodeResponse(contracts.MCP, name, blocks[0], int(options.MaxOutputBytes), 4)
	if err != nil || response.RequestID != options.RequestID || response.Budget.DeliveryMode != options.DeliveryMode || response.Budget.MaxOutputBytes > options.MaxOutputBytes || model.ValidateDeliveredBytes(response.Budget, blocks) != nil {
		return nil, model.Response{}, ErrCoreBackend
	}
	if packRequest, ok := request.(model.PackRequest); ok && model.ValidatePackResponseSemantics(packRequest, response) != nil {
		return nil, model.Response{}, ErrCoreBackend
	}
	if packRequest, ok := request.(model.PackRequest); ok && packRequest.FreshnessMode == "analysis_snapshot" && response.Data != nil && (response.Readiness != "needs_context" || response.Freshness.Scope != "none" || response.Freshness.CheckedAt != nil || response.Freshness.ChangedCount != nil || response.Freshness.SourceView != "indexed_snapshot") {
		return nil, model.Response{}, ErrCoreBackend
	}
	if expand, ok := request.(model.ExpandRequest); ok && response.Data != nil {
		pack, ok := response.Data.(model.PackData)
		if !ok || pack.PackToken != expand.PackToken {
			return nil, model.Response{}, ErrCoreBackend
		}
	}
	if err := validateCoreResponseScope(response, int(options.MaxOutputBytes)); err != nil {
		return nil, model.Response{}, ErrCoreBackend
	}
	return blocks, response, nil
}

func (s *CoreService) checkHost(ctx context.Context, options CoreCallOptions) error {
	if options.DeliveryMode == "mcp_structured_verified" && (options.VerifyStructuredHost == nil || options.VerifyStructuredHost(ctx) != nil) {
		return coreContextError(ctx, ErrValidationAuthority)
	}
	return ctx.Err()
}

func requestOutputCap(request model.Request, maximum int64) int64 {
	var cap *int64
	switch r := request.(type) {
	case model.PackRequest:
		cap = r.MaxOutputBytes
	case model.ExpandRequest:
		cap = r.MaxOutputBytes
	case *model.RenameInitialRequest:
		cap = r.MaxOutputBytes
	case *model.RenameContinueRequest:
		cap = r.MaxOutputBytes
	}
	if cap != nil && *cap < maximum {
		return *cap
	}
	return maximum
}

func validCorePrincipal(p CorePrincipal) bool {
	return p.ID != "" && len(p.ID) <= 1024 && utf8.ValidString(p.ID) && coreHash(p.Digest) && coreHash(p.BindingDigest)
}

func coreHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range s {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func coreUUID(s string) bool {
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
		} else if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func coreContextError(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fallback
}

func coreRequestError(ctx context.Context, request model.Request, fallback error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if pack, ok := request.(model.PackRequest); ok && pack.FreshnessMode == "checked_workspace" {
			return ErrFreshnessUnknown
		}
		if status, ok := request.(model.StatusRequest); ok && status.Verify != nil && *status.Verify == "workspace" {
			return ErrFreshnessUnknown
		}
	}
	return coreContextError(ctx, fallback)
}

var (
	ErrCoreInput           = errors.New("invalid core service input")
	ErrCoreAuthentication  = errors.New("core authentication unavailable")
	ErrCoreUnsupported     = errors.New("core capability unavailable")
	ErrCoreBackend         = errors.New("invalid core backend response")
	ErrCoreIndexMissing    = errors.New("published index unavailable")
	ErrCoreSnapshotExpired = errors.New("pinned snapshot expired")
	ErrCoreResultExpired   = errors.New("retained result unavailable")
	ErrCoreStoreChanged    = errors.New("store identity changed")
	ErrCoreStoreRecovery   = errors.New("store recovery required")
	ErrCoreLimit           = errors.New("core resource limit reached")
	ErrCoreAmbiguous       = errors.New("symbol selection is ambiguous")
)

// marshalCoreResponse is used only for bounded, sanitized service errors.
func marshalCoreResponse(response model.Response, name string, registry *contracts.Registry) (CoreCallResult, error) {
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
		return CoreCallResult{}, ErrCoreBackend
	}
	if _, err := registry.DecodeResponse(contracts.MCP, name, blocks[0], int(budget.MaxOutputBytes), 4); err != nil {
		return CoreCallResult{}, ErrCoreBackend
	}
	return CoreCallResult{Blocks: blocks, IsError: true}, nil
}
