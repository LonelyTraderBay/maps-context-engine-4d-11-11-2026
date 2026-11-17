package app

import "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"

// Validate the identities of every source-bearing field against one verified
// vector. Actual file contents and actor authorization remain backend and
// authority responsibilities. Ambiguous multi-service identities fail closed.
func validateCoreResponseScope(response model.Response, maxBytes int) error {
	if response.Scope != nil && model.VerifyScopeVectorDigest(*response.Scope, maxBytes) != nil {
		return ErrCoreBackend
	}
	checkRef := func(ref model.SourceRef) error {
		if response.Scope == nil {
			return ErrCoreBackend
		}
		hash := ref.ContentHash
		file := &model.FileRef{Kind: "file", GraphIdentity: model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}, Path: ref.Path, ContentHash: &hash}
		if _, err := model.MatchPinnedMember(*response.Scope, file, nil); err != nil {
			return ErrCoreBackend
		}
		return nil
	}
	checkRefs := func(refs []model.SourceRef) error {
		for _, ref := range refs {
			if err := checkRef(ref); err != nil {
				return err
			}
		}
		return nil
	}
	checkGaps := func(gaps []model.Gap) error {
		for _, gap := range gaps {
			if err := checkRefs(gap.SourceRefs); err != nil {
				return err
			}
		}
		return nil
	}
	checkEvidence := func(evidence []model.EvidenceRef) error {
		for _, item := range evidence {
			if err := checkRefs(item.SourceRefs); err != nil {
				return err
			}
		}
		return nil
	}
	if checkGaps(response.Coverage.Gaps) != nil || checkGaps(response.Warnings) != nil {
		return ErrCoreBackend
	}
	if response.Error != nil && checkGaps(response.Error.Details) != nil {
		return ErrCoreBackend
	}
	switch data := response.Data.(type) {
	case nil, model.StatusData:
		return nil
	case model.LocateData:
		if response.Scope == nil {
			return ErrCoreBackend
		}
		for _, candidate := range data.Candidates {
			if _, err := model.MatchPinnedMember(*response.Scope, candidate.Ref, nil); err != nil || checkRefs(candidate.SourceRefs) != nil || checkEvidence(candidate.EvidenceRefs) != nil {
				return ErrCoreBackend
			}
		}
	case model.PackData:
		if response.Scope == nil {
			return ErrCoreBackend
		}
		for _, ref := range data.TargetRefs {
			if _, err := model.MatchPinnedMember(*response.Scope, ref, nil); err != nil {
				return ErrCoreBackend
			}
		}
		for _, item := range data.Items {
			if checkRefs(item.SourceRefs) != nil || checkEvidence(item.EvidenceRefs) != nil || checkGaps(item.Limitations) != nil || item.FragmentOf != nil && checkRef(*item.FragmentOf) != nil {
				return ErrCoreBackend
			}
		}
	case model.ValidateData:
		if response.Scope == nil || checkRefs(data.ChangedSources) != nil || data.EditPreconditionValidated && (response.Freshness.Status != "observed_current" || response.Freshness.Scope != "workspace") {
			return ErrCoreBackend
		}
	case model.RenameData:
		if response.Scope == nil || data.PreviewComplete && response.Pagination.Truncated || checkGaps(data.Conflicts) != nil {
			return ErrCoreBackend
		}
		for _, edit := range data.Edits {
			if checkRef(edit.SourceRef) != nil {
				return ErrCoreBackend
			}
		}
	default:
		return ErrCoreBackend
	}
	return nil
}
