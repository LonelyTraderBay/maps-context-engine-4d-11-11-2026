package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type EvidenceItem struct {
	ItemID            string        `json:"item_id"`
	Kind              string        `json:"kind"`
	RequiredForReview bool          `json:"required_for_review"`
	WhyIncluded       string        `json:"why_included"`
	SourceRefs        []SourceRef   `json:"source_refs"`
	Content           *string       `json:"content"`
	EvidenceRefs      []EvidenceRef `json:"evidence_refs"`
	Limitations       []Gap         `json:"limitations"`
	FragmentOf        *SourceRef    `json:"fragment_of"`
}
type PackData struct {
	Operation                 string         `json:"operation"`
	PackToken                 string         `json:"pack_token"`
	TargetRefs                []GraphRef     `json:"target_refs"`
	Items                     []EvidenceItem `json:"items"`
	EditPreconditionValidated bool           `json:"edit_precondition_validated"`
}

func DecodeEvidenceItemJSON(data []byte, maxBytes int) (EvidenceItem, error) {
	var e EvidenceItem
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return e, err
	}
	f, err := scopeObject(data, []string{"item_id", "kind", "required_for_review", "why_included", "source_refs", "content", "evidence_refs", "limitations", "fragment_of"}, map[string]bool{"content": true, "fragment_of": true})
	if err != nil {
		return e, err
	}
	if _, err := decodeSourceRefs(f["source_refs"], 16, maxBytes); err != nil {
		return e, err
	}
	if _, err := decodeEvidenceRefs(f["evidence_refs"], 16, maxBytes); err != nil {
		return e, err
	}
	if _, err := decodeGaps(f["limitations"], 16, maxBytes); err != nil {
		return e, err
	}
	if !bytes.Equal(bytes.TrimSpace(f["fragment_of"]), []byte("null")) {
		if _, err := DecodeSourceRefJSON(f["fragment_of"], maxBytes); err != nil {
			return e, err
		}
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return EvidenceItem{}, err
	}
	if !validID(e.ItemID) || !validID(e.WhyIncluded) || e.SourceRefs == nil || e.EvidenceRefs == nil || e.Limitations == nil || e.Content != nil && !stringAtMost(*e.Content, 131072) {
		return EvidenceItem{}, fmt.Errorf("invalid evidence item")
	}
	switch e.Kind {
	case "source_range", "reference_catalog", "constraint_excerpt", "boundary", "overview", "validation_obligation", "source_range_fragment":
	default:
		return EvidenceItem{}, fmt.Errorf("invalid item kind")
	}
	if e.Kind == "source_range_fragment" {
		if e.FragmentOf == nil || len(e.SourceRefs) != 1 || e.Content == nil || !lowerHex(e.ItemID, 64) || int64(len(*e.Content)) != e.SourceRefs[0].Range.EndByte-e.SourceRefs[0].Range.StartByte {
			return EvidenceItem{}, fmt.Errorf("invalid source fragment")
		}
		p, c := e.FragmentOf, e.SourceRefs[0]
		if p.WorktreeID != c.WorktreeID || p.ProfileID != c.ProfileID || p.GenerationID != c.GenerationID || p.Path != c.Path || p.ContentHash != c.ContentHash || c.Range.StartByte < p.Range.StartByte || c.Range.EndByte > p.Range.EndByte || c.Range.StartByte >= c.Range.EndByte {
			return EvidenceItem{}, fmt.Errorf("fragment parent mismatch")
		}
	} else if e.FragmentOf != nil {
		return EvidenceItem{}, fmt.Errorf("nonfragment has parent")
	}
	return e, nil
}
func (e EvidenceItem) ValidateFragment(source []byte, maxBytes int) error {
	if e.Kind != "source_range_fragment" || e.FragmentOf == nil || len(e.SourceRefs) != 1 || e.Content == nil {
		return fmt.Errorf("not a valid fragment")
	}
	return ValidateSourceFragment(*e.FragmentOf, e.SourceRefs[0], *e.Content, e.ItemID, source, maxBytes)
}

// DecodePackDataJSON checks a wire page, not page continuity, scope freshness,
// evidence authorization or whether editing is actually safe.
func DecodePackDataJSON(data []byte, maxBytes int) (PackData, error) {
	var p PackData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	f, err := scopeObject(data, []string{"operation", "pack_token", "target_refs", "items", "edit_precondition_validated"}, nil)
	if err != nil {
		return p, err
	}
	p.TargetRefs, err = decodeGraphRefs(f["target_refs"], 0, 32, maxBytes)
	if err != nil {
		return p, err
	}
	delete(f, "target_refs")
	var items []json.RawMessage
	if err := json.Unmarshal(f["items"], &items); err != nil {
		return p, err
	}
	if items == nil || len(items) > 512 {
		return p, fmt.Errorf("invalid pack items")
	}
	delete(f, "items")
	raw, err := json.Marshal(f)
	if err != nil {
		return p, err
	}
	var body PackData
	if err := json.Unmarshal(raw, &body); err != nil {
		return p, err
	}
	body.TargetRefs = p.TargetRefs
	body.Items = make([]EvidenceItem, 0, len(items))
	if body.Operation != "context_pack" && body.Operation != "expand_pack" || !validToken(body.PackToken) {
		return PackData{}, fmt.Errorf("invalid pack data")
	}
	for _, item := range items {
		value, err := DecodeEvidenceItemJSON(item, maxBytes)
		if err != nil {
			return PackData{}, err
		}
		body.Items = append(body.Items, value)
	}
	return body, nil
}
