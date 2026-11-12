package contextpack

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// wireCandidate translates an internal task item to the sealed EvidenceItem
// shape. It never clips source or changes the meaning of a relation.
func wireCandidate(s *Selection, item TaskEvidence) (model.EvidenceItem, error) {
	kind := item.Kind
	switch kind {
	case "constraint":
		kind = "constraint_excerpt"
	case "test_reference":
		kind = "reference_catalog"
	case "source_range", "constraint_excerpt", "reference_catalog", "boundary", "validation_obligation":
	default:
		return model.EvidenceItem{}, ErrInput
	}
	out := model.EvidenceItem{ItemID: item.ID, Kind: kind, RequiredForReview: item.RequiredForReview, WhyIncluded: item.Why, SourceRefs: append([]model.SourceRef{}, item.SourceRefs...), Content: item.Content, EvidenceRefs: []model.EvidenceRef{}, Limitations: []model.Gap{}}
	if item.Reference != nil {
		r := item.Reference
		body, err := json.Marshal(struct {
			Relation       model.RelationFact `json:"relation"`
			Classification string             `json:"classification"`
		}{r.Relation, r.Classification})
		if err != nil {
			return model.EvidenceItem{}, ErrInput
		}
		value := string(body)
		out.Content = &value
		out.EvidenceRefs = append(out.EvidenceRefs, model.EvidenceRef{EvidenceID: r.Relation.RelationID, Origin: "static_analyzer", Kind: r.Relation.EvidenceKind, SourceRefs: []model.SourceRef{r.Source}})
	}
	if item.Trust == "declared_rule_data" {
		if len(item.SourceRefs) != 1 {
			return model.EvidenceItem{}, ErrInput
		}
		out.EvidenceRefs = append(out.EvidenceRefs, model.EvidenceRef{EvidenceID: item.ID, Origin: "static_analyzer", Kind: "explicit_document", SourceRefs: append([]model.SourceRef{}, item.SourceRefs...)})
	}
	if item.Content != nil && len(item.SourceRefs) == 1 && (item.Kind == "source_range" || item.Kind == "constraint_excerpt") {
		ref := item.SourceRefs[0]
		file, ok := s.snapshot.File(ref.Path)
		if !ok || file.ContentHash() != ref.ContentHash {
			return model.EvidenceItem{}, ErrInput
		}
		body, err := file.Range(int(ref.Range.StartByte), int(ref.Range.EndByte))
		if err != nil || !bytes.Equal(body, []byte(*item.Content)) {
			return model.EvidenceItem{}, ErrInput
		}
	}
	if out.Content != nil && utf8.RuneCountInString(*out.Content) > 131072 {
		return model.EvidenceItem{}, ErrItemTooLarge
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return model.EvidenceItem{}, ErrInput
	}
	if _, err := model.DecodeEvidenceItemJSON(raw, 16*1024*1024); err != nil {
		return model.EvidenceItem{}, ErrInput
	}
	return out, nil
}
