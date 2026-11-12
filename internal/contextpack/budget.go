package contextpack

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrBudgetTooSmall          = errors.New("minimum budget cannot carry a complete item")
	ErrItemTooLarge            = errors.New("whole evidence item exceeds the supported budget")
	ErrContinuationUnavailable = errors.New("verified advancing continuation unavailable")
)

// OversizedItemError exposes only the selected source descriptor, not a
// truncated body. Fragmentation is owned by T025.
type OversizedItemError struct {
	ItemID  string
	Sources []model.SourceRef
}

func (*OversizedItemError) Error() string { return ErrItemTooLarge.Error() }
func (*OversizedItemError) Unwrap() error { return ErrItemTooLarge }

// CursorPosition is an internal plan for T025's authenticated continuation.
// The issuer must bind it to the principal, pinned vector and request, and
// report whether the continuation can actually advance. This package does not
// implement signing, verification, expiry or persistence.
type CursorPosition struct {
	ScopeDigest         string
	NextItem            int
	NextCoverageGap     int
	NextWarningGap      int
	TraversalIncomplete bool
}

type BudgetInput struct {
	Slice                *TaskSlice
	RequestID            string
	PackToken            string // issued and checked by the future token owner
	MaxOutputBytes       int64
	MaxItems             int
	DeliveryMode         string
	VerifyStructuredHost func(context.Context) error
	IssueContinuation    func(context.Context, CursorPosition) (token string, canAdvance bool, err error)
}

// BudgetPage is internal. Blocks are the exact tool-visible serialized JSON
// representations; actual transport framing and host support remain unproved.
// Pending gaps are retained for the T025 continuation owner.
type BudgetPage struct {
	Response            model.Response
	Blocks              [][]byte
	Next                CursorPosition
	PendingCoverageGaps []model.Gap
	PendingWarningGaps  []model.Gap
}

var errOverBudget = errors.New("serialized response exceeds budget")

func normalizeBudget(in BudgetInput) (BudgetInput, error) {
	if in.Slice == nil {
		return BudgetInput{}, ErrInput
	}
	if in.MaxOutputBytes == 0 {
		in.MaxOutputBytes = 24576
		if in.MaxOutputBytes > in.Slice.policyMaxOutput {
			in.MaxOutputBytes = in.Slice.policyMaxOutput
		}
	}
	if in.MaxItems == 0 {
		in.MaxItems = 512
	}
	if in.DeliveryMode == "" {
		in.DeliveryMode = "mcp_dual"
	}
	if in.MaxOutputBytes < 4096 || in.MaxOutputBytes > 131072 || in.MaxOutputBytes > in.Slice.policyMaxOutput || in.MaxItems < 1 || in.MaxItems > 512 {
		return BudgetInput{}, ErrInput
	}
	switch in.DeliveryMode {
	case "http", "cli", "mcp_text", "mcp_dual", "mcp_structured_verified":
	default:
		return BudgetInput{}, ErrInput
	}
	return in, nil
}

func verifyBudgetHost(ctx context.Context, in BudgetInput) error {
	if in.DeliveryMode != "mcp_structured_verified" {
		return nil
	}
	if in.VerifyStructuredHost == nil {
		return ErrAuthority
	}
	if err := in.VerifyStructuredHost(ctx); err != nil {
		return authorityError(ctx)
	}
	return ctx.Err()
}

func groupedGaps(gaps []model.Gap, visible int) []model.Gap {
	out := append([]model.Gap{}, gaps[:visible]...)
	if visible < len(gaps) {
		count := int64(len(gaps) - visible)
		out = append(out, model.Gap{Code: "GAP_DETAILS_PENDING", Reason: "Additional safety gap details must be expanded before a review is complete.", Count: &count, SourceRefs: []model.SourceRef{}})
	}
	return out
}

// BudgetTaskSlice emits only complete S43 evidence items. It has no public
// pagination route and must not be wired until token authority exists.
func BudgetTaskSlice(ctx context.Context, in BudgetInput) (BudgetPage, error) {
	if ctx == nil {
		return BudgetPage{}, ErrInput
	}
	var err error
	in, err = normalizeBudget(in)
	if err != nil {
		return BudgetPage{}, err
	}
	if err := verifyBudgetHost(ctx, in); err != nil {
		return BudgetPage{}, err
	}
	slice, err := in.Slice.Read(ctx)
	if err != nil {
		return BudgetPage{}, err
	}
	selection := in.Slice.selection
	scopeBytes, e := json.Marshal(slice.Scope)
	if e != nil {
		return BudgetPage{}, ErrInput
	}
	if e = selection.scanner.CheckContent(scopeBytes); e != nil {
		return BudgetPage{}, e
	}
	ordered := append([]TaskEvidence{}, slice.Items...)
	plan, err := json.Marshal(struct {
		Status, Guidance      string
		RelatedTestReferences int
	}{slice.Validation.Status, slice.Validation.Guidance, len(slice.Validation.TestReferences)})
	if err != nil {
		return BudgetPage{}, ErrInput
	}
	planText := string(plan)
	planID, err := evidenceID(slice.Scope.VectorDigest, slice.TaskKind, "validation_obligation", []model.SourceRef{}, "", planText, 0)
	if err != nil {
		return BudgetPage{}, err
	}
	ordered = append(ordered, TaskEvidence{ID: planID, Kind: "validation_obligation", RequiredForReview: true, Why: "validation_plan", Plane: "annotation", Trust: "derived_data", SourceRefs: []model.SourceRef{}, Content: &planText})
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].RequiredForReview && !ordered[j].RequiredForReview })
	coverage := selection.report.Coverage
	coverageGaps := append([]model.Gap{}, coverage.Gaps...)
	warnings := append([]model.Gap{}, slice.Gaps...)
	vc, vw := len(coverageGaps), len(warnings)
	if vc > 64 {
		vc = 63
	}
	if vw > 64 {
		vw = 63
	}
	if vc < 0 || vw < 0 {
		return BudgetPage{}, ErrInput
	}

	// Recompute the envelope, token and fixed-point byte count for each trial.
	// The token callback is fixture-only until T025 provides the signer.
	render := func(items []model.EvidenceItem, cv, wv int, cap int64) (BudgetPage, bool, error) {
		if err := ctx.Err(); err != nil {
			return BudgetPage{}, false, err
		}
		required, optional := int64(len(coverageGaps)-cv+len(warnings)-wv), int64(0)
		for _, item := range ordered[len(items):] {
			if item.RequiredForReview {
				required++
			} else {
				optional++
			}
		}
		truncated := required > 0 || optional > 0 || !slice.EnumerationComplete
		position := CursorPosition{ScopeDigest: slice.Scope.VectorDigest, NextItem: len(items), NextCoverageGap: cv, NextWarningGap: wv, TraversalIncomplete: !slice.EnumerationComplete}
		var cursor *string
		canAdvance := true
		if truncated {
			value := "pending-continuation-placeholder"
			canAdvance = false
			if in.IssueContinuation != nil {
				var e error
				value, canAdvance, e = in.IssueContinuation(ctx, position)
				if e != nil {
					return BudgetPage{}, false, authorityError(ctx)
				}
				if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 32768 {
					return BudgetPage{}, false, ErrInput
				}
			}
			cursor = &value
		}
		var reason *string
		if truncated {
			value := "OUTPUT_BUDGET"
			if !slice.EnumerationComplete {
				value = "TRAVERSAL_INCOMPLETE"
			} else if cv < len(coverageGaps) || wv < len(warnings) {
				value = "SAFETY_GAPS_PENDING"
			} else if len(items) >= in.MaxItems {
				value = "ITEM_LIMIT"
			}
			reason = &value
		}
		var reqCount, optCount *int64
		if slice.EnumerationComplete {
			reqCount, optCount = &required, &optional
		}
		pageCoverage := coverage
		pageCoverage.Gaps = groupedGaps(coverageGaps, cv)
		if cv < len(coverageGaps) && pageCoverage.Status == "supported_scope_complete" {
			pageCoverage.Status = "partial"
		}
		status := "ok"
		if truncated {
			status = "partial"
		}
		target := slice.Target
		response := model.Response{SchemaVersion: 1, RequestID: in.RequestID, Status: status, Scope: &slice.Scope, Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}, Coverage: pageCoverage, Readiness: "needs_context", Data: model.PackData{Operation: "context_pack", PackToken: in.PackToken, TargetRefs: []model.GraphRef{&target}, Items: append([]model.EvidenceItem{}, items...), EditPreconditionValidated: false}, Pagination: model.Pagination{Truncated: truncated, EnumerationComplete: slice.EnumerationComplete, ReturnedCount: int64(len(items)), RequiredPending: reqCount, OptionalPending: optCount, NextCursor: cursor, LimitReason: reason}, Budget: model.Budget{MaxOutputBytes: cap, DeliveryMode: in.DeliveryMode}, Warnings: groupedGaps(warnings, wv)}
		blocks, e := settleResponse(&response)
		if e != nil {
			return BudgetPage{}, false, e
		}
		return BudgetPage{Response: response, Blocks: blocks, Next: position, PendingCoverageGaps: append([]model.Gap{}, coverageGaps[cv:]...), PendingWarningGaps: append([]model.Gap{}, warnings[wv:]...)}, canAdvance, nil
	}
	items := []model.EvidenceItem{}
	var best BudgetPage
	var progress bool
	for {
		best, progress, err = render(items, vc, vw, in.MaxOutputBytes)
		if !errors.Is(err, errOverBudget) {
			break
		}
		if vw > 0 {
			vw--
			continue
		}
		if vc > 0 {
			vc--
			continue
		}
		return BudgetPage{}, ErrBudgetTooSmall
	}
	if err != nil {
		return BudgetPage{}, err
	}
	var firstOversized bool
	for i := 0; i < len(ordered) && i < in.MaxItems; i++ {
		candidate, e := wireCandidate(selection, ordered[i])
		if e != nil {
			if errors.Is(e, ErrItemTooLarge) {
				firstOversized = len(items) == 0
				break
			}
			return BudgetPage{}, e
		}
		trial := append(append([]model.EvidenceItem{}, items...), candidate)
		trialCV, trialWV := vc, vw
		next, adv, e := render(trial, trialCV, trialWV, in.MaxOutputBytes)
		for errors.Is(e, errOverBudget) && candidate.RequiredForReview && (trialCV > 0 || trialWV > 0) {
			if trialWV > 0 {
				trialWV--
			} else {
				trialCV--
			}
			next, adv, e = render(trial, trialCV, trialWV, in.MaxOutputBytes)
		}
		if errors.Is(e, errOverBudget) {
			if len(items) == 0 {
				_, _, maxErr := render(trial, trialCV, trialWV, in.Slice.policyMaxOutput)
				firstOversized = errors.Is(maxErr, errOverBudget)
			}
			break
		}
		if e != nil {
			return BudgetPage{}, e
		}
		items, best, progress, vc, vw = trial, next, adv, trialCV, trialWV
	}
	if len(items) == 0 && len(ordered) > 0 {
		if firstOversized {
			return BudgetPage{}, &OversizedItemError{ItemID: ordered[0].ID, Sources: append([]model.SourceRef{}, ordered[0].SourceRefs...)}
		}
		return BudgetPage{}, ErrBudgetTooSmall
	}
	if best.Response.Pagination.Truncated && (in.IssueContinuation == nil || !progress) {
		return BudgetPage{}, ErrContinuationUnavailable
	}
	if err := in.Slice.authorize(ctx); err != nil {
		return BudgetPage{}, err
	}
	if err := verifyBudgetHost(ctx, in); err != nil {
		return BudgetPage{}, err
	}
	return best, nil
}

func settleResponse(response *model.Response) ([][]byte, error) {
	estimate := int64(0)
	for i := 0; i < 8; i++ {
		response.Budget.SerializedBytes = estimate
		raw, err := json.Marshal(response)
		if err != nil {
			return nil, ErrInput
		}
		count := int64(len(raw))
		if response.Budget.DeliveryMode == "mcp_dual" {
			count *= 2
		}
		if count == estimate {
			if count > response.Budget.MaxOutputBytes {
				return nil, errOverBudget
			}
			blocks := [][]byte{raw}
			if response.Budget.DeliveryMode == "mcp_dual" {
				blocks = append(blocks, append([]byte(nil), raw...))
			}
			if _, err := model.DecodeResponseJSON(raw, int(response.Budget.MaxOutputBytes), 4, "context_pack", false); err != nil {
				return nil, ErrInput
			}
			if err := model.ValidateDeliveredBytes(response.Budget, blocks); err != nil {
				return nil, ErrInput
			}
			return blocks, nil
		}
		estimate = count
	}
	return nil, ErrInput
}
