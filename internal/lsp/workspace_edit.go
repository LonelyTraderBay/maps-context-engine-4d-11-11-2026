package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var ErrWorkspaceEdit = errors.New("unsafe or invalid LSP workspace edit")

// CapturedDocument associates an immutable source with its published identity.
// Version is nil only for an unopened document whose LSP version is null.
type CapturedDocument struct {
	File    snapshot.File
	Ref     model.SourceRef
	Version *int64
}

type WorkspaceEditInput struct {
	WorkspaceRoot string
	Documents     []CapturedDocument
	Encoding      snapshot.PositionEncoding
	MaxBytes      int
	MaxEdits      int
}

type ValidatedWorkspaceEdit struct {
	FilePreconditions []model.RenameFilePrecondition
	Edits             []model.ProposedEdit
}

// ValidateWorkspaceEdit converts a complete LSP WorkspaceEdit into captured
// byte ranges. The caller must verify scope authority, workspace closure and
// source freshness before and after the provider call. This function never
// opens files or applies edits, and does not establish provider completeness.
func ValidateWorkspaceEdit(ctx context.Context, raw []byte, in WorkspaceEditInput) (ValidatedWorkspaceEdit, error) {
	if ctx == nil || !filepath.IsAbs(in.WorkspaceRoot) || filepath.Clean(in.WorkspaceRoot) != in.WorkspaceRoot || !utf8.ValidString(in.WorkspaceRoot) || len(in.Documents) == 0 || len(in.Documents) > 10000 || in.MaxBytes < 1 || in.MaxBytes > int(resultHardBytes) || in.MaxEdits < 1 || in.MaxEdits > 10000 {
		return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
	}
	if in.Encoding == "" {
		in.Encoding = snapshot.UTF16
	}
	if in.Encoding != snapshot.UTF8 && in.Encoding != snapshot.UTF16 && in.Encoding != snapshot.UTF32 {
		return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
	}
	if err := model.ValidateWireJSON(raw, in.MaxBytes); err != nil {
		return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
	}
	if err := ctx.Err(); err != nil {
		return ValidatedWorkspaceEdit{}, err
	}
	docs, err := capturedDocuments(ctx, in.WorkspaceRoot, in.Documents, in.MaxBytes)
	if err != nil {
		return ValidatedWorkspaceEdit{}, err
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil || len(object) != 1 {
		return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
	}
	result := ValidatedWorkspaceEdit{FilePreconditions: []model.RenameFilePrecondition{}, Edits: []model.ProposedEdit{}}
	seen := map[string]bool{}
	add := func(uri string, version *int64, versionPresent bool, items []json.RawMessage) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		doc, ok := docs[uri]
		if !ok || seen[uri] || len(items) == 0 || versionPresent && ((version == nil) != (doc.Version == nil) || version != nil && *version != *doc.Version) || !versionPresent && doc.Version != nil {
			return ErrWorkspaceEdit
		}
		seen[uri] = true
		if len(items) > in.MaxEdits-len(result.Edits) {
			return ErrResultTooLarge
		}
		result.FilePreconditions = append(result.FilePreconditions, model.RenameFilePrecondition{WorktreeID: doc.Ref.WorktreeID, ProfileID: doc.Ref.ProfileID, GenerationID: doc.Ref.GenerationID, Path: doc.Ref.Path, ContentHash: doc.Ref.ContentHash})
		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			edit, err := decodeTextEdit(item, doc, in.Encoding, in.MaxBytes)
			if err != nil {
				return err
			}
			result.Edits = append(result.Edits, edit)
		}
		return nil
	}
	if changes, ok := object["changes"]; ok {
		var entries map[string][]json.RawMessage
		if json.Unmarshal(changes, &entries) != nil || entries == nil {
			return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
		}
		for uri, edits := range entries {
			if err := add(uri, nil, false, edits); err != nil {
				return ValidatedWorkspaceEdit{}, err
			}
		}
	} else if changes, ok := object["documentChanges"]; ok {
		var entries []json.RawMessage
		if json.Unmarshal(changes, &entries) != nil || entries == nil || len(entries) > len(docs) {
			return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
		}
		for _, entry := range entries {
			var change map[string]json.RawMessage
			if json.Unmarshal(entry, &change) != nil || !exactFields(change, "textDocument", "edits") {
				return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit // includes create, rename and delete
			}
			var identifier map[string]json.RawMessage
			if json.Unmarshal(change["textDocument"], &identifier) != nil || !exactFields(identifier, "uri", "version") {
				return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
			}
			var uri string
			if json.Unmarshal(identifier["uri"], &uri) != nil || uri == "" {
				return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
			}
			var version *int64
			if !bytes.Equal(bytes.TrimSpace(identifier["version"]), []byte("null")) {
				var n int64
				if json.Unmarshal(identifier["version"], &n) != nil || n < 0 || n > 9007199254740991 {
					return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
				}
				version = &n
			}
			var edits []json.RawMessage
			if json.Unmarshal(change["edits"], &edits) != nil || edits == nil {
				return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
			}
			if err := add(uri, version, true, edits); err != nil {
				return ValidatedWorkspaceEdit{}, err
			}
		}
	} else {
		return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
	}
	if len(result.Edits) == 0 {
		return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
	}
	ordered, err := normalizeRenamePayload(RenameResultPayload{FilePreconditions: result.FilePreconditions, Edits: result.Edits, Conflicts: []model.Gap{}}, in.MaxBytes)
	if err != nil {
		return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
	}
	for i := 1; i < len(ordered.Edits); i++ {
		a, b := ordered.Edits[i-1], ordered.Edits[i]
		if a.SourceRef.WorktreeID == b.SourceRef.WorktreeID && a.SourceRef.ProfileID == b.SourceRef.ProfileID && a.SourceRef.GenerationID == b.SourceRef.GenerationID && a.SourceRef.Path == b.SourceRef.Path && a.SourceRef.ContentHash == b.SourceRef.ContentHash {
			x, y := a.SourceRef.Range, b.SourceRef.Range
			if y.StartByte < x.EndByte || y.StartByte == x.StartByte && (x.StartByte == x.EndByte || y.StartByte == y.EndByte) {
				return ValidatedWorkspaceEdit{}, ErrWorkspaceEdit
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return ValidatedWorkspaceEdit{}, err
	}
	return ValidatedWorkspaceEdit{FilePreconditions: ordered.FilePreconditions, Edits: ordered.Edits}, nil
}

func capturedDocuments(ctx context.Context, root string, documents []CapturedDocument, maxBytes int) (map[string]CapturedDocument, error) {
	if ctx == nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || !utf8.ValidString(root) || len(documents) == 0 || len(documents) > 10000 || maxBytes < 1 || maxBytes > int(resultHardBytes) {
		return nil, ErrWorkspaceEdit
	}
	for _, r := range root {
		if unicode.IsControl(r) {
			return nil, ErrWorkspaceEdit
		}
	}
	docs := make(map[string]CapturedDocument, len(documents))
	for _, doc := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if doc.File.Path() == "" || doc.Ref.Path != doc.File.Path() || doc.Ref.ContentHash != doc.File.ContentHash() || doc.Ref.Range != (model.ByteRange{}) || doc.Version != nil && (*doc.Version < 0 || *doc.Version > 9007199254740991) {
			return nil, ErrWorkspaceEdit
		}
		encoded, err := json.Marshal(doc.Ref)
		if err != nil {
			return nil, ErrWorkspaceEdit
		}
		if _, err := model.DecodeSourceRefJSON(encoded, maxBytes); err != nil {
			return nil, ErrWorkspaceEdit
		}
		if doc.Version != nil {
			version := *doc.Version
			doc.Version = &version
		}
		uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(root, filepath.FromSlash(doc.File.Path())))}).String()
		if _, exists := docs[uri]; exists {
			return nil, ErrWorkspaceEdit
		}
		docs[uri] = doc
	}
	return docs, nil
}

func exactFields(value map[string]json.RawMessage, names ...string) bool {
	if len(value) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := value[name]; !ok {
			return false
		}
	}
	return true
}

func decodeTextEdit(raw json.RawMessage, doc CapturedDocument, encoding snapshot.PositionEncoding, maxBytes int) (model.ProposedEdit, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || !exactFields(object, "range", "newText") {
		return model.ProposedEdit{}, ErrWorkspaceEdit
	}
	var text string
	if bytes.Equal(bytes.TrimSpace(object["newText"]), []byte("null")) || json.Unmarshal(object["newText"], &text) != nil || !utf8.ValidString(text) {
		return model.ProposedEdit{}, ErrWorkspaceEdit
	}
	var bounds map[string]json.RawMessage
	if json.Unmarshal(object["range"], &bounds) != nil || !exactFields(bounds, "start", "end") {
		return model.ProposedEdit{}, ErrWorkspaceEdit
	}
	position := func(value json.RawMessage) (snapshot.Position, error) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(value, &fields) != nil || !exactFields(fields, "line", "character") {
			return snapshot.Position{}, ErrWorkspaceEdit
		}
		var line, character int
		if json.Unmarshal(fields["line"], &line) != nil || json.Unmarshal(fields["character"], &character) != nil || line < 0 || character < 0 || strings.TrimSpace(string(fields["line"])) == "null" || strings.TrimSpace(string(fields["character"])) == "null" {
			return snapshot.Position{}, ErrWorkspaceEdit
		}
		return snapshot.Position{Line: line, Character: character}, nil
	}
	start, err := position(bounds["start"])
	if err != nil {
		return model.ProposedEdit{}, err
	}
	end, err := position(bounds["end"])
	if err != nil {
		return model.ProposedEdit{}, err
	}
	from, err := doc.File.OffsetAt(start, encoding)
	if err != nil {
		return model.ProposedEdit{}, ErrWorkspaceEdit
	}
	to, err := doc.File.OffsetAt(end, encoding)
	if err != nil || to < from {
		return model.ProposedEdit{}, ErrWorkspaceEdit
	}
	ref := doc.Ref
	ref.Range = model.ByteRange{StartByte: int64(from), EndByte: int64(to)}
	edit := model.ProposedEdit{SourceRef: ref, NewText: text}
	encoded, err := json.Marshal(edit)
	if err != nil {
		return model.ProposedEdit{}, ErrWorkspaceEdit
	}
	if _, err = model.DecodeProposedEditJSON(encoded, maxBytes); err != nil {
		return model.ProposedEdit{}, ErrWorkspaceEdit
	}
	return edit, nil
}
