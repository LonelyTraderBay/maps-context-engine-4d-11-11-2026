package app

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

type planArchiveMetadata struct {
	Version                   int             `json:"version"`
	Request                   json.RawMessage `json:"request"`
	Coverage                  json.RawMessage `json:"coverage"`
	DeclarationRevisionDigest string          `json:"declaration_revision_digest"`
}

// FreshnessPlanRestoreInput contains current trusted callbacks. The archive is
// not a live freshness verdict and never supplies its own authorization.
type FreshnessPlanRestoreInput struct {
	Plan         overview.PlanRestoreInput
	PrincipalID  string
	Scanner      *policy.Scanner
	Validation   ValidateCapturedInput
	CaptureLive  CaptureLiveSource
	RequestLimit int
	// Independently obtained from the current declaration authority.
	DeclarationRevisionDigest string
}

type retainedPlanRequest struct {
	request        model.PackRequest
	revisionDigest string
}

// authenticatedRetainedPlanRequest is the only request passed to a restore
// factory. It verifies the signed bytes, actor and original request digest
// before an owner uses the request to select declaration authority.
func authenticatedRetainedPlanRequest(ctx context.Context, raw []byte, codec *contextpack.CursorCodec, token, principal, actor string, claims contextpack.SignedCursor, guard contextpack.CursorGuard, maxBytes int) (retainedPlanRequest, error) {
	var empty retainedPlanRequest
	if ctx == nil || codec == nil || guard == nil || actor == "" || len(actor) > 4096 {
		return empty, ErrValidationInput
	}
	metadataRaw, err := overview.ReadPlanArchiveMetadata(ctx, raw, codec, token, principal, maxBytes, guard)
	if err != nil {
		return empty, packContextError(ctx, err)
	}
	var metadata planArchiveMetadata
	decoder := json.NewDecoder(bytes.NewReader(metadataRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&metadata) != nil {
		return empty, ErrValidationInput
	}
	canonical, err := json.Marshal(metadata)
	if err != nil || !bytes.Equal(canonical, metadataRaw) || metadata.Version != 2 || !coreHash(metadata.DeclarationRevisionDigest) || len(metadata.Request) > 65536 || len(metadata.Coverage) > 1024*1024 {
		return empty, ErrValidationInput
	}
	request, err := model.DecodePackRequestJSON(metadata.Request, 65536)
	if err != nil || request.View == nil || *request.View != "overview" || request.TaskKind != "plan" && request.TaskKind != "explore" {
		return empty, ErrValidationInput
	}
	requestJSON, err := json.Marshal(request)
	if err != nil || !bytes.Equal(requestJSON, metadata.Request) {
		return empty, ErrValidationInput
	}
	if _, err := model.DecodeCoverageJSON(metadata.Coverage, 1024*1024); err != nil {
		return empty, ErrValidationInput
	}
	// The caller authenticated these claims for this exact pack token.
	// ReadPlanArchiveMetadata also checks the matching signed token pair.
	digest, err := model.RequestDigest("context_pack", actor, "PackRequest", request, 65536, 4)
	if err != nil || digest != claims.Binding.RequestDigest {
		return empty, ErrValidationAuthority
	}
	if err := codec.Recheck(ctx, claims, sanitizedPackGuard(guard)); err != nil {
		return empty, packContextError(ctx, err)
	}
	return retainedPlanRequest{request: request, revisionDigest: metadata.DeclarationRevisionDigest}, nil
}

// ArchiveEnvelope binds the original normalized request and coverage to the
// already issued plan. Its caller is responsible for secure bounded storage.
func (p *FreshnessPlan) ArchiveEnvelope(ctx context.Context, principalID string, requestJSON []byte, ttlSeconds int64, maxBytes int) (RetainedPackEnvelope, error) {
	var empty RetainedPackEnvelope
	if ctx == nil || p == nil || p.pager == nil || p.codec == nil || p.guard == nil || principalID == "" || len(principalID) > 4096 || len(requestJSON) > 65536 || maxBytes < 1024 || maxBytes > maxPackEnvelope {
		return empty, ErrValidationInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !coreHash(p.declarationRevisionDigest) {
		return empty, ErrValidationAuthority
	}
	request, err := model.DecodePackRequestJSON(requestJSON, 65536)
	if err != nil || request.View == nil || *request.View != "overview" || request.TaskKind != p.taskKind || request.FreshnessMode != p.mode {
		return empty, ErrValidationInput
	}
	digest, err := model.RequestDigest("context_pack", principalID, "PackRequest", request, 65536, 4)
	if err != nil || digest != p.packBinding.RequestDigest {
		return empty, ErrValidationInput
	}
	requestRaw, err := json.Marshal(request)
	if err != nil || len(requestRaw) > 65536 {
		return empty, ErrValidationInput
	}
	coverageRaw, err := json.Marshal(p.coverage)
	if err != nil || len(coverageRaw) > 1024*1024 {
		return empty, ErrValidationInput
	}
	metadata, err := json.Marshal(planArchiveMetadata{Version: 2, Request: requestRaw, Coverage: coverageRaw, DeclarationRevisionDigest: p.declarationRevisionDigest})
	if err != nil || len(metadata) > 1024*1024+65536 {
		return empty, ErrValidationInput
	}
	// The pager checks its source/document owner while sealing private bytes;
	// the full application guard is checked after sealing before return.
	raw, err := p.pager.ArchiveForRetention(ctx, p.token, ttlSeconds, metadata, maxBytes)
	if err != nil {
		return empty, packContextError(ctx, err)
	}
	if err := p.recheck(ctx, "", p.principal, nil); err != nil {
		return empty, packContextError(ctx, err)
	}
	return RetainedPackEnvelope{Token: p.token, PrincipalDigest: p.principal, Bytes: raw}, nil
}

// RestoreFreshnessPlanEnvelope restores the same plan and token. It validates
// the original request against its signed digest and installs new authority
// and workspace callbacks; it never resolves or builds a replacement result.
func RestoreFreshnessPlanEnvelope(ctx context.Context, raw []byte, in FreshnessPlanRestoreInput) (*FreshnessPlan, error) {
	if ctx == nil || in.Plan.Codec == nil || in.Plan.Guard == nil || in.Scanner == nil || in.PrincipalID == "" || len(in.PrincipalID) > 4096 || in.RequestLimit < 1024 || in.RequestLimit > 65536 || !coreHash(in.DeclarationRevisionDigest) {
		return nil, ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrValidationInput
	}
	restored, err := overview.RestorePlanPager(ctx, raw, in.Plan)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	var metadata planArchiveMetadata
	decoder := json.NewDecoder(bytes.NewReader(restored.Metadata))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&metadata) != nil {
		return nil, ErrValidationInput
	}
	canonical, err := json.Marshal(metadata)
	if err != nil || !bytes.Equal(canonical, restored.Metadata) || metadata.Version != 2 || !coreHash(metadata.DeclarationRevisionDigest) || metadata.DeclarationRevisionDigest != in.DeclarationRevisionDigest || len(metadata.Request) > in.RequestLimit || len(metadata.Coverage) > 1024*1024 {
		return nil, ErrValidationInput
	}
	request, err := model.DecodePackRequestJSON(metadata.Request, in.RequestLimit)
	if err != nil || request.View == nil || *request.View != "overview" || request.TaskKind != "plan" && request.TaskKind != "explore" {
		return nil, ErrValidationInput
	}
	requestJSON, err := json.Marshal(request)
	if err != nil || !bytes.Equal(requestJSON, metadata.Request) {
		return nil, ErrValidationInput
	}
	guard := sanitizedPackGuard(in.Plan.Guard)
	packClaims, err := in.Plan.Codec.VerifyPrincipal(ctx, in.Plan.PackToken, "pack", in.Plan.PrincipalDigest, guard)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	listClaims, err := in.Plan.Codec.VerifyPrincipal(ctx, restored.Pager.Anchor(), "list", in.Plan.PrincipalDigest, guard)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	digest, err := model.RequestDigest("context_pack", in.PrincipalID, "PackRequest", request, in.RequestLimit, 4)
	scope := packClaims.Binding.Scope
	if err != nil || digest != packClaims.Binding.RequestDigest || scope.Kind != "single" || len(scope.Members) != 1 || len(scope.UnavailableMembers) != 0 || !requestMatchesScope(request.Scope, scope, scope.Members[0], in.RequestLimit) || in.Validation.Scope.Kind != "" && model.ComparePinnedScope(in.Validation.Scope, scope, in.RequestLimit) != nil || in.Validation.MaxInputBytes < 1024 || in.Validation.MaxInputBytes > 1024*1024 || in.Validation.MaxManifestBytes < 1024 || in.Validation.MaxManifestBytes > 64*1024*1024 {
		return nil, ErrValidationInput
	}
	if request.FreshnessMode == "checked_workspace" && (in.CaptureLive == nil || in.Validation.CurrentScope == nil || in.Validation.ResolveCapture == nil || in.Validation.AuthorizeSource == nil || in.Validation.VerifyWorkspace == nil) {
		return nil, ErrFreshnessUnknown
	}
	selector, err := restored.Plan.Selector()
	if err != nil {
		return nil, ErrValidationInput
	}
	actualSelector, errA := json.Marshal(selector)
	requestedSelector, errB := json.Marshal(request.Selector)
	if errA != nil || errB != nil || !bytes.Equal(actualSelector, requestedSelector) {
		return nil, ErrValidationInput
	}
	report, err := restored.Plan.Read(ctx)
	if err != nil {
		return nil, packContextError(ctx, err)
	}
	member := scope.Members[0]
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	if report.Scope.Identity != identity || report.Scope.SnapshotDigest != member.SnapshotDigest || report.Scope.PolicyDigest != member.PolicyDigest || report.Scope.PolicyEpoch != member.PolicyEpoch || report.TaskKind != request.TaskKind {
		return nil, ErrValidationPrecondition
	}
	intent := ""
	if request.Intent != nil {
		intent = *request.Intent
	}
	constraints := append([]string{}, request.Constraints...)
	sort.Strings(constraints)
	if report.Intent != intent || len(report.Constraints) != len(constraints) {
		return nil, ErrValidationInput
	}
	for i := range constraints {
		if constraints[i] != report.Constraints[i] {
			return nil, ErrValidationInput
		}
	}
	coverage, err := model.DecodeCoverageJSON(metadata.Coverage, 1024*1024)
	if err != nil || in.Scanner.CheckContent(metadata.Coverage) != nil || scanPlanCoverage(in.Scanner, coverage) != nil {
		return nil, ErrValidationInput
	}
	targets, err := planTargets(request.Selector, report, identity)
	if err != nil {
		return nil, err
	}
	validation := in.Validation
	validation.Scope, validation.Sources = scope, nil
	firstMax := int64(0)
	if request.MaxOutputBytes != nil {
		firstMax = *request.MaxOutputBytes
	}
	result := &FreshnessPlan{plan: restored.Plan, pager: restored.Pager, codec: in.Plan.Codec, guard: guard, packBinding: packClaims.Binding, listBinding: listClaims.Binding,
		token: in.Plan.PackToken, principal: in.Plan.PrincipalDigest, mode: request.FreshnessMode, taskKind: request.TaskKind, firstMax: firstMax,
		coverage: coverage, scanner: in.Scanner, targets: targets, validation: validation, capture: in.CaptureLive, declarationRevisionDigest: in.DeclarationRevisionDigest}
	if err := result.recheck(ctx, "", result.principal, nil); err != nil {
		return nil, packContextError(ctx, err)
	}
	return result, nil
}

// ProveExpiredPlanEnvelope supplies only signed ownership/expiry evidence;
// storage must separately require GC authorization and the writer lease.
func ProveExpiredPlanEnvelope(ctx context.Context, id string, raw []byte, codec *contextpack.CursorCodec, maxBytes int) (bool, error) {
	if ctx == nil || codec == nil {
		return false, ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return false, ErrValidationInput
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	proof, err := codec.InspectPlanMaterialization(raw, maxBytes)
	if err != nil || proof.ResultID != id {
		return false, ErrValidationAuthority
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return proof.Expired, nil
}
