package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// CaptureLiveSource must read the named file through an authorized, bounded
// secure-open adapter. It is a trust boundary, not a freshness cache lookup.
type CaptureLiveSource func(context.Context, model.ScopeMember, string) (snapshot.File, error)

// ResolvePackPosition must resolve the exact position against the pinned
// generation, with ambiguity rejected by the trusted search adapter.
type ResolvePackPosition func(context.Context, model.PositionSelector, model.ScopeVector, *string) (model.SymbolRef, error)

type FreshnessPackInput struct {
	Slice           *contextpack.TaskSlice
	Codec           *contextpack.CursorCodec
	Binding         contextpack.CursorBinding
	Guard           contextpack.CursorGuard
	RequestJSON     []byte // bounded raw request; decoded with documented defaults
	PrincipalID     string // authenticated identity, never client asserted
	ServiceID       *string
	Validation      ValidateCapturedInput
	CaptureLive     CaptureLiveSource
	ResolvePosition ResolvePackPosition
	RequestLimit    int
}

// FreshnessPack stores the immutable pack, not a live freshness verdict.
// Its owner must bound the number and lifetime of retained instances.
type FreshnessPack struct {
	pack       *contextpack.PagedPack
	slice      *contextpack.TaskSlice
	codec      *contextpack.CursorCodec
	guard      contextpack.CursorGuard
	principal  string
	binding    string
	request    string
	token      string
	mode       string
	firstMax   int64
	service    *string
	validation ValidateCapturedInput
	capture    CaptureLiveSource
}

// NewFreshnessPack binds the validated, defaulted request (including mode) to
// the signed request-v1 digest. The caller must authenticate the principal and
// resolve the requested selector and service against trusted authorities.
func NewFreshnessPack(ctx context.Context, in FreshnessPackInput) (*FreshnessPack, string, error) {
	if ctx == nil || in.Slice == nil || in.Codec == nil || in.Guard == nil || in.PrincipalID == "" || len(in.PrincipalID) > 4096 || in.RequestLimit < 1024 || in.RequestLimit > 65536 || len(in.RequestJSON) > in.RequestLimit || in.Binding.Purpose != "pack" || in.Binding.ResultID != "" || in.Binding.ResultDigest != "" {
		return nil, "", ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, "", ErrValidationInput
	}
	if !validationInputsWithinBudget(in.Binding.Scope, nil, in.RequestLimit) || in.Validation.Scope.Kind != "" && !validationInputsWithinBudget(in.Validation.Scope, nil, in.RequestLimit) || in.ServiceID != nil && len(*in.ServiceID) > 768 {
		return nil, "", ErrValidationInput
	}
	request, err := model.DecodePackRequestJSON(in.RequestJSON, in.RequestLimit)
	if err != nil {
		return nil, "", ErrValidationInput
	}
	digest, err := model.RequestDigest("context_pack", in.PrincipalID, "PackRequest", request, in.RequestLimit, 4)
	if err != nil || digest != in.Binding.RequestDigest {
		return nil, "", ErrValidationInput
	}
	safeGuard := sanitizedPackGuard(in.Guard)
	preToken, err := in.Codec.Issue(ctx, in.Binding, contextpack.SignedPosition{LastKey: []string{}}, safeGuard)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	preClaims, err := in.Codec.Verify(ctx, preToken, in.Binding, safeGuard)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	report, err := in.Slice.Read(ctx)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	if report.TaskKind != request.TaskKind || model.ComparePinnedScope(report.Scope, in.Binding.Scope, in.RequestLimit) != nil || in.Validation.Scope.Kind != "" && model.ComparePinnedScope(report.Scope, in.Validation.Scope, in.RequestLimit) != nil {
		return nil, "", ErrValidationInput
	}
	if !report.EnumerationComplete {
		return nil, "", contextpack.ErrContinuationUnavailable
	}
	// Until a service-aware selection exposes its member identity, reject
	// duplicate graph identities rather than guessing a policy across services.
	member, err := model.MatchPinnedMember(report.Scope, &report.Target, nil)
	if err != nil {
		return nil, "", ErrValidationPrecondition
	}
	if in.ServiceID != nil && (member.ServiceID == nil || *member.ServiceID != *in.ServiceID) || !requestMatchesScope(request.Scope, report.Scope, member, in.RequestLimit) {
		return nil, "", ErrValidationPrecondition
	}
	if request.FreshnessMode == "checked_workspace" && (in.CaptureLive == nil || in.Validation.CurrentScope == nil || in.Validation.ResolveCapture == nil || in.Validation.AuthorizeSource == nil || in.Validation.VerifyWorkspace == nil) {
		return nil, "", ErrFreshnessUnknown
	}
	intent := ""
	if request.Intent != nil {
		intent = *request.Intent
	}
	if report.Intent != intent || !sliceMatchesConstraints(report, request.Constraints) {
		return nil, "", ErrValidationInput
	}
	if position, ok := request.Selector.(*model.PositionSelector); ok {
		if position == nil || in.ResolvePosition == nil {
			return nil, "", ErrValidationAuthority
		}
		raw, err := json.Marshal(report.Scope)
		if err != nil || len(raw) > in.RequestLimit {
			return nil, "", ErrValidationInput
		}
		copyScope, err := model.DecodeScopeVectorJSON(raw, in.RequestLimit)
		if err != nil {
			return nil, "", ErrValidationInput
		}
		resolved, err := in.ResolvePosition(ctx, *position, copyScope, copyValidationService(in.ServiceID))
		if err != nil || resolved != report.Target {
			return nil, "", packContextError(ctx, ErrValidationPrecondition)
		}
	} else if !sliceMatchesSelector(report, request.Selector) {
		return nil, "", ErrValidationInput
	}
	if in.Validation.MaxInputBytes < 1024 || in.Validation.MaxInputBytes > 1024*1024 || in.Validation.MaxManifestBytes < 1024 || in.Validation.MaxManifestBytes > 64*1024*1024 {
		return nil, "", ErrValidationInput
	}
	service := copyValidationService(in.ServiceID)
	validation := in.Validation
	validation.Scope = report.Scope
	validation.Sources = nil
	if request.FreshnessMode == "checked_workspace" {
		sources, err := sliceValidationSources(report, service, in.Validation.MaxInputBytes)
		if err != nil {
			return nil, "", err
		}
		validation.Sources = sources
	}
	pager, token, err := contextpack.NewPagedPack(ctx, in.Slice, in.Codec, in.Binding, safeGuard)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	if err := in.Codec.Recheck(ctx, preClaims, safeGuard); err != nil {
		return nil, "", packContextError(ctx, err)
	}
	firstMax := int64(0)
	if request.MaxOutputBytes != nil {
		firstMax = *request.MaxOutputBytes
	}
	return &FreshnessPack{pack: pager, slice: in.Slice, codec: in.Codec, guard: safeGuard, principal: in.Binding.PrincipalDigest, binding: in.Binding.BindingDigest, request: in.Binding.RequestDigest, token: token, mode: request.FreshnessMode, firstMax: firstMax, service: service, validation: validation, capture: in.CaptureLive}, token, nil
}

// First performs a fresh full workspace verification for checked mode. In
// analysis mode it never asks the live-workspace or live-file adapters.
func (p *FreshnessPack) First(ctx context.Context, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p == nil || ctx == nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	if p.firstMax > 0 && (options.MaxOutputBytes == 0 || options.MaxOutputBytes > p.firstMax) {
		options.MaxOutputBytes = p.firstMax
	}
	page, err := p.pack.First(ctx, options)
	if err != nil {
		return contextpack.PackPage{}, p.describeOversized(ctx, err, "", p.principal, p.guard, options, true)
	}
	if err := p.checkHistoricalPage(page, "context_pack"); err != nil {
		return contextpack.PackPage{}, err
	}
	if p.mode == "checked_workspace" {
		validation, err := ValidateCapturedWorkspace(ctx, p.validation)
		if err != nil {
			return contextpack.PackPage{}, err
		}
		page.Response.Freshness = validation.Freshness
		// Even a current observation is not a durable authorization to edit.
		page.Response.Readiness = "needs_context"
		refreshPageWarnings(&page.Response)
		page, err = resettleFreshnessPage(page, "context_pack")
		if err != nil {
			return contextpack.PackPage{}, err
		}
	}
	if err := p.recheck(ctx, p.token, "", p.principal, p.guard); err != nil {
		return contextpack.PackPage{}, packContextError(ctx, err)
	}
	if err := recheckStructuredHost(ctx, options); err != nil {
		return contextpack.PackPage{}, err
	}
	return page, nil
}

// Continue keeps the original generation. Checked mode rechecks every file
// represented by this page; analysis mode remains an indexed snapshot.
func (p *FreshnessPack) Continue(ctx context.Context, token, cursor, principal string, currentGuard contextpack.CursorGuard, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p == nil || ctx == nil || currentGuard == nil || token != p.token || principal != p.principal {
		return contextpack.PackPage{}, ErrValidationInput
	}
	safeCurrent := sanitizedPackGuard(currentGuard)
	page, err := p.pack.Continue(ctx, token, cursor, principal, safeCurrent, options)
	if err != nil {
		return contextpack.PackPage{}, p.describeOversized(ctx, err, cursor, principal, safeCurrent, options, false)
	}
	if err := p.checkHistoricalPage(page, "expand_pack"); err != nil {
		return contextpack.PackPage{}, err
	}
	if p.mode == "checked_workspace" {
		sources, err := pageValidationSources(page.Response, p.service, p.validation.MaxInputBytes)
		if err != nil {
			return contextpack.PackPage{}, err
		}
		if len(sources) > 0 {
			freshness, err := validateReturnedFiles(ctx, p.validation, sources, p.capture)
			if err != nil {
				return contextpack.PackPage{}, err
			}
			page.Response.Freshness = freshness
			refreshPageWarnings(&page.Response)
		}
		page.Response.Readiness = "needs_context"
		page, err = resettleFreshnessPage(page, "expand_pack")
		if err != nil {
			return contextpack.PackPage{}, err
		}
	}
	if err := p.recheck(ctx, token, cursor, principal, safeCurrent); err != nil {
		return contextpack.PackPage{}, packContextError(ctx, err)
	}
	if err := recheckStructuredHost(ctx, options); err != nil {
		return contextpack.PackPage{}, err
	}
	return page, nil
}

func (p *FreshnessPack) checkHistoricalPage(page contextpack.PackPage, operation string) error {
	data, ok := page.Response.Data.(model.PackData)
	if !ok || data.Operation != operation || data.PackToken != p.token || data.EditPreconditionValidated || page.Response.Scope == nil || model.ComparePinnedScope(p.validation.Scope, *page.Response.Scope, p.validation.MaxInputBytes) != nil ||
		page.Response.Readiness != "needs_context" || page.Response.Freshness.Status != "not_checked" || page.Response.Freshness.Scope != "none" || page.Response.Freshness.CheckedAt != nil || page.Response.Freshness.ChangedCount != nil || page.Response.Freshness.SourceView != "indexed_snapshot" || page.Response.Freshness.BufferScope != "disk_only" || page.Response.Freshness.Consistency != "captured_bytes_with_observed_stability" {
		return ErrValidationInput
	}
	return nil
}

// Validate always repeats full membership/content/configuration verification.
// The result is an observation, not a filesystem lock or permission to apply.
func (p *FreshnessPack) Validate(ctx context.Context, token, principal string, currentGuard contextpack.CursorGuard) (CapturedValidation, error) {
	if p == nil || ctx == nil || currentGuard == nil || token != p.token || principal != p.principal {
		return CapturedValidation{}, ErrValidationInput
	}
	safeCurrent := sanitizedPackGuard(currentGuard)
	if err := p.recheck(ctx, token, "", principal, safeCurrent); err != nil {
		return CapturedValidation{}, packContextError(ctx, err)
	}
	if p.validation.CurrentScope == nil || p.validation.ResolveCapture == nil || p.validation.AuthorizeSource == nil || p.validation.VerifyWorkspace == nil {
		return CapturedValidation{}, ErrFreshnessUnknown
	}
	report, err := p.slice.Read(ctx)
	if err != nil {
		return CapturedValidation{}, packContextError(ctx, err)
	}
	sources, err := sliceValidationSources(report, p.service, p.validation.MaxInputBytes)
	if err != nil {
		return CapturedValidation{}, err
	}
	validation := p.validation
	validation.Sources = sources
	result, err := ValidateCapturedWorkspace(ctx, validation)
	if err != nil {
		return CapturedValidation{}, err
	}
	if err := p.recheck(ctx, token, "", principal, safeCurrent); err != nil {
		return CapturedValidation{}, packContextError(ctx, err)
	}
	return result, nil
}

func refreshPageWarnings(response *model.Response) {
	for i := range response.Warnings {
		if response.Warnings[i].Code != "LIVE_FRESHNESS_NOT_CHECKED" {
			continue
		}
		response.Warnings[i].Code = "LIVE_FRESHNESS_OBSERVATION_ONLY"
		response.Warnings[i].Reason = "Freshness is an observation; repeat full workspace validation before editing."
	}
}

func packContextError(ctx context.Context, err error) error {
	if cause := validationContextError(ctx); cause != nil {
		return cause
	}
	return err
}

func sanitizedPackGuard(guard contextpack.CursorGuard) contextpack.CursorGuard {
	return func(ctx context.Context, binding contextpack.CursorBinding) error {
		if err := guard(ctx, binding); err != nil {
			return packContextError(ctx, ErrValidationAuthority)
		}
		return validationContextError(ctx)
	}
}

func recheckStructuredHost(ctx context.Context, options contextpack.PackPageOptions) error {
	if options.DeliveryMode != "mcp_structured_verified" {
		return validationContextError(ctx)
	}
	if options.VerifyStructuredHost == nil || options.VerifyStructuredHost(ctx) != nil {
		return packContextError(ctx, ErrValidationAuthority)
	}
	return validationContextError(ctx)
}

func (p *FreshnessPack) recheck(ctx context.Context, token, cursor, principal string, currentGuard contextpack.CursorGuard) error {
	check := func(ctx context.Context, b contextpack.CursorBinding) error {
		if err := p.guard(ctx, b); err != nil {
			return err
		}
		return currentGuard(ctx, b)
	}
	claims, err := p.codec.VerifyPrincipal(ctx, token, "pack", principal, check)
	if err != nil || claims.Binding.ResultID != p.pack.ID() {
		if err != nil {
			return err
		}
		return ErrValidationAuthority
	}
	if cursor != "" {
		if _, err := p.codec.VerifyPrincipal(ctx, cursor, "list", principal, check); err != nil {
			return err
		}
	}
	if _, err := p.slice.Read(ctx); err != nil {
		return err
	}
	if err := p.codec.Recheck(ctx, claims, check); err != nil {
		return err
	}
	return validationContextError(ctx)
}

// retainedAuthority authenticates already verified immutable archive bytes at a
// storage boundary. The retention owner must still call RecheckRetained after
// the storage operation, before returning a handle or any page. It avoids
// recursively reading the entire selection in every byte-verifier callback.
func (p *FreshnessPack) retainedAuthority(ctx context.Context, token, principal string, current contextpack.CursorGuard) error {
	if p == nil || ctx == nil || current == nil || p.token != token || p.principal != principal {
		return ErrValidationAuthority
	}
	check := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if err := p.guard(ctx, binding); err != nil {
			return err
		}
		return current(ctx, binding)
	}
	claims, err := p.codec.VerifyPrincipal(ctx, token, "pack", principal, check)
	if err != nil {
		return err
	}
	if claims.Binding.ResultID != p.pack.ID() {
		return ErrValidationAuthority
	}
	return p.codec.Recheck(ctx, claims, check)
}

// Re-serialize after changing freshness; never emit a page over the original
// exact budget. A too-small budget fails explicitly, with no partial output.
func resettleFreshnessPage(page contextpack.PackPage, operation string) (contextpack.PackPage, error) {
	response := &page.Response
	estimate := int64(0)
	for i := 0; i < 8; i++ {
		response.Budget.SerializedBytes = estimate
		raw, err := json.Marshal(response)
		if err != nil {
			return contextpack.PackPage{}, ErrValidationInput
		}
		count := int64(len(raw))
		if response.Budget.DeliveryMode == "mcp_dual" {
			count *= 2
		}
		if count == estimate {
			if count > response.Budget.MaxOutputBytes {
				return contextpack.PackPage{}, contextpack.ErrBudgetTooSmall
			}
			blocks := [][]byte{raw}
			if response.Budget.DeliveryMode == "mcp_dual" {
				blocks = append(blocks, append([]byte(nil), raw...))
			}
			if _, err := model.DecodeResponseJSON(raw, int(response.Budget.MaxOutputBytes), 4, operation, false); err != nil || model.ValidateDeliveredBytes(response.Budget, blocks) != nil {
				return contextpack.PackPage{}, ErrValidationInput
			}
			page.Blocks = blocks
			return page, nil
		}
		estimate = count
	}
	return contextpack.PackPage{}, errors.New("freshness response byte accounting did not converge")
}
