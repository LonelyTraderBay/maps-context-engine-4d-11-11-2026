package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type RenameFilePrecondition struct {
	WorktreeID   string `json:"worktree_id"`
	ProfileID    string `json:"profile_id"`
	GenerationID string `json:"generation_id"`
	Path         string `json:"path"`
	ContentHash  string `json:"content_hash"`
}

func renameFileKey(p RenameFilePrecondition) string {
	return p.WorktreeID + "\x00" + p.ProfileID + "\x00" + p.GenerationID + "\x00" + p.Path
}
func renamePrecondition(r SourceRef) RenameFilePrecondition {
	return RenameFilePrecondition{r.WorktreeID, r.ProfileID, r.GenerationID, r.Path, r.ContentHash}
}

// RenameResultDigest only hashes a complete caller-provided result. It does not
// run a provider, verify captured source bytes, authorize edits or apply them.
func RenameResultDigest(scope ScopeVector, initial RenameInitialRequest, providerDigest string, preconditions []RenameFilePrecondition, edits []ProposedEdit, conflicts []Gap, maxBytes int) (string, error) {
	if maxBytes <= 0 || !lowerHex(providerDigest, 64) || preconditions == nil || edits == nil || conflicts == nil || len(preconditions) > maxBytes || len(edits) > 10000 || len(conflicts) > maxBytes {
		return "", fmt.Errorf("invalid rename result budget")
	}
	normalizedScope, err := NormalizeScopeVector(scope, maxBytes)
	if err != nil {
		return "", err
	}
	scope = normalizedScope
	reqRaw, err := json.Marshal(initial)
	if err != nil {
		return "", err
	}
	req, err := DecodeRenameRequestJSON(reqRaw, maxBytes)
	if err != nil {
		return "", err
	}
	initialRequest, ok := req.(*RenameInitialRequest)
	if !ok {
		return "", fmt.Errorf("expected initial rename")
	}
	if initialRequest.Scope != nil && initialRequest.Scope.PinnedVector != nil {
		if err := ComparePinnedScope(scope, *initialRequest.Scope.PinnedVector, maxBytes); err != nil {
			return "", err
		}
		vector, err := NormalizeScopeVector(*initialRequest.Scope.PinnedVector, maxBytes)
		if err != nil {
			return "", err
		}
		initialRequest.Scope.PinnedVector = &vector
	}
	sortedPre := append([]RenameFilePrecondition{}, preconditions...)
	files := map[string]string{}
	for _, p := range sortedPre {
		if !uuid(p.WorktreeID) || !lowerHex(p.ProfileID, 64) || !uuid(p.GenerationID) || !validSourcePath(p.Path) || !lowerHex(p.ContentHash, 64) {
			return "", fmt.Errorf("invalid file precondition")
		}
		matches := 0
		for _, m := range scope.Members {
			if m.WorktreeID == p.WorktreeID && m.ProfileID == p.ProfileID && m.GenerationID == p.GenerationID {
				matches++
			}
		}
		if matches == 0 {
			return "", ErrMemberNotInScope
		}
		if matches > 1 {
			return "", ErrAmbiguousMember
		}
		key := renameFileKey(p)
		if hash, ok := files[key]; ok && hash != p.ContentHash {
			return "", fmt.Errorf("conflicting file preconditions")
		}
		files[key] = p.ContentHash
	}
	sort.Slice(sortedPre, func(i, j int) bool {
		a, b := sortedPre[i], sortedPre[j]
		for _, pair := range [][2]string{{a.WorktreeID, b.WorktreeID}, {a.ProfileID, b.ProfileID}, {a.GenerationID, b.GenerationID}, {a.Path, b.Path}, {a.ContentHash, b.ContentHash}} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
	type editValue struct {
		edit    ProposedEdit
		encoded []byte
	}
	sortedEdits := make([]editValue, 0, len(edits))
	remainingEdits := maxBytes
	for _, e := range edits {
		raw, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		v, err := DecodeProposedEditJSON(raw, maxBytes)
		if err != nil {
			return "", err
		}
		p := renamePrecondition(v.SourceRef)
		if hash, ok := files[renameFileKey(p)]; !ok || hash != p.ContentHash {
			return "", fmt.Errorf("edit missing file precondition")
		}
		canonical, err := DecodeIdentityJSON(raw, maxBytes)
		if err != nil {
			return "", err
		}
		writer := canonicalWriter{limit: remainingEdits}
		if err = writer.value(canonical, 0); err != nil {
			return "", err
		}
		remainingEdits -= len(writer.data)
		sortedEdits = append(sortedEdits, editValue{v, writer.data})
	}
	sort.Slice(sortedEdits, func(i, j int) bool {
		a, b := sortedEdits[i].edit, sortedEdits[j].edit
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
		return bytes.Compare(sortedEdits[i].encoded, sortedEdits[j].encoded) < 0
	})
	unique := make([]ProposedEdit, 0, len(sortedEdits))
	lastByFile := map[string]ProposedEdit{}
	for _, entry := range sortedEdits {
		e := entry.edit
		if len(unique) > 0 && unique[len(unique)-1] == e {
			continue
		}
		key := renameFileKey(renamePrecondition(e.SourceRef))
		if prev, ok := lastByFile[key]; ok {
			a, b := prev.SourceRef.Range, e.SourceRef.Range
			if b.StartByte < a.EndByte || b.StartByte == a.StartByte && (a.StartByte == a.EndByte || b.StartByte == b.EndByte) {
				return "", fmt.Errorf("overlapping edits")
			}
		}
		lastByFile[key] = e
		unique = append(unique, e)
	}
	type gapValue struct {
		gap     Gap
		encoded []byte
	}
	ordered := make([]gapValue, 0, len(conflicts))
	remaining := maxBytes
	for _, gap := range conflicts {
		raw, err := json.Marshal(gap)
		if err != nil {
			return "", err
		}
		valid, err := DecodeGapJSON(raw, maxBytes)
		if err != nil {
			return "", err
		}
		v, err := DecodeIdentityJSON(raw, maxBytes)
		if err != nil {
			return "", err
		}
		writer := canonicalWriter{limit: remaining}
		if err = writer.value(v, 0); err != nil {
			return "", err
		}
		remaining -= len(writer.data)
		ordered = append(ordered, gapValue{valid, writer.data})
	}
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i].encoded, ordered[j].encoded) < 0 })
	sortedGaps := make([]Gap, 0, len(ordered))
	for _, g := range ordered {
		sortedGaps = append(sortedGaps, g.gap)
	}
	value := struct {
		Scope             ScopeVector              `json:"scope"`
		InitialRequest    *RenameInitialRequest    `json:"initial_request"`
		ProviderDigest    string                   `json:"provider_digest"`
		FilePreconditions []RenameFilePrecondition `json:"file_preconditions"`
		Edits             []ProposedEdit           `json:"edits"`
		Conflicts         []Gap                    `json:"conflicts"`
	}{scope, initialRequest, providerDigest, sortedPre, unique, sortedGaps}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	v, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("rename-result-v1", v, maxBytes)
	return digest, err
}
