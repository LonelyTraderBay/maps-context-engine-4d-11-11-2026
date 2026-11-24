package app

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// FreshnessPackRestoreInput is supplied by the trusted owner after loading an
// authenticated archive. RequestJSON is checked against the original signed
// request digest, not trusted as authority or as a live freshness result.
type FreshnessPackRestoreInput struct {
	Archive         []byte
	Pack            contextpack.PackRestoreInput
	RequestJSON     []byte
	PrincipalID     string
	ServiceID       *string
	Validation      ValidateCapturedInput
	CaptureLive     CaptureLiveSource
	ResolvePosition ResolvePackPosition
	RequestLimit    int
}

// RestoreFreshnessPack keeps the original pack and mode while installing fresh
// authority and workspace callbacks. It cannot create a missing archive, use
// an active generation, or turn an analysis snapshot into a checked result.
// The storage owner must enforce its own quota and TTL before calling this.
func RestoreFreshnessPack(ctx context.Context, in FreshnessPackRestoreInput) (*FreshnessPack, error) {
	if ctx == nil || in.Pack.Codec == nil || in.Pack.Guard == nil || in.PrincipalID == "" || len(in.PrincipalID) > 4096 || in.RequestLimit < 1024 || in.RequestLimit > 65536 || len(in.RequestJSON) > in.RequestLimit || in.ServiceID != nil && len(*in.ServiceID) > 768 {
		return nil, ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrValidationInput
	}
	request, err := model.DecodePackRequestJSON(in.RequestJSON, in.RequestLimit)
	if err != nil || request.View != nil && *request.View != "symbol" {
		return nil, ErrValidationInput
	}
	guard := sanitizedPackGuard(in.Pack.Guard)
	claims, err := in.Pack.Codec.VerifyPrincipal(ctx, in.Pack.Token, "pack", in.Pack.PrincipalDigest, guard)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	digest, err := model.RequestDigest("context_pack", in.PrincipalID, "PackRequest", request, in.RequestLimit, 4)
	if err != nil || digest != claims.Binding.RequestDigest {
		return nil, ErrValidationInput
	}
	if in.Validation.MaxInputBytes < 1024 || in.Validation.MaxInputBytes > 1024*1024 || in.Validation.MaxManifestBytes < 1024 || in.Validation.MaxManifestBytes > 64*1024*1024 || !validationInputsWithinBudget(claims.Binding.Scope, nil, in.RequestLimit) || in.Validation.Scope.Kind != "" && model.ComparePinnedScope(claims.Binding.Scope, in.Validation.Scope, in.RequestLimit) != nil {
		return nil, ErrValidationInput
	}
	if request.FreshnessMode == "checked_workspace" && (in.CaptureLive == nil || in.Validation.CurrentScope == nil || in.Validation.ResolveCapture == nil || in.Validation.AuthorizeSource == nil || in.Validation.VerifyWorkspace == nil) {
		return nil, ErrFreshnessUnknown
	}
	restoreInput := in.Pack
	restoreInput.Guard = guard
	pager, err := contextpack.RestorePagedPack(ctx, in.Archive, restoreInput)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	slice := pager.RetainedTaskSlice()
	report, err := slice.Read(ctx)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	if report.TaskKind != request.TaskKind || model.ComparePinnedScope(report.Scope, claims.Binding.Scope, in.RequestLimit) != nil {
		return nil, ErrValidationInput
	}
	member, err := model.MatchPinnedMember(report.Scope, &report.Target, nil)
	if err != nil || in.ServiceID != nil && (member.ServiceID == nil || *member.ServiceID != *in.ServiceID) || !requestMatchesScope(request.Scope, report.Scope, member, in.RequestLimit) {
		return nil, ErrValidationPrecondition
	}
	intent := ""
	if request.Intent != nil {
		intent = *request.Intent
	}
	if report.Intent != intent || !sliceMatchesConstraints(report, request.Constraints) {
		return nil, ErrValidationInput
	}
	if position, ok := request.Selector.(*model.PositionSelector); ok {
		if position == nil || in.ResolvePosition == nil {
			return nil, ErrValidationAuthority
		}
		raw, err := json.Marshal(report.Scope)
		if err != nil || len(raw) > in.RequestLimit {
			return nil, ErrValidationInput
		}
		copyScope, err := model.DecodeScopeVectorJSON(raw, in.RequestLimit)
		if err != nil {
			return nil, ErrValidationInput
		}
		resolved, err := in.ResolvePosition(ctx, *position, copyScope, copyValidationService(in.ServiceID))
		if err != nil || resolved != report.Target {
			return nil, packContextError(ctx, ErrValidationPrecondition)
		}
	} else if !sliceMatchesSelector(report, request.Selector) {
		return nil, ErrValidationInput
	}
	validation := in.Validation
	validation.Scope = report.Scope
	validation.Sources = nil
	service := copyValidationService(in.ServiceID)
	if request.FreshnessMode == "checked_workspace" {
		validation.Sources, err = sliceValidationSources(report, service, validation.MaxInputBytes)
		if err != nil {
			return nil, err
		}
	}
	firstMax := int64(0)
	if request.MaxOutputBytes != nil {
		firstMax = *request.MaxOutputBytes
	}
	pack := &FreshnessPack{pack: pager, slice: slice, codec: in.Pack.Codec, guard: guard,
		principal: claims.Binding.PrincipalDigest, binding: claims.Binding.BindingDigest,
		request: claims.Binding.RequestDigest, token: in.Pack.Token, mode: request.FreshnessMode,
		firstMax: firstMax, service: service, validation: validation, capture: in.CaptureLive}
	if err := pack.recheck(ctx, pack.token, "", pack.principal, nil); err != nil {
		return nil, packContextError(ctx, err)
	}
	return pack, nil
}

// Archive delegates to the pack materialization; the caller must retain the
// initial request separately and verify its signed digest at restore time.
func (p *FreshnessPack) Archive(ctx context.Context, maxBytes int) ([]byte, error) {
	return p.ArchiveForRetention(ctx, maxBytes, 900)
}

// ArchiveForRetention preserves the original TTL even if a later process has
// a longer configured cache lifetime. It does not itself persist any bytes.
func (p *FreshnessPack) ArchiveForRetention(ctx context.Context, maxBytes int, ttlSeconds int64) ([]byte, error) {
	if p == nil || p.pack == nil {
		return nil, ErrValidationInput
	}
	if err := p.recheck(ctx, p.token, "", p.principal, nil); err != nil {
		return nil, packContextError(ctx, err)
	}
	raw, err := p.pack.ArchiveForRetention(ctx, maxBytes, ttlSeconds)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	if err := p.recheck(ctx, p.token, "", p.principal, nil); err != nil {
		return nil, packContextError(ctx, err)
	}
	return raw, nil
}
