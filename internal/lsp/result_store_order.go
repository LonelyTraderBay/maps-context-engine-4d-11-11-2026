package lsp

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// Prefix bytes from EncodeCanonical are identical for all entries, so they do
// not alter the specified lexicographic ordering by E(the complete value).
func resultOrderBytes(value any, limit int) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoded, err := model.DecodeIdentityJSON(raw, limit)
	if err != nil {
		return nil, err
	}
	encoded, _, err := model.EncodeCanonical("rename-order-v1", decoded, limit)
	return encoded, err
}

func normalizeRenamePayload(payload RenameResultPayload, limit int) (RenameResultPayload, error) {
	out := payload
	out.FilePreconditions = append([]model.RenameFilePrecondition{}, payload.FilePreconditions...)
	sort.Slice(out.FilePreconditions, func(i, j int) bool {
		a, b := out.FilePreconditions[i], out.FilePreconditions[j]
		for _, pair := range [][2]string{{a.WorktreeID, b.WorktreeID}, {a.ProfileID, b.ProfileID}, {a.GenerationID, b.GenerationID}, {a.Path, b.Path}, {a.ContentHash, b.ContentHash}} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
	type orderedEdit struct {
		value   model.ProposedEdit
		encoded []byte
	}
	edits := make([]orderedEdit, 0, len(payload.Edits))
	for _, edit := range payload.Edits {
		encoded, err := resultOrderBytes(edit, limit)
		if err != nil {
			return RenameResultPayload{}, err
		}
		edits = append(edits, orderedEdit{edit, encoded})
	}
	sort.Slice(edits, func(i, j int) bool {
		a, b := edits[i].value, edits[j].value
		if a.SourceRef.Path != b.SourceRef.Path {
			return a.SourceRef.Path < b.SourceRef.Path
		}
		if a.SourceRef.Range.StartByte != b.SourceRef.Range.StartByte {
			return a.SourceRef.Range.StartByte < b.SourceRef.Range.StartByte
		}
		if a.SourceRef.Range.EndByte != b.SourceRef.Range.EndByte {
			return a.SourceRef.Range.EndByte < b.SourceRef.Range.EndByte
		}
		if a.NewText != b.NewText {
			return a.NewText < b.NewText
		}
		return bytes.Compare(edits[i].encoded, edits[j].encoded) < 0
	})
	out.Edits = make([]model.ProposedEdit, 0, len(edits))
	for _, edit := range edits {
		if len(out.Edits) == 0 || out.Edits[len(out.Edits)-1] != edit.value {
			out.Edits = append(out.Edits, edit.value)
		}
	}
	type orderedGap struct {
		value   model.Gap
		encoded []byte
	}
	gaps := make([]orderedGap, 0, len(payload.Conflicts))
	for _, gap := range payload.Conflicts {
		encoded, err := resultOrderBytes(gap, limit)
		if err != nil {
			return RenameResultPayload{}, err
		}
		gaps = append(gaps, orderedGap{gap, encoded})
	}
	sort.Slice(gaps, func(i, j int) bool { return bytes.Compare(gaps[i].encoded, gaps[j].encoded) < 0 })
	out.Conflicts = make([]model.Gap, 0, len(gaps))
	for _, gap := range gaps {
		out.Conflicts = append(out.Conflicts, gap.value)
	}
	return out, nil
}
