package overview

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const planArchiveMax = 32 * 1024 * 1024
const planArchiveMetadataMax = 1024*1024 + 65536

// The archive contains only the selected plan and the authority inputs needed
// to recheck it. The full captured snapshot remains with the state owner.
type planArchiveBody struct {
	Version         int                   `json:"version"`
	Report          PlanReport            `json:"report"`
	Declarations    []DocumentDeclaration `json:"declarations"`
	OverviewPaths   []string              `json:"overview_paths"`
	FactsDigest     string                `json:"facts_digest"`
	Selector        json.RawMessage       `json:"selector"`
	MaxBytes        int                   `json:"max_bytes"`
	PolicyMaxOutput int64                 `json:"policy_max_output"`
	Metadata        []byte                `json:"metadata"`
}

// PlanRestoreInput must come from a trusted owner that reopens the exact
// pinned snapshot and independently rechecks principal, facts, source paths,
// and explicit document declarations. The archive itself grants no access.
type PlanRestoreInput struct {
	Codec              *contextpack.CursorCodec
	PackToken          string
	PrincipalDigest    string
	Guard              contextpack.CursorGuard
	Snapshot           *snapshot.Snapshot
	AuthorizeScope     func(context.Context, Scope) error
	AuthorizePath      func(context.Context, Scope, string) error
	VerifyFacts        func(context.Context, Scope, string) error
	AuthorizeDocument  func(context.Context, Scope, DocumentDeclaration) error
	VerifyDeclarations func(context.Context, Scope, []DocumentDeclaration) error
	MaxArchiveBytes    int
}

type RestoredPlan struct {
	Plan      *Plan
	Pager     *PlanPager
	Metadata  []byte
	ExpiresAt int64
}

// ReadPlanArchiveMetadata returns only authenticated application metadata.
// It allows the owner to select current declaration authority using the
// original request before any plan content is restored.
func ReadPlanArchiveMetadata(ctx context.Context, raw []byte, codec *contextpack.CursorCodec, token, principal string, maxBytes int, guard contextpack.CursorGuard) ([]byte, error) {
	if ctx == nil || codec == nil || guard == nil || maxBytes < 1024 || maxBytes > planArchiveMax {
		return nil, ErrInput
	}
	opened, err := codec.OpenPlanMaterialization(ctx, raw, token, principal, maxBytes, guard)
	if err != nil {
		return nil, err
	}
	var body planArchiveBody
	decoder := json.NewDecoder(bytes.NewReader(opened.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		return nil, ErrInput
	}
	canonical, err := json.Marshal(body)
	if err != nil || !bytes.Equal(canonical, opened.Payload) || body.Version != 1 || len(body.Metadata) > planArchiveMetadataMax {
		return nil, ErrInput
	}
	metadata := bytes.Clone(body.Metadata)
	if err := opened.Recheck(ctx, guard); err != nil {
		return nil, err
	}
	return metadata, nil
}

// ArchiveForRetention signs the already selected plan and application metadata
// against the original pack and list tokens. It does not persist any bytes.
func (p *PlanPager) ArchiveForRetention(ctx context.Context, packToken string, ttlSeconds int64, metadata []byte, maxBytes int) ([]byte, error) {
	if p == nil || p.plan == nil || p.plan.overview == nil || p.codec == nil || p.guard == nil || ctx == nil || len(metadata) > planArchiveMetadataMax || maxBytes < 1024 || maxBytes > planArchiveMax {
		return nil, ErrInput
	}
	report, err := p.plan.Read(ctx)
	if err != nil {
		return nil, err
	}
	a, errA := json.Marshal(report)
	b, errB := json.Marshal(p.report)
	digest, errD := planCanonicalDigest("document-plan-v1", report, p.plan.maxBytes)
	if errA != nil || errB != nil || errD != nil || len(a) > p.plan.maxBytes || !bytes.Equal(a, b) || digest != p.binding.ResultDigest {
		return nil, ErrAuthority
	}
	pathBytes := 0
	for _, name := range p.plan.overview.paths {
		pathBytes += len(name)
		if len(name) > 16384 || pathBytes > maxBytes {
			return nil, ErrLimit
		}
	}
	body := planArchiveBody{Version: 1, Report: report, Declarations: p.plan.declarations, OverviewPaths: p.plan.overview.paths, FactsDigest: p.plan.overview.factsDigest,
		Selector: append(json.RawMessage(nil), p.plan.selectorJSON...), MaxBytes: p.plan.maxBytes, PolicyMaxOutput: p.plan.policyMaxOutput, Metadata: bytes.Clone(metadata)}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxBytes {
		return nil, ErrLimit
	}
	// The codec authenticates both tokens and checks their current guard;
	// source/fact/declaration checks bracket this private sealing operation.
	// Do not nest a full plan read in each cryptographic token check.
	raw, err := p.codec.SealPlanMaterialization(ctx, packToken, p.anchor, p.binding.PrincipalDigest, encoded, ttlSeconds, p.plan.retentionExpires, maxBytes, p.guard)
	if err != nil {
		return nil, err
	}
	if err := p.plan.authorize(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

// RestorePlanPager returns the identical materialization and original anchor;
// it never rebuilds a selection or substitutes an active generation.
func RestorePlanPager(ctx context.Context, raw []byte, in PlanRestoreInput) (RestoredPlan, error) {
	var empty RestoredPlan
	if ctx == nil || in.Codec == nil || in.Guard == nil || in.Snapshot == nil || in.AuthorizeScope == nil || in.AuthorizePath == nil || in.VerifyFacts == nil || in.AuthorizeDocument == nil || in.VerifyDeclarations == nil || in.MaxArchiveBytes < 1024 || in.MaxArchiveBytes > planArchiveMax {
		return empty, ErrInput
	}
	opened, err := in.Codec.OpenPlanMaterialization(ctx, raw, in.PackToken, in.PrincipalDigest, in.MaxArchiveBytes, in.Guard)
	if err != nil {
		return empty, err
	}
	var body planArchiveBody
	decoder := json.NewDecoder(bytes.NewReader(opened.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		return empty, ErrInput
	}
	canonical, err := json.Marshal(body)
	if err != nil || !bytes.Equal(canonical, opened.Payload) || body.Version != 1 || body.MaxBytes < 4096 || body.MaxBytes > 16*1024*1024 || body.PolicyMaxOutput < 4096 || body.PolicyMaxOutput > 131072 || len(body.Metadata) > planArchiveMetadataMax || len(body.Declarations) > 512 || len(body.OverviewPaths) > 100000 || len(body.Selector) > 16384 {
		return empty, ErrInput
	}
	selector, err := model.DecodeSelectorJSON(body.Selector, 16384)
	if err != nil || !boundedPlanSelector(selector) {
		return empty, ErrInput
	}
	selectorJSON, err := json.Marshal(selector)
	if err != nil || !bytes.Equal(selectorJSON, body.Selector) {
		return empty, ErrInput
	}
	if len(body.FactsDigest) != 64 {
		return empty, ErrInput
	}
	if _, err := hex.DecodeString(body.FactsDigest); err != nil {
		return empty, ErrInput
	}
	if !sort.StringsAreSorted(body.OverviewPaths) {
		return empty, ErrInput
	}
	for i, name := range body.OverviewPaths {
		if model.ValidateRelativePath(name, false) != nil || i > 0 && name == body.OverviewPaths[i-1] {
			return empty, ErrInput
		}
		if _, ok := in.Snapshot.File(name); !ok {
			return empty, ErrAuthority
		}
	}
	for _, declaration := range body.Declarations {
		if !boundedSourceRef(declaration.Source) || model.ValidateRelativePath(declaration.Source.Path, false) != nil {
			return empty, ErrInput
		}
	}
	manifest := in.Snapshot.Manifest()
	scope := body.Report.Scope
	if manifest.WorktreeID != scope.Identity.WorktreeID || manifest.ProfileID != scope.Identity.ProfileID || manifest.PolicyDigest != scope.PolicyDigest || manifest.SnapshotDigest != scope.SnapshotDigest || model.VerifyCaptureDigest(manifest, 64*1024*1024) != nil {
		return empty, ErrAuthority
	}
	claims, err := in.Codec.VerifyPrincipal(ctx, opened.ListToken, "list", in.PrincipalDigest, in.Guard)
	if err != nil {
		return empty, err
	}
	reportJSON, err := json.Marshal(body.Report)
	if err != nil || len(reportJSON) > body.MaxBytes {
		return empty, ErrLimit
	}
	digest, err := planCanonicalDigest("document-plan-v1", body.Report, body.MaxBytes)
	if err != nil || digest != claims.Binding.ResultDigest {
		return empty, ErrAuthority
	}
	overview := &Report{scope: scope, factsDigest: body.FactsDigest, paths: append([]string(nil), body.OverviewPaths...), authorizeScope: in.AuthorizeScope, authorizePath: in.AuthorizePath, verifyFacts: in.VerifyFacts}
	if !pinnedOverviewMatches(claims.Binding.Scope, overview) {
		return empty, ErrAuthority
	}
	// Drop the serialized payload before retaining only the signed expiry proof.
	opened.Payload = nil
	plan := &Plan{overview: overview, declarations: body.Declarations, authorizeDocument: in.AuthorizeDocument, report: body.Report, maxBytes: body.MaxBytes,
		policyMaxOutput: body.PolicyMaxOutput, snapshot: in.Snapshot, selectorJSON: selectorJSON, retentionExpires: opened.ExpiresAt, verifyDeclarations: in.VerifyDeclarations,
		retentionCheck: func(ctx context.Context) error { return opened.Recheck(ctx, in.Guard) }}
	if err := plan.authorize(ctx); err != nil {
		return empty, err
	}
	pager := &PlanPager{plan: plan, report: body.Report, codec: in.Codec, binding: claims.Binding, guard: in.Guard, anchor: opened.ListToken, expires: opened.ExpiresAt}
	if err := opened.Recheck(ctx, in.Guard); err != nil {
		return empty, err
	}
	return RestoredPlan{Plan: plan, Pager: pager, Metadata: bytes.Clone(body.Metadata), ExpiresAt: opened.ExpiresAt}, nil
}
