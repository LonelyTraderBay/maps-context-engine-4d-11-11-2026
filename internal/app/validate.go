package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrValidationInput        = errors.New("invalid source validation input")
	ErrValidationAuthority    = errors.New("validation authority could not be verified")
	ErrValidationScopeChanged = errors.New("pinned validation scope changed")
	ErrValidationPrecondition = errors.New("pinned source precondition could not be verified")
	ErrFreshnessChanged       = errors.New("workspace changed since the pinned capture")
	ErrFreshnessUnknown       = errors.New("workspace freshness could not be established")
)

type WorkspaceObservation string

const (
	WorkspaceObservedCurrent WorkspaceObservation = "observed_current"
	WorkspaceChanged         WorkspaceObservation = "changed"
	WorkspaceUnknown         WorkspaceObservation = "unknown"
)

type ValidationSource struct {
	Ref       model.SourceRef `json:"source_ref"`
	ServiceID *string         `json:"service_id"`
}

type ValidateCapturedInput struct {
	Scope            model.ScopeVector
	Sources          []ValidationSource
	MaxInputBytes    int
	MaxManifestBytes int
	CurrentScope     func(context.Context) (model.ScopeVector, error)
	ResolveCapture   func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error)
	AuthorizeSource  func(context.Context, model.ScopeMember, model.SourceRef) error
	VerifyWorkspace  func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error)
}

type CapturedValidation struct {
	Data      model.ValidateData `json:"data"`
	Freshness model.Freshness    `json:"freshness"`
}

// ValidateCapturedWorkspace is an internal coordinator for an already
// authenticated, purpose-bound pack/result token. CurrentScope must read the
// actual current catalog/group/policy/record/binding epochs. ResolveCapture
// must bind the requested published generation and index incarnation. The
// workspace verifier must perform a full bounded membership, content and
// configuration check; callbacks are trust boundaries, not evidence by
// themselves. This method grants no permission to mutate source.
func ValidateCapturedWorkspace(ctx context.Context, input ValidateCapturedInput) (CapturedValidation, error) {
	if ctx == nil || input.MaxInputBytes < 1024 || input.MaxInputBytes > 1024*1024 || input.MaxManifestBytes < 1024 || input.MaxManifestBytes > 64*1024*1024 || len(input.Sources) < 1 || len(input.Sources) > 256 || input.CurrentScope == nil || input.ResolveCapture == nil || input.AuthorizeSource == nil || input.VerifyWorkspace == nil {
		return CapturedValidation{}, ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return CapturedValidation{}, ErrValidationInput
	}
	if err := validationContextError(ctx); err != nil {
		return CapturedValidation{}, err
	}
	if !validationInputsWithinBudget(input.Scope, input.Sources, input.MaxInputBytes) {
		return CapturedValidation{}, ErrValidationInput
	}
	encodedScope, err := json.Marshal(input.Scope)
	if err != nil || len(encodedScope) > input.MaxInputBytes {
		return CapturedValidation{}, ErrValidationInput
	}
	decodedScope, err := model.DecodeScopeVectorJSON(encodedScope, input.MaxInputBytes)
	if err != nil {
		return CapturedValidation{}, ErrValidationInput
	}
	scope, err := model.NormalizeScopeVector(decodedScope, input.MaxInputBytes)
	if err != nil {
		return CapturedValidation{}, ErrValidationInput
	}
	if len(scope.Members) == 0 || len(scope.UnavailableMembers) != 0 {
		return CapturedValidation{}, ErrFreshnessUnknown
	}
	sources := make([]ValidationSource, len(input.Sources))
	for i, source := range input.Sources {
		sources[i] = source
		if source.ServiceID != nil {
			id := *source.ServiceID
			sources[i].ServiceID = &id
		}
	}
	raw, err := json.Marshal(sources)
	if err != nil || len(raw) > input.MaxInputBytes {
		return CapturedValidation{}, ErrValidationInput
	}
	postcheck := func() error {
		current, err := input.CurrentScope(ctx)
		if err != nil {
			if cause := validationContextError(ctx); cause != nil {
				return cause
			}
			return ErrValidationAuthority
		}
		if err := model.ComparePinnedScope(scope, current, input.MaxInputBytes); err != nil {
			if errors.Is(err, model.ErrScopeChanged) {
				return ErrValidationScopeChanged
			}
			return ErrValidationAuthority
		}
		return validationContextError(ctx)
	}
	if err := postcheck(); err != nil {
		return CapturedValidation{}, err
	}
	captures := make([]*snapshot.Snapshot, len(scope.Members))
	for i, member := range scope.Members {
		if err := validationContextError(ctx); err != nil {
			return CapturedValidation{}, err
		}
		captured, err := input.ResolveCapture(ctx, copyValidationMember(member))
		if err != nil || captured == nil {
			if cause := validationContextError(ctx); cause != nil {
				return CapturedValidation{}, cause
			}
			return CapturedValidation{}, ErrValidationAuthority
		}
		manifest := captured.Manifest()
		if model.VerifyCaptureDigest(manifest, input.MaxManifestBytes) != nil || manifest.WorktreeID != member.WorktreeID || manifest.ProfileID != member.ProfileID || manifest.PolicyDigest != member.PolicyDigest || manifest.SnapshotDigest != member.SnapshotDigest {
			return CapturedValidation{}, ErrValidationPrecondition
		}
		captures[i] = captured
	}
	memberIndexes := make([]int, len(sources))
	grouped := make([][]snapshot.ExpectedSource, len(scope.Members))
	for i, source := range sources {
		if err := validationContextError(ctx); err != nil {
			return CapturedValidation{}, err
		}
		member, err := model.MatchPinnedSource(scope, source.Ref, source.ServiceID, input.MaxInputBytes)
		if err != nil {
			return CapturedValidation{}, ErrValidationPrecondition
		}
		index := -1
		for j, candidate := range scope.Members {
			if reflect.DeepEqual(candidate, member) {
				index = j
				break
			}
		}
		if index < 0 {
			return CapturedValidation{}, ErrValidationPrecondition
		}
		if err := input.AuthorizeSource(ctx, copyValidationMember(member), source.Ref); err != nil {
			if cause := validationContextError(ctx); cause != nil {
				return CapturedValidation{}, cause
			}
			return CapturedValidation{}, ErrValidationAuthority
		}
		memberIndexes[i] = index
		grouped[index] = append(grouped[index], snapshot.ExpectedSource{Ref: source.Ref, ServiceID: source.ServiceID})
	}
	for i, group := range grouped {
		if len(group) == 0 {
			continue
		}
		if err := snapshot.ValidateCapturedSources(ctx, scope, group, captures[i], input.MaxManifestBytes); err != nil {
			if cause := validationContextError(ctx); cause != nil {
				return CapturedValidation{}, cause
			}
			return CapturedValidation{}, ErrValidationPrecondition
		}
	}
	changed := false
	for i, member := range scope.Members {
		if err := validationContextError(ctx); err != nil {
			return CapturedValidation{}, err
		}
		observation, err := input.VerifyWorkspace(ctx, copyValidationMember(member), captures[i])
		if err != nil {
			if cause := validationContextError(ctx); cause != nil {
				return CapturedValidation{}, cause
			}
			return CapturedValidation{}, ErrFreshnessUnknown
		}
		switch observation {
		case WorkspaceObservedCurrent:
		case WorkspaceChanged:
			changed = true
		case WorkspaceUnknown:
			return CapturedValidation{}, ErrFreshnessUnknown
		default:
			return CapturedValidation{}, ErrValidationAuthority
		}
	}
	if err := postcheck(); err != nil {
		return CapturedValidation{}, err
	}
	if changed {
		return CapturedValidation{}, ErrFreshnessChanged
	}
	for i, source := range sources {
		if err := validationContextError(ctx); err != nil {
			return CapturedValidation{}, err
		}
		if err := input.AuthorizeSource(ctx, copyValidationMember(scope.Members[memberIndexes[i]]), source.Ref); err != nil {
			if cause := validationContextError(ctx); cause != nil {
				return CapturedValidation{}, cause
			}
			return CapturedValidation{}, ErrValidationAuthority
		}
	}
	if err := postcheck(); err != nil {
		return CapturedValidation{}, err
	}
	checked := time.Now().UTC().Format(time.RFC3339Nano)
	count := int64(0)
	if err := validationContextError(ctx); err != nil {
		return CapturedValidation{}, err
	}
	return CapturedValidation{Data: model.ValidateData{Operation: "validate_pack", EditPreconditionValidated: true, ChangedSources: []model.SourceRef{}},
		Freshness: model.Freshness{Status: "observed_current", Scope: "workspace", CheckedAt: &checked, BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", ChangedCount: &count, SourceView: "indexed_snapshot"}}, nil
}

func validationInputsWithinBudget(scope model.ScopeVector, sources []ValidationSource, limit int) bool {
	if len(scope.Members) > 32 || len(scope.UnavailableMembers) > 32 || len(sources) > 256 {
		return false
	}
	remaining := limit
	add := func(values ...string) bool {
		for _, value := range values {
			if len(value) > remaining {
				return false
			}
			remaining -= len(value)
		}
		return true
	}
	optional := func(value *string) bool { return value == nil || add(*value) }
	if !add(scope.Kind, scope.VectorDigest, scope.CatalogIncarnation) || !optional(scope.GroupID) {
		return false
	}
	for _, member := range scope.Members {
		if !add(member.WorktreeID, member.ProfileID, member.GenerationID, member.SnapshotDigest, member.PolicyDigest, member.IndexIncarnation, member.RecordsIncarnation) || !optional(member.ServiceID) {
			return false
		}
	}
	for _, member := range scope.UnavailableMembers {
		if !optional(member.ServiceID) || !optional(member.WorktreeID) || !optional(member.ProfileID) || !add(member.Status, member.ReasonCode) {
			return false
		}
	}
	for _, source := range sources {
		ref := source.Ref
		if !add(ref.WorktreeID, ref.ProfileID, ref.GenerationID, ref.Path, ref.ContentHash) || !optional(source.ServiceID) {
			return false
		}
	}
	return true
}

func copyValidationMember(member model.ScopeMember) model.ScopeMember {
	if member.ServiceID != nil {
		id := *member.ServiceID
		member.ServiceID = &id
	}
	return member
}

func validationContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrFreshnessUnknown
	}
	return ctx.Err()
}
