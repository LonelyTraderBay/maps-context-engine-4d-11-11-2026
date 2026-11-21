package records

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/domain/state"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type StateModelMutationRequest struct {
	MutationRequest
	Source     *model.SourceRef
	ReadSource func(context.Context, model.SourceRef, int) ([]byte, error)
}

// MutateStateModel uses the owner-authenticated records transaction and the
// authorized immutable source capture. No file-supplied status grants approval.
func (s *Store) MutateStateModel(ctx context.Context, request StateModelMutationRequest) (MutationResult, error) {
	if request.Mapping != nil || request.CheckEvidence != nil || request.ReadSource == nil ||
		(request.Action == "propose") != (request.Source != nil) {
		return MutationResult{}, ErrInvalidStore
	}
	return s.mutate(ctx, request.MutationRequest, "state_model", func(worktree string) (Version, error) {
		source := *request.Source
		if source.WorktreeID != worktree || !state.EligibleSource(source) {
			return Version{}, ErrInvalidStore
		}
		return Version{SourceRefs: []model.SourceRef{source}, StateModelSource: true}, nil
	}, func(ctx context.Context, record Version) error {
		if len(record.SourceRefs) != 1 || record.SourceRefs[0].WorktreeID != s.identity.WorktreeID ||
			!state.EligibleSource(record.SourceRefs[0]) {
			return ErrIntegrity
		}
		ref := record.SourceRefs[0]
		raw, err := request.ReadSource(ctx, ref, contracts.StateModelMaxBytes)
		if err != nil {
			return err
		}
		_, err = state.DecodeCaptured(ref, raw)
		return err
	})
}

func stateModelPayload(refs []model.SourceRef) ([]byte, string, error) {
	if len(refs) != 1 || !state.EligibleSource(refs[0]) {
		return nil, "", ErrIntegrity
	}
	raw, err := json.Marshal(struct {
		Format string          `json:"format"`
		Source model.SourceRef `json:"source_ref"`
	}{state.Format, refs[0]})
	if err != nil {
		return nil, "", err
	}
	decoded, err := model.DecodeIdentityJSON(raw, maxRecordBytes)
	if err != nil {
		return nil, "", err
	}
	_, digest, err := model.EncodeCanonical("state-model-record-v1", decoded, maxRecordBytes)
	return raw, digest, err
}
