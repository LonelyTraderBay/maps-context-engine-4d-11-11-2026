package model

import (
	"encoding/json"
	"fmt"
)

type ProposedEdit struct {
	SourceRef SourceRef `json:"source_ref"`
	NewText   string    `json:"new_text"`
}
type RenameData struct {
	Operation           string         `json:"operation"`
	Applied             bool           `json:"applied"`
	SourceMutationCount int64          `json:"source_mutation_count"`
	ValidationRequired  bool           `json:"validation_required"`
	Edits               []ProposedEdit `json:"edits"`
	Conflicts           []Gap          `json:"conflicts"`
	PreviewComplete     bool           `json:"preview_complete"`
	ResultToken         string         `json:"result_token"`
	ResultID            string         `json:"result_id"`
	ResultDigest        string         `json:"result_digest"`
	TotalEdits          int64          `json:"total_edits"`
	ComputationComplete bool           `json:"computation_complete"`
}

func DecodeProposedEditJSON(data []byte, maxBytes int) (ProposedEdit, error) {
	var e ProposedEdit
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return e, err
	}
	f, err := scopeObject(data, []string{"source_ref", "new_text"}, nil)
	if err != nil {
		return e, err
	}
	ref, err := DecodeSourceRefJSON(f["source_ref"], maxBytes)
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return ProposedEdit{}, err
	}
	e.SourceRef = ref
	if !stringAtMost(e.NewText, 131072) {
		return ProposedEdit{}, fmt.Errorf("invalid replacement text")
	}
	return e, nil
}

// DecodeRenameDataJSON enforces the read-only envelope. Page continuity,
// complete edit set, current source and apply authorization remain external.
func DecodeRenameDataJSON(data []byte, maxBytes int) (RenameData, error) {
	var r RenameData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "applied", "source_mutation_count", "validation_required", "edits", "conflicts", "preview_complete", "result_token", "result_id", "result_digest", "total_edits", "computation_complete"}, nil)
	if err != nil {
		return r, err
	}
	var edits, conflicts []json.RawMessage
	if err := json.Unmarshal(f["edits"], &edits); err != nil {
		return r, err
	}
	if err := json.Unmarshal(f["conflicts"], &conflicts); err != nil {
		return r, err
	}
	if len(edits) > 512 || len(conflicts) > 128 {
		return r, fmt.Errorf("too many rename results")
	}
	for _, e := range edits {
		if _, err := DecodeProposedEditJSON(e, maxBytes); err != nil {
			return r, err
		}
	}
	for _, c := range conflicts {
		if _, err := DecodeGapJSON(c, maxBytes); err != nil {
			return r, err
		}
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return RenameData{}, err
	}
	if r.Operation != "rename_preview" || r.Applied || r.SourceMutationCount != 0 || !r.ValidationRequired || !r.ComputationComplete || r.Edits == nil || r.Conflicts == nil || !validToken(r.ResultToken) || !uuid(r.ResultID) || !lowerHex(r.ResultDigest, 64) || r.TotalEdits < 0 || r.TotalEdits > 10000 || int64(len(r.Edits)) > r.TotalEdits {
		return RenameData{}, fmt.Errorf("invalid rename data")
	}
	return r, nil
}
