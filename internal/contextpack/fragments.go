package contextpack

import (
	"bytes"
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// NextSourceFragment returns one exact slice of a selected mandatory source
// item. maxContentBytes limits content alone; the caller must still measure the
// complete serialized response, including its cursor and any repeated blocks.
// An incomplete parent remains a mandatory obligation until its last fragment.
func NextSourceFragment(ctx context.Context, slice *TaskSlice, itemID string, offset int64, maxContentBytes int) (model.EvidenceItem, int64, bool, error) {
	if slice == nil {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	if err := slice.authorize(ctx); err != nil {
		return model.EvidenceItem{}, 0, false, err
	}
	fragment, end, complete, err := sourceFragmentCandidate(ctx, slice, itemID, offset, maxContentBytes)
	if checkErr := slice.authorize(ctx); checkErr != nil {
		return model.EvidenceItem{}, 0, false, checkErr
	}
	return fragment, end, complete, err
}

// Only immutable captured bytes are used while choosing a page size. The
// enclosing pager must perform its full scope/fact/path and cursor postchecks
// before returning any candidate. Direct callers use NextSourceFragment.
func sourceFragmentCandidate(ctx context.Context, slice *TaskSlice, itemID string, offset int64, maxContentBytes int) (model.EvidenceItem, int64, bool, error) {
	if ctx == nil || slice == nil || slice.selection == nil || itemID == "" || maxContentBytes < 1 || maxContentBytes > 131072 {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return model.EvidenceItem{}, 0, false, err
	}
	var item *TaskEvidence
	for i := range slice.report.Items {
		if slice.report.Items[i].ID == itemID {
			if item != nil {
				return model.EvidenceItem{}, 0, false, ErrInput
			}
			item = &slice.report.Items[i]
		}
	}
	if item == nil || item.Kind != "source_range" || !item.RequiredForReview || item.Content == nil || len(item.SourceRefs) != 1 {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	parent := item.SourceRefs[0]
	file, ok := slice.selection.snapshot.File(parent.Path)
	if !ok || file.ContentHash() != parent.ContentHash {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	source := file.Bytes()
	if err := parent.ValidateCapturedBytes(source); err != nil || offset < parent.Range.StartByte || offset >= parent.Range.EndByte || file.CheckRange(int(offset), int(offset)) != nil {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	if !bytes.Equal(source[int(parent.Range.StartByte):int(parent.Range.EndByte)], []byte(*item.Content)) {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	if err := slice.selection.scanner.CheckContent(source); err != nil {
		return model.EvidenceItem{}, 0, false, err
	}
	end := offset + int64(maxContentBytes)
	if end > parent.Range.EndByte {
		end = parent.Range.EndByte
	}
	for end > offset && end < int64(len(source)) && !utf8.RuneStart(source[int(end)]) {
		end--
	}
	if end == offset {
		return model.EvidenceItem{}, 0, false, ErrBudgetTooSmall
	}
	if offset == parent.Range.StartByte && end == parent.Range.EndByte {
		// The caller should send the complete source item instead of calling it
		// a fragment; a fragment must be strictly smaller than its parent.
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	if newline := bytes.LastIndexByte(source[int(offset):int(end)], '\n'); newline >= 0 {
		end = offset + int64(newline) + 1
	}
	child := parent
	child.Range = model.ByteRange{StartByte: offset, EndByte: end}
	id, err := model.SourceFragmentID(parent, child.Range, source, 65536)
	if err != nil {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	content := string(source[int(offset):int(end)])
	out := model.EvidenceItem{ItemID: id, Kind: "source_range_fragment", RequiredForReview: true, WhyIncluded: item.Why, SourceRefs: []model.SourceRef{child}, Content: &content, EvidenceRefs: []model.EvidenceRef{}, Limitations: []model.Gap{}, FragmentOf: &parent}
	raw, err := json.Marshal(out)
	if err != nil {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	if _, err = model.DecodeEvidenceItemJSON(raw, 1024*1024); err != nil {
		return model.EvidenceItem{}, 0, false, ErrInput
	}
	return out, end, end == parent.Range.EndByte, ctx.Err()
}
