package contextpack

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// PagedPack is a bounded, process-local materialization. Losing it requires a
// new pack; callers must never resolve a continuation against a newer snapshot.
// The application owner must bound the number and lifetime of such instances.
type PagedPack struct {
	slice     *TaskSlice
	report    SliceReport
	coverage  model.Coverage
	items     []TaskEvidence
	codec     *CursorCodec
	binding   CursorBinding
	guard     CursorGuard
	token     string
	expiresAt int64 // optional, signed retention limit of a restored pack
}

type PackPageOptions struct {
	RequestID            string
	MaxOutputBytes       int64
	MaxItems             int
	DeliveryMode         string
	VerifyStructuredHost func(context.Context) error
}

type PackPage struct {
	Response model.Response
	Blocks   [][]byte
}

func newPackID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

// NewPagedPack captures an authorized task slice once. Binding digests and the
// guard come from trusted application code; the materialization is not a public
// handler and does not itself prove host compatibility or current workspace.
func NewPagedPack(ctx context.Context, slice *TaskSlice, codec *CursorCodec, binding CursorBinding, guard CursorGuard) (*PagedPack, string, error) {
	if ctx == nil || slice == nil || slice.selection == nil || codec == nil || guard == nil || binding.Purpose != "pack" || binding.ResultID != "" || binding.ResultDigest != "" {
		return nil, "", ErrInput
	}
	report, err := slice.Read(ctx)
	if err != nil {
		return nil, "", err
	}
	// An incomplete traversal has no proven continuation in this primitive.
	if !report.EnumerationComplete {
		return nil, "", ErrContinuationUnavailable
	}
	if err = model.ComparePinnedScope(binding.Scope, report.Scope, cursorLimit); err != nil {
		return nil, "", ErrInput
	}
	selectionReport, err := slice.selection.Read(ctx)
	if err != nil {
		return nil, "", err
	}
	sliceCopy, selectionCopy := *slice, *slice.selection
	selectionCopy.report = selectionReport
	sliceCopy.selection = &selectionCopy
	sliceCopy.report = report
	sliceCopy.rulePaths = append([]string{}, slice.rulePaths...)
	plan, err := json.Marshal(struct {
		Status, Guidance      string
		RelatedTestReferences int
	}{report.Validation.Status, report.Validation.Guidance, len(report.Validation.TestReferences)})
	if err != nil {
		return nil, "", ErrInput
	}
	planText := string(plan)
	planID, err := evidenceID(report.Scope.VectorDigest, report.TaskKind, "validation_obligation", []model.SourceRef{}, "", planText, 0)
	if err != nil {
		return nil, "", err
	}
	ordered := append([]TaskEvidence{}, report.Items...)
	ordered = append(ordered, TaskEvidence{ID: planID, Kind: "validation_obligation", RequiredForReview: true, Why: "validation_plan", Plane: "annotation", Trust: "derived_data", SourceRefs: []model.SourceRef{}, Content: &planText})
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].RequiredForReview && !ordered[j].RequiredForReview })
	unique := make([]TaskEvidence, 0, len(ordered))
	seen := make(map[string]string, len(ordered))
	for _, item := range ordered {
		raw, err := json.Marshal(item)
		if err != nil || item.ID == "" {
			return nil, "", ErrInput
		}
		if previous, ok := seen[item.ID]; ok {
			if previous != string(raw) {
				return nil, "", ErrInput
			}
			continue
		}
		seen[item.ID] = string(raw)
		unique = append(unique, item)
	}
	binding.ResultID, err = newPackID()
	if err != nil {
		return nil, "", err
	}
	p := &PagedPack{slice: &sliceCopy, report: report, coverage: selectionReport.Coverage, items: unique, codec: codec, binding: binding, guard: guard}
	p.token, err = codec.Issue(ctx, binding, SignedPosition{LastKey: []string{}}, p.authorize)
	if err != nil {
		return nil, "", err
	}
	return p, p.token, nil
}

func (p *PagedPack) authorize(ctx context.Context, binding CursorBinding) error {
	if p == nil || p.guard == nil || p.slice == nil {
		return ErrAuthority
	}
	if err := p.guard(ctx, binding); err != nil {
		return err
	}
	return p.slice.authorize(ctx)
}

// First emits the first page of this materialization. A fresh pack starts at
// zero and never assumes that a prior conversation retained source content.
func (p *PagedPack) First(ctx context.Context, options PackPageOptions) (PackPage, error) {
	if p == nil {
		return PackPage{}, ErrInput
	}
	return p.page(ctx, p.token, "", p.binding.PrincipalDigest, nil, options, true)
}

// Continue only accepts the original pack token and a cursor from that pack.
// Budget and item limit may change; scope, request and principal may not.
func (p *PagedPack) Continue(ctx context.Context, packToken, cursor, principalDigest string, currentGuard CursorGuard, options PackPageOptions) (PackPage, error) {
	return p.page(ctx, packToken, cursor, principalDigest, currentGuard, options, false)
}

// VerifyOversizedItem checks that the descriptor names the next indivisible
// item in the authenticated materialization. It never returns source bytes.
func (p *PagedPack) VerifyOversizedItem(ctx context.Context, cursor, principal string, current CursorGuard, item *OversizedItemError) error {
	if p == nil || ctx == nil || p.codec == nil || current == nil || item == nil || principal != p.binding.PrincipalDigest {
		return ErrInput
	}
	check := func(ctx context.Context, binding CursorBinding) error {
		if err := p.authorize(ctx, binding); err != nil {
			return err
		}
		return runCursorGuard(ctx, current, binding)
	}
	anchor, err := p.codec.Verify(ctx, p.token, p.binding, check)
	if err != nil {
		return err
	}
	position := SignedPosition{LastKey: []string{}}
	var previous SignedCursor
	if cursor != "" {
		binding := p.binding
		binding.Purpose = "list"
		previous, err = p.codec.Verify(ctx, cursor, binding, check)
		if err != nil {
			return err
		}
		position = previous.Position
	}
	if position.NextItem < 0 || position.NextItem >= int64(len(p.items)) || position.NextByteOffset != 0 || len(position.LastKey) != 0 {
		return ErrCursorInvalid
	}
	candidate := p.items[position.NextItem]
	if candidate.ID != item.ItemID || len(candidate.SourceRefs) != len(item.Sources) {
		return ErrAuthority
	}
	for i := range candidate.SourceRefs {
		if candidate.SourceRefs[i] != item.Sources[i] {
			return ErrAuthority
		}
	}
	if err := p.codec.Recheck(ctx, anchor, check); err != nil {
		return err
	}
	if cursor != "" {
		return p.codec.Recheck(ctx, previous, check)
	}
	return ctx.Err()
}

func (p *PagedPack) page(ctx context.Context, packToken, cursor, principalDigest string, currentGuard CursorGuard, options PackPageOptions, first bool) (PackPage, error) {
	if p == nil || ctx == nil || p.codec == nil || packToken == "" || first && cursor != "" || !first && (cursor == "" || currentGuard == nil) {
		return PackPage{}, ErrInput
	}
	if !cursorHash(principalDigest) || principalDigest != p.binding.PrincipalDigest {
		return PackPage{}, ErrAuthority
	}
	authorize := func(ctx context.Context, binding CursorBinding) error {
		if err := p.authorize(ctx, binding); err != nil {
			return err
		}
		if !first {
			return runCursorGuard(ctx, currentGuard, binding)
		}
		return nil
	}
	packClaims, err := p.codec.Verify(ctx, packToken, p.binding, authorize)
	if err != nil {
		return PackPage{}, err
	}
	previous := SignedPosition{LastKey: []string{}}
	var cursorClaims SignedCursor
	cursorBinding := p.binding
	cursorBinding.Purpose = "list"
	if !first {
		cursorClaims, err = p.codec.Verify(ctx, cursor, cursorBinding, authorize)
		if err != nil {
			return PackPage{}, err
		}
		previous = cursorClaims.Position
	}
	if len(previous.LastKey) != 0 || previous.NextItem > int64(len(p.items)) || previous.NextCoverageGap > int64(len(p.coverage.Gaps)) || previous.NextWarningGap > int64(len(p.report.Gaps)) || previous.NextItem == int64(len(p.items)) && previous.NextByteOffset != 0 {
		return PackPage{}, ErrCursorInvalid
	}
	in, err := normalizeBudget(BudgetInput{Slice: p.slice, RequestID: options.RequestID, PackToken: packToken, MaxOutputBytes: options.MaxOutputBytes, MaxItems: options.MaxItems, DeliveryMode: options.DeliveryMode, VerifyStructuredHost: options.VerifyStructuredHost})
	if err != nil {
		return PackPage{}, err
	}
	if err = verifyBudgetHost(ctx, in); err != nil {
		return PackPage{}, err
	}
	scopeRaw, err := json.Marshal(p.report.Scope)
	if err != nil {
		return PackPage{}, ErrInput
	}
	if err = p.slice.selection.scanner.CheckContent(scopeRaw); err != nil {
		return PackPage{}, err
	}
	coverageRemaining := p.coverage.Gaps[previous.NextCoverageGap:]
	warningsRemaining := p.report.Gaps[previous.NextWarningGap:]
	cv, wv := len(coverageRemaining), len(warningsRemaining)
	if cv > 64 {
		cv = 63
	}
	if wv > 64 {
		wv = 63
	}
	operation := "context_pack"
	if !first {
		operation = "expand_pack"
	}
	render := func(items []model.EvidenceItem, itemPosition SignedPosition, visibleCoverage, visibleWarnings int, cap int64) (PackPage, SignedPosition, error) {
		if err := ctx.Err(); err != nil {
			return PackPage{}, SignedPosition{}, err
		}
		next := itemPosition
		next.NextCoverageGap = previous.NextCoverageGap + int64(visibleCoverage)
		next.NextWarningGap = previous.NextWarningGap + int64(visibleWarnings)
		required := int64(len(coverageRemaining) - visibleCoverage + len(warningsRemaining) - visibleWarnings)
		optional := int64(0)
		for _, item := range p.items[next.NextItem:] {
			if item.RequiredForReview {
				required++
			} else {
				optional++
			}
		}
		truncated := required > 0 || optional > 0
		var nextCursor, reason *string
		if truncated {
			value, _, e := p.codec.issueCandidate(ctx, cursorBinding, next)
			if e != nil {
				return PackPage{}, SignedPosition{}, e
			}
			nextCursor = &value
			why := "OUTPUT_BUDGET"
			if visibleCoverage < len(coverageRemaining) || visibleWarnings < len(warningsRemaining) {
				why = "SAFETY_GAPS_PENDING"
			} else if len(items) >= in.MaxItems {
				why = "ITEM_LIMIT"
			}
			reason = &why
		}
		coverage := p.coverage
		coverage.Gaps = groupedGaps(coverageRemaining, visibleCoverage)
		if visibleCoverage < len(coverageRemaining) && coverage.Status == "supported_scope_complete" {
			coverage.Status = "partial"
		}
		status := "ok"
		if truncated {
			status = "partial"
		}
		target := p.report.Target
		response := model.Response{SchemaVersion: 1, RequestID: in.RequestID, Status: status, Scope: &p.report.Scope, Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}, Coverage: coverage, Readiness: "needs_context", Data: model.PackData{Operation: operation, PackToken: packToken, TargetRefs: []model.GraphRef{&target}, Items: append([]model.EvidenceItem{}, items...), EditPreconditionValidated: false}, Pagination: model.Pagination{Truncated: truncated, EnumerationComplete: true, ReturnedCount: int64(len(items)), RequiredPending: &required, OptionalPending: &optional, NextCursor: nextCursor, LimitReason: reason}, Budget: model.Budget{MaxOutputBytes: cap, DeliveryMode: in.DeliveryMode}, Warnings: groupedGaps(warningsRemaining, visibleWarnings)}
		blocks, e := settlePagedResponse(&response, operation)
		if e != nil {
			return PackPage{}, SignedPosition{}, e
		}
		return PackPage{Response: response, Blocks: blocks}, next, nil
	}
	items := []model.EvidenceItem{}
	position := previous
	var best PackPage
	var bestPosition SignedPosition
	for {
		best, bestPosition, err = render(items, position, cv, wv, in.MaxOutputBytes)
		if !errors.Is(err, errOverBudget) {
			break
		}
		if wv > 0 {
			wv--
			continue
		}
		if cv > 0 {
			cv--
			continue
		}
		return PackPage{}, ErrBudgetTooSmall
	}
	if err != nil {
		return PackPage{}, err
	}
	for int(position.NextItem) < len(p.items) && len(items) < in.MaxItems {
		if err = ctx.Err(); err != nil {
			return PackPage{}, err
		}
		item := p.items[position.NextItem]
		var candidate model.EvidenceItem
		wholeErr := error(nil)
		if position.NextByteOffset == 0 {
			candidate, wholeErr = wireCandidate(p.slice.selection, item)
		} else {
			wholeErr = ErrItemTooLarge
		}
		nextPosition := position
		nextPosition.NextItem++
		nextPosition.NextByteOffset = 0
		trialCV, trialWV := cv, wv
		var page PackPage
		var finalPosition SignedPosition
		if wholeErr == nil {
			trial := append(append([]model.EvidenceItem{}, items...), candidate)
			page, finalPosition, err = render(trial, nextPosition, trialCV, trialWV, in.MaxOutputBytes)
			for errors.Is(err, errOverBudget) && item.RequiredForReview && (trialCV > 0 || trialWV > 0) {
				if trialWV > 0 {
					trialWV--
				} else {
					trialCV--
				}
				page, finalPosition, err = render(trial, nextPosition, trialCV, trialWV, in.MaxOutputBytes)
			}
			if err == nil {
				items, position, best, bestPosition, cv, wv = trial, nextPosition, page, finalPosition, trialCV, trialWV
				continue
			}
			if !errors.Is(err, errOverBudget) {
				return PackPage{}, err
			}
		} else if !errors.Is(wholeErr, ErrItemTooLarge) {
			return PackPage{}, wholeErr
		}
		if len(items) > 0 {
			break
		}
		if item.Kind != "source_range" || !item.RequiredForReview || len(item.SourceRefs) != 1 {
			return PackPage{}, &OversizedItemError{ItemID: item.ID, Sources: append([]model.SourceRef{}, item.SourceRefs...)}
		}
		parent := item.SourceRefs[0]
		offset := position.NextByteOffset
		if offset == 0 {
			offset = parent.Range.StartByte
		}
		remaining := parent.Range.EndByte - offset
		if remaining < 1 {
			return PackPage{}, ErrCursorInvalid
		}
		max := remaining
		if max > 131072 {
			max = 131072
		}
		if offset == parent.Range.StartByte && max == remaining {
			max--
		}
		fitted := false
		for max > 0 {
			fragment, end, complete, e := sourceFragmentCandidate(ctx, p.slice, item.ID, offset, int(max))
			if e != nil {
				if errors.Is(e, ErrBudgetTooSmall) {
					break
				}
				return PackPage{}, e
			}
			fragmentPosition := position
			fragmentPosition.NextByteOffset = end
			if complete {
				fragmentPosition.NextItem++
				fragmentPosition.NextByteOffset = 0
			}
			trialCV, trialWV = cv, wv
			trial := []model.EvidenceItem{fragment}
			page, finalPosition, err = render(trial, fragmentPosition, trialCV, trialWV, in.MaxOutputBytes)
			for errors.Is(err, errOverBudget) && (trialCV > 0 || trialWV > 0) {
				if trialWV > 0 {
					trialWV--
				} else {
					trialCV--
				}
				page, finalPosition, err = render(trial, fragmentPosition, trialCV, trialWV, in.MaxOutputBytes)
			}
			if err == nil {
				items, position, best, bestPosition, cv, wv = trial, fragmentPosition, page, finalPosition, trialCV, trialWV
				fitted = true
				break
			}
			if !errors.Is(err, errOverBudget) {
				return PackPage{}, err
			}
			max /= 2
		}
		if !fitted {
			return PackPage{}, ErrBudgetTooSmall
		}
		break
	}
	if best.Response.Pagination.Truncated && !CursorAdvances(previous, bestPosition) {
		return PackPage{}, ErrContinuationUnavailable
	}
	if err = p.codec.Recheck(ctx, packClaims, authorize); err != nil {
		return PackPage{}, err
	}
	if !first {
		if err = p.codec.Recheck(ctx, cursorClaims, authorize); err != nil {
			return PackPage{}, err
		}
	}
	if next := best.Response.Pagination.NextCursor; next != nil {
		if _, err = p.codec.Verify(ctx, *next, cursorBinding, authorize); err != nil {
			return PackPage{}, err
		}
	}
	if err = verifyBudgetHost(ctx, in); err != nil {
		return PackPage{}, err
	}
	if err = p.codec.Recheck(ctx, packClaims, authorize); err != nil {
		return PackPage{}, err
	}
	if !first {
		if err = p.codec.Recheck(ctx, cursorClaims, authorize); err != nil {
			return PackPage{}, err
		}
	}
	if next := best.Response.Pagination.NextCursor; next != nil {
		if _, err = p.codec.Verify(ctx, *next, cursorBinding, authorize); err != nil {
			return PackPage{}, err
		}
	}
	return best, nil
}

func settlePagedResponse(response *model.Response, operation string) ([][]byte, error) {
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
			if _, err = model.DecodeResponseJSON(raw, int(response.Budget.MaxOutputBytes), 4, operation, false); err != nil {
				return nil, ErrInput
			}
			if err = model.ValidateDeliveredBytes(response.Budget, blocks); err != nil {
				return nil, ErrInput
			}
			return blocks, nil
		}
		estimate = count
	}
	return nil, ErrInput
}
