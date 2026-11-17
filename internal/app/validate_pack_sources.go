package app

import (
	"context"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func copyValidationService(service *string) *string {
	if service == nil {
		return nil
	}
	value := *service
	return &value
}

func requestMatchesScope(request *model.ScopeRequest, scope model.ScopeVector, member model.ScopeMember, limit int) bool {
	if request == nil {
		return true // the authenticated guard must verify a unique binding
	}
	mode, err := request.SelectionMode(1)
	if err != nil {
		return false
	}
	switch mode {
	case "bound":
		return true // actual binding count is verified by the guard
	case "pinned":
		return model.ComparePinnedScope(*request.PinnedVector, scope, limit) == nil
	case "group":
		return scope.Kind == "federated" && scope.GroupID != nil && *scope.GroupID == *request.GroupID
	case "single":
		return scope.Kind == "single" && *request.WorktreeID == member.WorktreeID && *request.ProfileID == member.ProfileID && (request.GenerationID == nil || *request.GenerationID == member.GenerationID)
	default:
		return false
	}
}

func sliceMatchesConstraints(report contextpack.SliceReport, expected []string) bool {
	counts := make(map[string]int, len(expected))
	for _, value := range expected {
		counts[value]++
	}
	for _, item := range report.Items {
		if item.Kind == "constraint" {
			if item.Content == nil || counts[*item.Content] == 0 {
				return false
			}
			counts[*item.Content]--
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func sliceMatchesSelector(report contextpack.SliceReport, selector model.Selector) bool {
	switch sel := selector.(type) {
	case *model.SymbolSelector:
		return sel != nil && sel.SymbolID == report.Target.SymbolID
	case *model.RefSelector:
		if sel == nil {
			return false
		}
		ref, ok := sel.Ref.(*model.SymbolRef)
		return ok && ref != nil && *ref == report.Target
	}
	return false
}

// Dedupe by file identity and reject conflicting hashes. Full workspace
// validation hashes whole files, so one pinned range per distinct file is
// sufficient for its source precondition. More than 256 files fails closed.
type sourceCollector struct {
	service *string
	values  []ValidationSource
	seen    map[string]string
	used    int
	limit   int
}

func newSourceCollector(service *string, limit int) *sourceCollector {
	return &sourceCollector{service: copyValidationService(service), values: []ValidationSource{}, seen: map[string]string{}, limit: limit}
}

func (c *sourceCollector) add(ref model.SourceRef) error {
	if c == nil || len(ref.WorktreeID)+len(ref.ProfileID)+len(ref.GenerationID)+len(ref.Path)+len(ref.ContentHash)+3 > c.limit {
		return ErrFreshnessUnknown
	}
	key := ref.WorktreeID + "\x00" + ref.ProfileID + "\x00" + ref.GenerationID + "\x00" + ref.Path
	if hash, ok := c.seen[key]; ok {
		if hash != ref.ContentHash {
			return ErrValidationPrecondition
		}
		return nil
	}
	if len(key)+len(ref.ContentHash) > c.limit-c.used {
		return ErrFreshnessUnknown
	}
	if len(c.values) == 256 {
		return ErrFreshnessUnknown
	}
	c.seen[key] = ref.ContentHash
	c.used += len(key) + len(ref.ContentHash)
	c.values = append(c.values, ValidationSource{Ref: ref, ServiceID: copyValidationService(c.service)})
	return nil
}

func (c *sourceCollector) gaps(gaps []model.Gap) error {
	for _, gap := range gaps {
		for _, ref := range gap.SourceRefs {
			if err := c.add(ref); err != nil {
				return err
			}
		}
	}
	return nil
}

func sliceValidationSources(report contextpack.SliceReport, service *string, limit int) ([]ValidationSource, error) {
	c := newSourceCollector(service, limit)
	for _, item := range report.Items {
		for _, ref := range item.SourceRefs {
			if err := c.add(ref); err != nil {
				return nil, err
			}
		}
	}
	for _, ref := range report.Validation.TestReferences {
		if err := c.add(ref); err != nil {
			return nil, err
		}
	}
	if err := c.gaps(report.Gaps); err != nil {
		return nil, err
	}
	if len(c.values) == 0 || !validationInputsWithinBudget(report.Scope, c.values, limit) {
		return nil, ErrValidationInput
	}
	return c.values, nil
}

func pageValidationSources(response model.Response, service *string, limit int) ([]ValidationSource, error) {
	data, ok := response.Data.(model.PackData)
	if !ok || response.Scope == nil {
		return nil, ErrValidationInput
	}
	c := newSourceCollector(service, limit)
	for _, item := range data.Items {
		for _, ref := range item.SourceRefs {
			if err := c.add(ref); err != nil {
				return nil, err
			}
		}
		for _, evidence := range item.EvidenceRefs {
			for _, ref := range evidence.SourceRefs {
				if err := c.add(ref); err != nil {
					return nil, err
				}
			}
		}
		if item.FragmentOf != nil {
			if err := c.add(*item.FragmentOf); err != nil {
				return nil, err
			}
		}
		if err := c.gaps(item.Limitations); err != nil {
			return nil, err
		}
	}
	if err := c.gaps(response.Coverage.Gaps); err != nil {
		return nil, err
	}
	if err := c.gaps(response.Warnings); err != nil {
		return nil, err
	}
	if !validationInputsWithinBudget(*response.Scope, c.values, limit) {
		return nil, ErrValidationInput
	}
	return c.values, nil
}

func validateReturnedFiles(ctx context.Context, input ValidateCapturedInput, sources []ValidationSource, capture CaptureLiveSource) (model.Freshness, error) {
	if ctx == nil || capture == nil || len(sources) == 0 || len(sources) > 256 {
		return model.Freshness{}, ErrValidationInput
	}
	postcheck := func() error {
		current, err := input.CurrentScope(ctx)
		if err != nil {
			if cause := validationContextError(ctx); cause != nil {
				return cause
			}
			return ErrValidationAuthority
		}
		if err := model.ComparePinnedScope(input.Scope, current, input.MaxInputBytes); err != nil {
			return ErrValidationScopeChanged
		}
		return validationContextError(ctx)
	}
	if err := postcheck(); err != nil {
		return model.Freshness{}, err
	}
	for _, source := range sources {
		if err := validationContextError(ctx); err != nil {
			return model.Freshness{}, err
		}
		member, err := model.MatchPinnedSource(input.Scope, source.Ref, source.ServiceID, input.MaxInputBytes)
		if err != nil {
			return model.Freshness{}, ErrValidationPrecondition
		}
		captured, err := input.ResolveCapture(ctx, copyValidationMember(member))
		if err != nil || captured == nil || snapshot.ValidateCapturedSource(input.Scope, source.Ref, source.ServiceID, captured, input.MaxManifestBytes) != nil {
			if cause := validationContextError(ctx); cause != nil {
				return model.Freshness{}, cause
			}
			return model.Freshness{}, ErrValidationPrecondition
		}
		if err := input.AuthorizeSource(ctx, copyValidationMember(member), source.Ref); err != nil {
			return model.Freshness{}, packContextError(ctx, ErrValidationAuthority)
		}
		for i := 0; i < 2; i++ {
			file, err := capture(ctx, copyValidationMember(member), source.Ref.Path)
			if err != nil {
				if cause := validationContextError(ctx); cause != nil {
					return model.Freshness{}, cause
				}
				return model.Freshness{}, ErrFreshnessUnknown
			}
			if file.Path() != source.Ref.Path || file.ContentHash() != source.Ref.ContentHash || source.Ref.Range.EndByte > int64(file.Len()) || source.Ref.Range.StartByte < 0 || file.CheckRange(int(source.Ref.Range.StartByte), int(source.Ref.Range.EndByte)) != nil {
				return model.Freshness{}, ErrFreshnessChanged
			}
		}
		if err := input.AuthorizeSource(ctx, copyValidationMember(member), source.Ref); err != nil {
			return model.Freshness{}, packContextError(ctx, ErrValidationAuthority)
		}
	}
	if err := postcheck(); err != nil {
		return model.Freshness{}, err
	}
	checked, zero := time.Now().UTC().Format(time.RFC3339Nano), int64(0)
	return model.Freshness{Status: "observed_current", Scope: "returned_files", CheckedAt: &checked, BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", ChangedCount: &zero, SourceView: "indexed_snapshot"}, nil
}
