package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// ValidateRenameWorkspace is for a complete stored result whose purpose-bound
// token, principal, immutable payload and original expiry were independently
// authenticated by the retention owner. It does not authenticate a token, prove
// typed package closure, track page delivery or grant permission to apply.
// renameProviderDigest is independently obtained from the approved rename
// provider; it need not equal the index/capture provider fingerprint.
// It rechecks every edit/precondition/contract boundary against the pinned
// capture, then uses the same full-workspace verifier as pack validation.
func ValidateRenameWorkspace(ctx context.Context, result lsp.RenameStoredResult, renameProviderDigest string, input ValidateCapturedInput, maxResultBytes int) (CapturedValidation, error) {
	if input.VerifyWorkspace == nil {
		return CapturedValidation{}, ErrValidationInput
	}
	sources, err := ValidateRenameCaptured(ctx, result, renameProviderDigest, input, maxResultBytes)
	if err != nil {
		return CapturedValidation{}, err
	}
	input.Sources = sources
	return ValidateCapturedWorkspace(ctx, input)
}

// ValidateRenameCaptured checks the complete immutable result against the
// authorized pinned capture. It grants no live-workspace freshness, token
// authenticity, typed closure or apply authority. Retention guards use it on
// every read; the validate operation additionally checks the whole workspace.
func ValidateRenameCaptured(ctx context.Context, result lsp.RenameStoredResult, renameProviderDigest string, input ValidateCapturedInput, maxResultBytes int) ([]ValidationSource, error) {
	if ctx == nil || input.CurrentScope == nil || input.ResolveCapture == nil || input.AuthorizeSource == nil || maxResultBytes < 1024 || maxResultBytes > 32<<20 || input.MaxInputBytes < 1024 || input.MaxInputBytes > 1<<20 || input.MaxManifestBytes < 1024 || input.MaxManifestBytes > 64<<20 || !coreHash(renameProviderDigest) {
		return nil, ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrValidationInput
	}
	if err := validationContextError(ctx); err != nil {
		return nil, err
	}
	scope := input.Scope
	if scope.Kind != "single" || len(scope.Members) != 1 || len(scope.UnavailableMembers) != 0 || model.ComparePinnedScope(scope, result.Header.Scope, input.MaxInputBytes) != nil {
		return nil, ErrValidationScopeChanged
	}
	member := scope.Members[0]
	payload := result.Payload
	if result.Header.WorktreeID != member.WorktreeID || payload.ProviderDigest != renameProviderDigest || len(payload.Edits) > 10000 || len(payload.FilePreconditions) > 128 || len(payload.Conflicts) > 10000 || !requestMatchesScope(payload.InitialRequest.Scope, scope, member, input.MaxInputBytes) {
		return nil, ErrValidationPrecondition
	}
	// Store decoding bounds individual fields; bound their aggregate before
	// encoding another defensive copy here. This method never reads a new head.
	used := len(payload.InitialRequest.Selector.Path) + len(payload.InitialRequest.NewName)
	for _, pre := range payload.FilePreconditions {
		used += len(pre.Path) + len(pre.ContentHash) + 256
		if len(pre.Path) > 16384 || used > maxResultBytes {
			return nil, ErrCoreLimit
		}
	}
	for _, edit := range payload.Edits {
		used += len(edit.NewText) + len(edit.SourceRef.Path) + 512
		if len(edit.NewText) > 524288 || len(edit.SourceRef.Path) > 16384 || used > maxResultBytes {
			return nil, ErrCoreLimit
		}
	}
	for _, gap := range payload.Conflicts {
		used += len(gap.Code) + len(gap.Reason) + 128
		if len(gap.SourceRefs) > 16 || len(gap.Reason) > 4096 || len(gap.Code) > 768 {
			return nil, ErrCoreLimit
		}
		for _, ref := range gap.SourceRefs {
			used += len(ref.Path) + 512
		}
		if used > maxResultBytes {
			return nil, ErrCoreLimit
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > maxResultBytes {
		return nil, ErrCoreLimit
	}
	hash := sha256.Sum256(raw)
	digest, err := model.RenameResultDigest(scope, payload.InitialRequest, renameProviderDigest, payload.FilePreconditions, payload.Edits, payload.Conflicts, maxResultBytes)
	if err != nil || digest != result.Header.ResultDigest || hex.EncodeToString(hash[:]) != result.Header.PayloadHash {
		return nil, ErrValidationPrecondition
	}
	current, err := input.CurrentScope(ctx)
	if err != nil {
		return nil, coreContextError(ctx, ErrValidationAuthority)
	}
	if model.ComparePinnedScope(scope, current, input.MaxInputBytes) != nil {
		return nil, ErrValidationScopeChanged
	}
	captured, err := input.ResolveCapture(ctx, copyValidationMember(member))
	if err != nil || captured == nil {
		return nil, coreContextError(ctx, ErrValidationAuthority)
	}
	manifest := captured.Manifest()
	if model.VerifyCaptureDigest(manifest, input.MaxManifestBytes) != nil || manifest.WorktreeID != member.WorktreeID || manifest.ProfileID != member.ProfileID || manifest.PolicyDigest != member.PolicyDigest || manifest.SnapshotDigest != member.SnapshotDigest {
		return nil, ErrValidationPrecondition
	}
	files := make(map[string]snapshot.File)
	refs := make(map[string]model.SourceRef)
	addFile := func(name, hash string) (snapshot.File, error) {
		file, ok := captured.File(name)
		if !ok || file.ContentHash() != hash {
			return snapshot.File{}, ErrValidationPrecondition
		}
		files[name] = file
		refs[name] = model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: name, ContentHash: hash, Range: model.ByteRange{StartByte: 0, EndByte: int64(file.Len())}}
		return file, nil
	}
	selected := payload.InitialRequest.Selector
	file, err := addFile(selected.Path, selected.ContentHash)
	if err != nil || selected.ByteOffset < 0 || selected.ByteOffset > int64(file.Len()) || file.CheckRange(int(selected.ByteOffset), int(selected.ByteOffset)) != nil {
		return nil, ErrValidationPrecondition
	}
	preconditions := make(map[string]string, len(payload.FilePreconditions))
	for _, pre := range payload.FilePreconditions {
		if pre.WorktreeID != member.WorktreeID || pre.ProfileID != member.ProfileID || pre.GenerationID != member.GenerationID || preconditions[pre.Path] != "" {
			return nil, ErrValidationPrecondition
		}
		if _, err := addFile(pre.Path, pre.ContentHash); err != nil {
			return nil, err
		}
		preconditions[pre.Path] = pre.ContentHash
	}
	checkRef := func(ref model.SourceRef) error {
		if err := validationContextError(ctx); err != nil {
			return err
		}
		if _, err := model.MatchPinnedSource(scope, ref, nil, input.MaxInputBytes); err != nil {
			return ErrValidationPrecondition
		}
		file, err := addFile(ref.Path, ref.ContentHash)
		if err != nil || file.CheckRange(int(ref.Range.StartByte), int(ref.Range.EndByte)) != nil {
			return ErrValidationPrecondition
		}
		if err := input.AuthorizeSource(ctx, copyValidationMember(member), ref); err != nil {
			return coreContextError(ctx, ErrValidationAuthority)
		}
		return nil
	}
	for _, edit := range payload.Edits {
		if preconditions[edit.SourceRef.Path] != edit.SourceRef.ContentHash {
			return nil, ErrValidationPrecondition
		}
		if err := checkRef(edit.SourceRef); err != nil {
			return nil, err
		}
	}
	for _, gap := range payload.Conflicts {
		for _, ref := range gap.SourceRefs {
			if err := checkRef(ref); err != nil {
				return nil, err
			}
		}
	}
	// Independently preserve the syntax-backed contract boundaries, including
	// implicit JSON field names. A valid digest alone cannot prove they exist.
	if len(payload.Edits) > 0 {
		documents := make([]lsp.CapturedDocument, 0, len(preconditions))
		for name := range preconditions {
			documents = append(documents, lsp.CapturedDocument{File: files[name], Ref: refs[name]})
		}
		required, err := lsp.InspectRenameContracts(ctx, documents, payload.Edits, payload.InitialRequest.PreserveContracts)
		if err != nil {
			return nil, coreContextError(ctx, ErrValidationPrecondition)
		}
		present := make(map[string]bool, len(payload.Conflicts))
		for _, gap := range payload.Conflicts {
			raw, _ := json.Marshal(gap)
			present[string(raw)] = true
		}
		for _, gap := range required {
			raw, _ := json.Marshal(gap)
			if !present[string(raw)] {
				return nil, ErrValidationPrecondition
			}
		}
	}
	// Coalesce edits into whole-file validation obligations; 1025 edits do not
	// overflow the 256-source request cap. Full inventory verification still
	// covers unedited files, configuration, rules and dependency inputs.
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	input.Sources = make([]ValidationSource, 0, len(names))
	for _, name := range names {
		input.Sources = append(input.Sources, ValidationSource{Ref: refs[name]})
	}
	return input.Sources, nil
}
