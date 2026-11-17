package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// FreshnessPlanInput joins an already authorized plan with the same scope,
// cursor, freshness and policy boundaries used by a symbol pack.
type FreshnessPlanInput struct {
	Plan         *overview.Plan
	Codec        *contextpack.CursorCodec
	Binding      contextpack.CursorBinding
	Guard        contextpack.CursorGuard
	RequestJSON  []byte
	PrincipalID  string
	Coverage     model.Coverage
	Scanner      *policy.Scanner
	Validation   ValidateCapturedInput
	CaptureLive  CaptureLiveSource
	RequestLimit int
	// Optional for process-local plans; required to retain a plan across owners.
	DeclarationRevisionDigest string
}

type FreshnessPlan struct {
	plan                      *overview.Plan
	pager                     *overview.PlanPager
	codec                     *contextpack.CursorCodec
	guard                     contextpack.CursorGuard
	packBinding               contextpack.CursorBinding
	listBinding               contextpack.CursorBinding
	token                     string
	principal                 string
	mode                      string
	taskKind                  string
	firstMax                  int64
	coverage                  model.Coverage
	scanner                   *policy.Scanner
	targets                   []model.GraphRef
	validation                ValidateCapturedInput
	capture                   CaptureLiveSource
	declarationRevisionDigest string
}

// NewFreshnessPlan pins the plan and request before producing a pack token.
// The caller owns real scope, document, path and workspace authorization.
func NewFreshnessPlan(ctx context.Context, in FreshnessPlanInput) (*FreshnessPlan, string, error) {
	if ctx == nil || in.Plan == nil || in.Codec == nil || in.Guard == nil || in.Scanner == nil || in.PrincipalID == "" || len(in.PrincipalID) > 4096 || in.RequestLimit < 1024 || in.RequestLimit > 65536 || len(in.RequestJSON) > in.RequestLimit || in.Binding.Purpose != "pack" || in.Binding.ResultID != "" || in.Binding.ResultDigest != "" || in.DeclarationRevisionDigest != "" && !coreHash(in.DeclarationRevisionDigest) {
		return nil, "", ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, "", ErrValidationInput
	}
	request, err := model.DecodePackRequestJSON(in.RequestJSON, in.RequestLimit)
	if err != nil || request.TaskKind != "plan" && request.TaskKind != "explore" || request.View == nil || *request.View != "overview" {
		return nil, "", ErrValidationInput
	}
	digest, err := model.RequestDigest("context_pack", in.PrincipalID, "PackRequest", request, in.RequestLimit, 4)
	if err != nil || digest != in.Binding.RequestDigest || model.VerifyScopeVectorDigest(in.Binding.Scope, in.RequestLimit) != nil || in.Binding.Scope.Kind != "single" || len(in.Binding.Scope.Members) != 1 || len(in.Binding.Scope.UnavailableMembers) != 0 {
		return nil, "", ErrValidationInput
	}
	member := in.Binding.Scope.Members[0]
	if !requestMatchesScope(request.Scope, in.Binding.Scope, member, in.RequestLimit) || in.Validation.Scope.Kind != "" && model.ComparePinnedScope(in.Validation.Scope, in.Binding.Scope, in.RequestLimit) != nil || in.Validation.MaxInputBytes < 1024 || in.Validation.MaxInputBytes > 1024*1024 || in.Validation.MaxManifestBytes < 1024 || in.Validation.MaxManifestBytes > 64*1024*1024 {
		return nil, "", ErrValidationInput
	}
	if request.FreshnessMode == "checked_workspace" && (in.CaptureLive == nil || in.Validation.CurrentScope == nil || in.Validation.ResolveCapture == nil || in.Validation.AuthorizeSource == nil || in.Validation.VerifyWorkspace == nil) {
		return nil, "", ErrFreshnessUnknown
	}
	selector, err := in.Plan.Selector()
	if err != nil {
		return nil, "", ErrValidationInput
	}
	a, err := json.Marshal(selector)
	if err != nil {
		return nil, "", ErrValidationInput
	}
	b, err := json.Marshal(request.Selector)
	if err != nil || !bytes.Equal(a, b) {
		return nil, "", ErrValidationInput
	}
	report, err := in.Plan.Read(ctx)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	if report.Scope.Identity != identity || report.Scope.SnapshotDigest != member.SnapshotDigest || report.Scope.PolicyDigest != member.PolicyDigest || report.Scope.PolicyEpoch != member.PolicyEpoch || report.TaskKind != request.TaskKind {
		return nil, "", ErrValidationPrecondition
	}
	intent := ""
	if request.Intent != nil {
		intent = *request.Intent
	}
	constraints := append([]string{}, request.Constraints...)
	sort.Strings(constraints)
	if report.Intent != intent || len(report.Constraints) != len(constraints) {
		return nil, "", ErrValidationInput
	}
	for i := range constraints {
		if constraints[i] != report.Constraints[i] {
			return nil, "", ErrValidationInput
		}
	}
	rawCoverage, err := json.Marshal(in.Coverage)
	if err != nil || len(rawCoverage) > 1024*1024 {
		return nil, "", ErrValidationInput
	}
	if err := in.Scanner.CheckContent(rawCoverage); err != nil {
		return nil, "", err
	}
	coverage, err := model.DecodeCoverageJSON(rawCoverage, 1024*1024)
	if err != nil {
		return nil, "", ErrValidationInput
	}
	if err := scanPlanCoverage(in.Scanner, coverage); err != nil {
		return nil, "", err
	}
	targets, err := planTargets(request.Selector, report, identity)
	if err != nil {
		return nil, "", err
	}
	listBinding := in.Binding
	listBinding.Purpose = "list"
	guard := sanitizedPackGuard(in.Guard)
	pager, err := overview.NewPlanPager(ctx, in.Plan, in.Codec, listBinding, guard)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	claims, err := in.Codec.VerifyPrincipal(ctx, pager.Anchor(), "list", in.Binding.PrincipalDigest, guard)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	listBinding = claims.Binding
	packBinding := listBinding
	packBinding.Purpose = "pack"
	token, err := in.Codec.Issue(ctx, packBinding, contextpack.SignedPosition{LastKey: []string{}}, guard)
	if err != nil {
		return nil, "", packContextError(ctx, err)
	}
	if err := in.Codec.Recheck(ctx, claims, guard); err != nil {
		return nil, "", packContextError(ctx, err)
	}
	validation := in.Validation
	validation.Scope = packBinding.Scope
	validation.Sources = nil
	firstMax := int64(0)
	if request.MaxOutputBytes != nil {
		firstMax = *request.MaxOutputBytes
	}
	result := &FreshnessPlan{plan: in.Plan, pager: pager, codec: in.Codec, guard: guard, packBinding: packBinding, listBinding: listBinding, token: token, principal: in.Binding.PrincipalDigest, mode: request.FreshnessMode, taskKind: request.TaskKind, firstMax: firstMax, coverage: coverage, scanner: in.Scanner, targets: targets, validation: validation, capture: in.CaptureLive, declarationRevisionDigest: in.DeclarationRevisionDigest}
	if err := result.recheck(ctx, "", result.principal, nil); err != nil {
		return nil, "", packContextError(ctx, err)
	}
	return result, token, nil
}

func planTargets(selector model.Selector, report overview.PlanReport, identity model.GraphIdentity) ([]model.GraphRef, error) {
	targets := []model.GraphRef{}
	switch value := selector.(type) {
	case *model.WorkspaceSelector:
		if value == nil {
			return nil, ErrValidationInput
		}
	case *model.ModuleSelector:
		if value == nil || len(report.Modules) != 1 || report.Modules[0].Ref.ModuleID != value.ModuleID {
			return nil, ErrValidationInput
		}
		ref := report.Modules[0].Ref
		targets = append(targets, &ref)
	case *model.DocumentSelector:
		if value == nil {
			return nil, ErrValidationInput
		}
		hash := value.ContentHash
		targets = append(targets, &model.FileRef{Kind: "file", GraphIdentity: identity, Path: value.Path, ContentHash: &hash})
	case *model.RefSelector:
		if value == nil || value.ServiceID != nil {
			return nil, ErrValidationInput
		}
		raw, err := json.Marshal(value.Ref)
		if err != nil {
			return nil, ErrValidationInput
		}
		ref, err := model.DecodeGraphRefJSON(raw, 16384)
		if err != nil {
			return nil, ErrValidationInput
		}
		targets = append(targets, ref)
	default:
		return nil, ErrCoreUnsupported
	}
	return targets, nil
}

func scanPlanCoverage(scanner *policy.Scanner, coverage model.Coverage) error {
	check := func(values ...string) error {
		for _, value := range values {
			if err := scanner.CheckContent([]byte(value)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(coverage.Status, coverage.ScopeDescription); err != nil {
		return err
	}
	for _, capability := range coverage.Capabilities {
		if err := check(capability.Name, capability.ProviderID, capability.ProviderVersion, capability.Status); err != nil {
			return err
		}
	}
	for _, gap := range coverage.Gaps {
		if err := check(gap.Code, gap.Reason); err != nil {
			return err
		}
		for _, ref := range gap.SourceRefs {
			if err := check(ref.WorktreeID, ref.ProfileID, ref.GenerationID, ref.Path, ref.ContentHash); err != nil {
				return err
			}
		}
	}
	return nil
}

func (*FreshnessPlan) retainedPack() {}
func (p *FreshnessPlan) retainedID() string {
	if p == nil {
		return ""
	}
	return p.packBinding.ResultID
}
func (p *FreshnessPlan) retainedToken() string     { return p.token }
func (p *FreshnessPlan) retainedPrincipal() string { return p.principal }
func (p *FreshnessPlan) retainedCheck(ctx context.Context, principal string, guard contextpack.CursorGuard) error {
	return p.recheck(ctx, "", principal, guard)
}
func (p *FreshnessPlan) retainedBytes(limit int64) (int64, error) {
	weight, err := p.pager.MaterializedBytes(limit)
	if errors.Is(err, overview.ErrLimit) {
		return 0, contextpack.ErrLimit
	}
	if err != nil {
		return 0, ErrValidationInput
	}
	raw, err := json.Marshal(struct {
		Coverage model.Coverage
		Targets  []model.GraphRef
	}{p.coverage, p.targets})
	if err != nil {
		return 0, ErrValidationInput
	}
	if int64(len(raw)) > limit-weight {
		return 0, contextpack.ErrLimit
	}
	return weight + int64(len(raw)), nil
}

func (p *FreshnessPlan) recheck(ctx context.Context, cursor, principal string, current contextpack.CursorGuard) error {
	if p == nil || ctx == nil || p.guard == nil || principal != p.principal {
		return ErrValidationInput
	}
	// A nil current guard is private owner-only work (construct/archive/First).
	// Public continuation and retained callers separately require their guard.
	// Always call the original independent owner, including for owner-only work.
	check := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if err := p.guard(ctx, binding); err != nil {
			return err
		}
		if current != nil {
			return current(ctx, binding)
		}
		return ctx.Err()
	}
	claims, err := p.codec.AuthenticateBound(ctx, p.token, p.packBinding)
	if err != nil {
		return err
	}
	anchor, err := p.codec.AuthenticateBound(ctx, p.pager.Anchor(), p.listBinding)
	if err != nil {
		return err
	}
	var continuation contextpack.SignedCursor
	if cursor != "" {
		continuation, err = p.codec.AuthenticateBound(ctx, cursor, p.listBinding)
		if err != nil {
			return err
		}
	}
	if err := p.authorizeCoverage(ctx); err != nil {
		return err
	}
	if _, err := p.plan.Read(ctx); err != nil {
		return err
	}
	if err := p.authorizeCoverage(ctx); err != nil {
		return err
	}
	if err := p.codec.Recheck(ctx, anchor, check); err != nil {
		return err
	}
	if err := p.codec.Recheck(ctx, claims, check); err != nil {
		return err
	}
	if cursor != "" {
		if err := p.codec.Recheck(ctx, continuation, check); err != nil {
			return err
		}
	}
	return validationContextError(ctx)
}

// Storage callbacks authenticate the same immutable materialization and its
// independent declaration owner. A full plan/source/fact recheck still follows
// the completed storage operation before a handle or page can be released.
func (p *FreshnessPlan) retainedAuthority(ctx context.Context, principal string, current contextpack.CursorGuard) error {
	if p == nil || ctx == nil || current == nil || principal != p.principal {
		return ErrValidationInput
	}
	check := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if err := p.guard(ctx, binding); err != nil {
			return err
		}
		return current(ctx, binding)
	}
	if _, err := p.codec.Verify(ctx, p.token, p.packBinding, check); err != nil {
		return err
	}
	if _, err := p.codec.Verify(ctx, p.pager.Anchor(), p.listBinding, check); err != nil {
		return err
	}
	return validationContextError(ctx)
}

func (p *FreshnessPlan) authorizeCoverage(ctx context.Context) error {
	for _, gap := range p.coverage.Gaps {
		for _, ref := range gap.SourceRefs {
			if p.validation.AuthorizeSource == nil {
				return ErrValidationAuthority
			}
			member, err := model.MatchPinnedSource(p.packBinding.Scope, ref, nil, p.validation.MaxInputBytes)
			if err != nil {
				return ErrValidationPrecondition
			}
			if err := p.validation.AuthorizeSource(ctx, copyValidationMember(member), ref); err != nil {
				return packContextError(ctx, ErrValidationAuthority)
			}
		}
	}
	return validationContextError(ctx)
}

// First and Continue retain the original plan and never re-resolve the active
// generation. A page that cannot fit its complete envelope is not returned.
func (p *FreshnessPlan) First(ctx context.Context, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p == nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	return p.page(ctx, "", p.principal, p.guard, options, true)
}

func (p *FreshnessPlan) Continue(ctx context.Context, token, cursor, principal string, guard contextpack.CursorGuard, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p == nil || token != p.token || cursor == "" || guard == nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	return p.page(ctx, cursor, principal, guard, options, false)
}

func (p *FreshnessPlan) page(ctx context.Context, cursor, principal string, guard contextpack.CursorGuard, options contextpack.PackPageOptions, first bool) (contextpack.PackPage, error) {
	if ctx == nil || guard == nil || !coreUUID(options.RequestID) {
		return contextpack.PackPage{}, ErrValidationInput
	}
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = 24576
	}
	if options.MaxOutputBytes > p.plan.PolicyMaxOutput() {
		options.MaxOutputBytes = p.plan.PolicyMaxOutput()
	}
	if first && p.firstMax > 0 && options.MaxOutputBytes > p.firstMax {
		options.MaxOutputBytes = p.firstMax
	}
	if options.DeliveryMode == "" {
		options.DeliveryMode = "mcp_dual"
	}
	if options.MaxOutputBytes < 4096 || options.MaxOutputBytes > 131072 || options.MaxItems < 0 || options.MaxItems > 512 {
		return contextpack.PackPage{}, ErrValidationInput
	}
	if options.MaxItems == 1 {
		return contextpack.PackPage{}, contextpack.ErrBudgetTooSmall
	}
	switch options.DeliveryMode {
	case "http", "cli", "mcp_text", "mcp_dual", "mcp_structured_verified":
	default:
		return contextpack.PackPage{}, ErrValidationInput
	}
	if err := recheckStructuredHost(ctx, options); err != nil {
		return contextpack.PackPage{}, err
	}
	current := guard
	if first {
		// First is invoked only with this plan's original guard; checking it
		// twice nests the same database observations without another caller.
		current = nil
	}
	if err := p.recheck(ctx, cursor, principal, current); err != nil {
		return contextpack.PackPage{}, packContextError(ctx, err)
	}
	previous := contextpack.SignedPosition{LastKey: []string{}}
	if cursor != "" {
		claims, err := p.codec.AuthenticateBound(ctx, cursor, p.listBinding)
		if err != nil {
			return contextpack.PackPage{}, packContextError(ctx, err)
		}
		previous = claims.Position
	}
	var full CapturedValidation
	if first && p.mode == "checked_workspace" {
		var err error
		full, err = p.validateWorkspace(ctx)
		if err != nil {
			return contextpack.PackPage{}, err
		}
	}
	pageLimit := int(options.MaxOutputBytes)
	if options.DeliveryMode == "mcp_dual" {
		pageLimit /= 2
	}
	if pageLimit < 512 {
		pageLimit = 512
	}
	operation := "context_pack"
	if !first {
		operation = "expand_pack"
	}
	var last overview.PlanPage
	for {
		data, err := p.pager.Page(ctx, cursor, principal, guard, pageLimit)
		if errors.Is(err, overview.ErrLimit) {
			break
		}
		if errors.Is(err, contextpack.ErrItemTooLarge) && (previous.NextCoverageGap < int64(len(p.coverage.Gaps)) || previous.NextWarningGap < int64(len(data.Gaps))) {
			page, fitErr := p.fitPage(ctx, data, previous, guard, operation, options, first, full)
			if fitErr == nil {
				return p.finishPage(ctx, page, first, cursor, principal, guard, options)
			}
			if errors.Is(fitErr, contextpack.ErrBudgetTooSmall) {
				return contextpack.PackPage{}, contextpack.ErrBudgetTooSmall
			}
			return contextpack.PackPage{}, fitErr
		}
		if err != nil {
			return contextpack.PackPage{}, p.describeOversized(ctx, err, cursor, principal, guard, options, first)
		}
		last = data
		page, err := p.fitPage(ctx, data, previous, guard, operation, options, first, full)
		if errors.Is(err, contextpack.ErrBudgetTooSmall) && pageLimit > 512 {
			next := pageLimit * 3 / 4
			if next < 512 {
				next = 512
			}
			pageLimit = next
			continue
		}
		if errors.Is(err, contextpack.ErrBudgetTooSmall) {
			break
		}
		if err != nil {
			return contextpack.PackPage{}, err
		}
		return p.finishPage(ctx, page, first, cursor, principal, guard, options)
	}
	// If the next source cannot fit alongside pending safety details, a
	// details-only page may advance those obligations without consuming it.
	if len(last.Items) != 0 {
		if last.NextPosition.NextItem > previous.NextItem {
			last.PendingItems++
			if last.Items[0].RequiredForReview {
				last.RequiredPending++
			}
		}
		last.Items = nil
		last.NextPosition = previous
		page, err := p.fitPage(ctx, last, previous, guard, operation, options, first, full)
		if err == nil {
			return p.finishPage(ctx, page, first, cursor, principal, guard, options)
		}
		if err != nil && !errors.Is(err, contextpack.ErrBudgetTooSmall) {
			return contextpack.PackPage{}, packContextError(ctx, err)
		}
	}
	return contextpack.PackPage{}, contextpack.ErrBudgetTooSmall
}

func (p *FreshnessPlan) finishPage(ctx context.Context, page contextpack.PackPage, first bool, cursor, principal string, guard contextpack.CursorGuard, options contextpack.PackPageOptions) (contextpack.PackPage, error) {
	if p.mode == "checked_workspace" && !first {
		sources, err := pageValidationSources(page.Response, nil, p.validation.MaxInputBytes)
		if err != nil {
			return contextpack.PackPage{}, err
		}
		if len(sources) > 0 {
			// Recheck immediately before delivery, while retaining the earlier
			// observation timestamp that was measured in the actual envelope.
			if _, err := validateReturnedFiles(ctx, p.validation, sources, p.capture); err != nil {
				return contextpack.PackPage{}, err
			}
		}
	}
	current := guard
	if first {
		// First is invoked only with this plan's original guard; checking it
		// twice nests the same database observations without another caller.
		current = nil
	}
	if err := p.recheck(ctx, cursor, principal, current); err != nil {
		return contextpack.PackPage{}, packContextError(ctx, err)
	}
	if err := recheckStructuredHost(ctx, options); err != nil {
		return contextpack.PackPage{}, err
	}
	return page, nil
}

// fitPage keeps explicit safety details ahead of optional evidence. It tries
// the full set first, then finds a bounded prefix that fits with a grouped
// remainder and a signed advancing cursor.
func (p *FreshnessPlan) fitPage(ctx context.Context, page overview.PlanPage, previous contextpack.SignedPosition, guard contextpack.CursorGuard, operation string, options contextpack.PackPageOptions, first bool, full CapturedValidation) (contextpack.PackPage, error) {
	if previous.NextCoverageGap > int64(len(p.coverage.Gaps)) || previous.NextWarningGap > int64(len(page.Gaps)) {
		return contextpack.PackPage{}, contextpack.ErrCursorInvalid
	}
	coverageMax := len(p.coverage.Gaps) - int(previous.NextCoverageGap)
	warningMax := len(page.Gaps) - int(previous.NextWarningGap)
	if coverageMax > 64 {
		coverageMax = 63
	}
	if warningMax > 64 {
		warningMax = 63
	}
	try := func(total int) (contextpack.PackPage, error) {
		coverageVisible := total
		if coverageVisible > coverageMax {
			coverageVisible = coverageMax
		}
		warningVisible := total - coverageVisible
		return p.renderPage(ctx, page, previous, guard, coverageVisible, warningVisible, operation, options, first, full)
	}
	max := coverageMax + warningMax
	best, err := try(max)
	if err == nil {
		return best, nil
	}
	if !errors.Is(err, contextpack.ErrBudgetTooSmall) {
		return contextpack.PackPage{}, err
	}
	var found bool
	low := 0
	if len(page.Items) == 0 && (page.PendingItems > 0 || max > 0) {
		low = 1 // A details-only continuation must advance at least one gap.
	}
	for high := max - 1; low <= high; {
		mid := low + (high-low)/2
		candidate, e := try(mid)
		if e == nil {
			best, found, low = candidate, true, mid+1
		} else if errors.Is(e, contextpack.ErrBudgetTooSmall) {
			high = mid - 1
		} else {
			return contextpack.PackPage{}, e
		}
	}
	if !found {
		return contextpack.PackPage{}, contextpack.ErrBudgetTooSmall
	}
	return best, nil
}

func groupedPlanGaps(gaps []model.Gap, visible int) []model.Gap {
	result := append([]model.Gap{}, gaps[:visible]...)
	if visible < len(gaps) {
		count := int64(len(gaps) - visible)
		result = append(result, model.Gap{Code: "GAP_DETAILS_PENDING", Reason: "Additional safety gap details must be expanded before a review is complete.", Count: &count, SourceRefs: []model.SourceRef{}})
	}
	return result
}

func (p *FreshnessPlan) renderPage(ctx context.Context, page overview.PlanPage, previous contextpack.SignedPosition, guard contextpack.CursorGuard, coverageVisible, warningVisible int, operation string, options contextpack.PackPageOptions, first bool, full CapturedValidation) (contextpack.PackPage, error) {
	if page.PendingItems < 0 || page.RequiredPending < 0 || page.RequiredPending > page.PendingItems || len(page.Items) > 1 || len(page.Gaps) > 64 || page.NextPosition.NextCoverageGap != previous.NextCoverageGap || page.NextPosition.NextWarningGap != previous.NextWarningGap {
		return contextpack.PackPage{}, ErrValidationInput
	}
	metadata, err := json.Marshal(struct {
		ProjectStage           string   `json:"project_stage"`
		ImplementationEvidence string   `json:"implementation_evidence"`
		ValidationStatus       string   `json:"validation_status"`
		ReviewStatus           string   `json:"review_status"`
		Intent                 string   `json:"intent"`
		Constraints            []string `json:"constraints"`
	}{page.ProjectStage, page.ImplementationEvidence, page.ValidationStatus, page.ReviewStatus, page.Intent, page.Constraints})
	if err != nil || len(metadata) > 16384 {
		return contextpack.PackPage{}, ErrValidationInput
	}
	if err := p.scanner.CheckContent(metadata); err != nil {
		return contextpack.PackPage{}, err
	}
	left, err := model.CanonicalString(p.packBinding.Scope.VectorDigest)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	middle, err := model.CanonicalString(p.packBinding.RequestDigest)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	fields := []model.CanonicalField{}
	for _, field := range []struct{ key, value string }{{"project_stage", page.ProjectStage}, {"implementation_evidence", page.ImplementationEvidence}, {"validation_status", page.ValidationStatus}, {"review_status", page.ReviewStatus}, {"intent", page.Intent}} {
		value, err := model.CanonicalString(field.value)
		if err != nil {
			return contextpack.PackPage{}, ErrValidationInput
		}
		fields = append(fields, model.CanonicalField{Key: field.key, Value: value})
	}
	constraints := make([]model.CanonicalValue, 0, len(page.Constraints))
	for _, constraint := range page.Constraints {
		value, err := model.CanonicalString(constraint)
		if err != nil {
			return contextpack.PackPage{}, ErrValidationInput
		}
		constraints = append(constraints, value)
	}
	fields = append(fields, model.CanonicalField{Key: "constraints", Value: model.CanonicalArray(constraints)})
	right, err := model.CanonicalObject(fields)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	_, metadataID, err := model.EncodeCanonical("plan-summary-item-v1", model.CanonicalArray([]model.CanonicalValue{left, middle, right}), 65536)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	text := string(metadata)
	summaryWhy := "plan_summary"
	if p.taskKind == "explore" {
		summaryWhy = "overview_summary"
	}
	items := []model.EvidenceItem{{ItemID: metadataID, Kind: "overview", WhyIncluded: summaryWhy, SourceRefs: []model.SourceRef{}, Content: &text, EvidenceRefs: []model.EvidenceRef{}, Limitations: []model.Gap{}}}
	for _, item := range page.Items {
		kind, why := "source_range", "intended_"+item.Kind
		if item.Kind == "module" {
			why = "observed_module"
		}
		if item.Kind == "structure_module" {
			kind, why = "overview", "observed_module_structure"
		}
		if item.Kind == "structure_file" {
			kind, why = "overview", "observed_file_membership"
		}
		if item.Kind == "constraint" {
			kind = "constraint_excerpt"
		}
		if item.FragmentOf != nil {
			kind = "source_range_fragment"
		}
		content := item.Content
		items = append(items, model.EvidenceItem{ItemID: item.ID, Kind: kind, RequiredForReview: item.RequiredForReview, WhyIncluded: why, SourceRefs: []model.SourceRef{item.Source}, Content: &content, EvidenceRefs: []model.EvidenceRef{}, Limitations: []model.Gap{}, FragmentOf: item.FragmentOf})
	}
	warnings := make([]model.Gap, 0, len(page.Gaps))
	for _, code := range page.Gaps {
		warnings = append(warnings, model.Gap{Code: code, Reason: "The plan has an unresolved evidence limitation.", SourceRefs: []model.SourceRef{}})
	}
	scopeRaw, err := json.Marshal(p.packBinding.Scope)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	scope, err := model.DecodeScopeVectorJSON(scopeRaw, 32768)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	coverageRaw, err := json.Marshal(p.coverage)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	coverage, err := model.DecodeCoverageJSON(coverageRaw, 1024*1024)
	if err != nil {
		return contextpack.PackPage{}, ErrValidationInput
	}
	coverageRemaining := coverage.Gaps[int(previous.NextCoverageGap):]
	warningsRemaining := warnings[int(previous.NextWarningGap):]
	if coverageVisible < 0 || coverageVisible > len(coverageRemaining) || warningVisible < 0 || warningVisible > len(warningsRemaining) {
		return contextpack.PackPage{}, ErrValidationInput
	}
	coverage.Gaps = groupedPlanGaps(coverageRemaining, coverageVisible)
	warnings = groupedPlanGaps(warningsRemaining, warningVisible)
	if coverageVisible < len(coverageRemaining) && coverage.Status == "supported_scope_complete" {
		coverage.Status = "partial"
	}
	targets := make([]model.GraphRef, 0, len(p.targets))
	for _, target := range p.targets {
		raw, err := json.Marshal(target)
		if err != nil {
			return contextpack.PackPage{}, ErrValidationInput
		}
		ref, err := model.DecodeGraphRefJSON(raw, 16384)
		if err != nil {
			return contextpack.PackPage{}, ErrValidationInput
		}
		targets = append(targets, ref)
	}
	required := int64(page.RequiredPending + len(coverageRemaining) - coverageVisible + len(warningsRemaining) - warningVisible)
	optional := int64(page.PendingItems - page.RequiredPending)
	status := "ok"
	var reason *string
	var cursor *string
	if required > 0 || optional > 0 {
		status = "partial"
		value := "OUTPUT_BUDGET"
		if coverageVisible < len(coverageRemaining) || warningVisible < len(warningsRemaining) {
			value = "SAFETY_GAPS_PENDING"
		}
		reason = &value
		next := page.NextPosition
		next.NextCoverageGap = previous.NextCoverageGap + int64(coverageVisible)
		next.NextWarningGap = previous.NextWarningGap + int64(warningVisible)
		if !contextpack.CursorAdvances(previous, next) {
			return contextpack.PackPage{}, contextpack.ErrBudgetTooSmall
		}
		check := func(ctx context.Context, binding contextpack.CursorBinding) error {
			if err := p.guard(ctx, binding); err != nil {
				return err
			}
			return guard(ctx, binding)
		}
		token, err := p.codec.Issue(ctx, p.listBinding, next, check)
		if err != nil {
			return contextpack.PackPage{}, packContextError(ctx, err)
		}
		cursor = &token
	}
	response := model.Response{SchemaVersion: 1, RequestID: options.RequestID, Status: status, Scope: &scope, Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}, Coverage: coverage, Readiness: "needs_context", Data: model.PackData{Operation: operation, PackToken: p.token, TargetRefs: targets, Items: items, EditPreconditionValidated: false}, Pagination: model.Pagination{Truncated: cursor != nil, EnumerationComplete: true, ReturnedCount: int64(len(items)), RequiredPending: &required, OptionalPending: &optional, NextCursor: cursor, LimitReason: reason}, Budget: model.Budget{MaxOutputBytes: options.MaxOutputBytes, DeliveryMode: options.DeliveryMode}, Warnings: warnings}
	if p.mode == "checked_workspace" {
		if first {
			response.Freshness = full.Freshness
		} else {
			sources, err := pageValidationSources(response, nil, p.validation.MaxInputBytes)
			if err != nil {
				return contextpack.PackPage{}, err
			}
			if len(sources) > 0 {
				response.Freshness, err = validateReturnedFiles(ctx, p.validation, sources, p.capture)
				if err != nil {
					return contextpack.PackPage{}, err
				}
			}
		}
	}
	if err := validateCoreResponseScope(response, 1024*1024); err != nil {
		return contextpack.PackPage{}, err
	}
	return resettleFreshnessPage(contextpack.PackPage{Response: response}, operation)
}

func (p *FreshnessPlan) validateWorkspace(ctx context.Context) (CapturedValidation, error) {
	if p.validation.CurrentScope == nil || p.validation.ResolveCapture == nil || p.validation.AuthorizeSource == nil || p.validation.VerifyWorkspace == nil {
		return CapturedValidation{}, ErrFreshnessUnknown
	}
	report, err := p.plan.Read(ctx)
	if err != nil {
		return CapturedValidation{}, packContextError(ctx, err)
	}
	collector := newSourceCollector(nil, p.validation.MaxInputBytes)
	for _, item := range report.Items {
		if err := collector.add(item.Source); err != nil {
			return CapturedValidation{}, err
		}
	}
	if err := collector.gaps(p.coverage.Gaps); err != nil {
		return CapturedValidation{}, err
	}
	if len(collector.values) == 0 {
		return CapturedValidation{}, ErrFreshnessUnknown
	}
	validation := p.validation
	validation.Sources = collector.values
	return ValidateCapturedWorkspace(ctx, validation)
}

// Validate repeats full workspace verification and never grants edit authority.
func (p *FreshnessPlan) Validate(ctx context.Context, token, principal string, guard contextpack.CursorGuard) (CapturedValidation, error) {
	if p == nil || token != p.token || guard == nil {
		return CapturedValidation{}, ErrValidationInput
	}
	if err := p.recheck(ctx, "", principal, guard); err != nil {
		return CapturedValidation{}, packContextError(ctx, err)
	}
	result, err := p.validateWorkspace(ctx)
	if err != nil {
		return CapturedValidation{}, err
	}
	if err := p.recheck(ctx, "", principal, guard); err != nil {
		return CapturedValidation{}, packContextError(ctx, err)
	}
	return result, nil
}
