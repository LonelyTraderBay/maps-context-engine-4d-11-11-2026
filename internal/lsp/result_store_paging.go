package lsp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type RenameResultPage struct {
	Data                model.RenameData
	RemainingEdits      int64
	RemainingConflicts  int64
	NextCursor          *string
	SerializedItemBytes int
}

// IssueResultToken reads and rechecks the installed spool before issuing a
// purpose-specific token. Guards must establish current caller authority and
// source/epoch validity; this method does not invoke or repeat LSP computation.
func (s *ResultStore) IssueResultToken(ctx context.Context, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, cursorGuard contextpack.CursorGuard, resultGuardFn ResultGuard) (string, error) {
	if s == nil || codec == nil || binding.Purpose != "rename-result" || !resultUUID(binding.ResultID) || !resultHash(binding.ResultDigest) || !resultHash(binding.PrincipalDigest) || cursorGuard == nil || resultGuardFn == nil {
		return "", ErrResultInvalid
	}
	result, err := s.Read(ctx, binding.ResultID, binding.PrincipalDigest, binding.Scope, resultGuardFn)
	if err != nil {
		return "", err
	}
	if result.Header.ResultDigest != binding.ResultDigest {
		return "", ErrResultInvalid
	}
	token, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, cursorGuard)
	if err != nil {
		return "", err
	}
	result, err = s.Read(ctx, binding.ResultID, binding.PrincipalDigest, binding.Scope, resultGuardFn)
	if err != nil || result.Header.ResultDigest != binding.ResultDigest {
		return "", errors.Join(ErrResultInvalid, err)
	}
	if _, err = codec.Verify(ctx, token, binding, cursorGuard); err != nil {
		return "", err
	}
	return token, nil
}

// ReadResultPage resumes across processes from the immutable spool. Item bytes
// include the edit/conflict arrays only; the adapter must measure the complete
// wire envelope, cursor and repeated MCP blocks against its own budget.
func (s *ResultStore) ReadResultPage(ctx context.Context, codec *contextpack.CursorCodec, resultToken, cursor, principal string, cursorGuard contextpack.CursorGuard, resultGuardFn ResultGuard, maxEdits, maxItemBytes int) (RenameResultPage, error) {
	if s == nil || codec == nil || !resultHash(principal) || cursorGuard == nil || resultGuardFn == nil || maxEdits < 1 || maxEdits > 512 || maxItemBytes < 4 || maxItemBytes > 131072 {
		return RenameResultPage{}, ErrResultInvalid
	}
	resultClaims, err := codec.VerifyPrincipal(ctx, resultToken, "rename-result", principal, cursorGuard)
	if err != nil {
		return RenameResultPage{}, err
	}
	binding := resultClaims.Binding
	if resultClaims.Position.NextItem != 0 || resultClaims.Position.NextCoverageGap != 0 || resultClaims.Position.NextWarningGap != 0 || resultClaims.Position.NextByteOffset != 0 || len(resultClaims.Position.LastKey) != 0 {
		return RenameResultPage{}, ErrResultInvalid
	}
	result, err := s.Read(ctx, binding.ResultID, principal, binding.Scope, resultGuardFn)
	if err != nil {
		return RenameResultPage{}, err
	}
	if result.Header.ResultDigest != binding.ResultDigest {
		return RenameResultPage{}, ErrResultInvalid
	}
	pageBinding := binding
	pageBinding.Purpose = "rename-page"
	previous := contextpack.SignedPosition{LastKey: []string{}}
	var pageClaims contextpack.SignedCursor
	if cursor != "" {
		pageClaims, err = codec.Verify(ctx, cursor, pageBinding, cursorGuard)
		if err != nil {
			return RenameResultPage{}, err
		}
		previous = pageClaims.Position
	}
	if len(previous.LastKey) != 0 || previous.NextWarningGap != 0 || previous.NextByteOffset != 0 || previous.NextItem > int64(len(result.Payload.Edits)) || previous.NextCoverageGap > int64(len(result.Payload.Conflicts)) || previous.NextItem < int64(len(result.Payload.Edits)) && previous.NextCoverageGap != 0 {
		return RenameResultPage{}, ErrResultInvalid
	}
	edits := make([]model.ProposedEdit, 0, maxEdits)
	conflicts := make([]model.Gap, 0, 128)
	used := 4 // two empty JSON arrays
	i, j := int(previous.NextItem), int(previous.NextCoverageGap)
	for i < len(result.Payload.Edits) && len(edits) < maxEdits {
		if err := ctx.Err(); err != nil {
			return RenameResultPage{}, err
		}
		raw, err := json.Marshal(result.Payload.Edits[i])
		if err != nil {
			return RenameResultPage{}, ErrResultInvalid
		}
		need := len(raw)
		if len(edits) > 0 {
			need++
		}
		if need > maxItemBytes-used {
			if len(edits) == 0 {
				return RenameResultPage{}, ErrResultTooLarge
			}
			break
		}
		edits = append(edits, result.Payload.Edits[i])
		used += need
		i++
	}
	if i == len(result.Payload.Edits) {
		for j < len(result.Payload.Conflicts) && len(conflicts) < 128 {
			if err := ctx.Err(); err != nil {
				return RenameResultPage{}, err
			}
			raw, err := json.Marshal(result.Payload.Conflicts[j])
			if err != nil {
				return RenameResultPage{}, ErrResultInvalid
			}
			need := len(raw)
			if len(conflicts) > 0 {
				need++
			}
			if need > maxItemBytes-used {
				if len(edits) == 0 && len(conflicts) == 0 {
					return RenameResultPage{}, ErrResultTooLarge
				}
				break
			}
			conflicts = append(conflicts, result.Payload.Conflicts[j])
			used += need
			j++
		}
	}
	remainingEdits := len(result.Payload.Edits) - i
	remainingConflicts := len(result.Payload.Conflicts) - j
	complete := remainingEdits == 0 && remainingConflicts == 0
	var next *string
	if !complete {
		newPos := contextpack.SignedPosition{NextItem: int64(i), NextCoverageGap: int64(j), LastKey: []string{}}
		if !contextpack.CursorAdvances(previous, newPos) {
			return RenameResultPage{}, ErrResultInvalid
		}
		value, err := codec.Issue(ctx, pageBinding, newPos, cursorGuard)
		if err != nil {
			return RenameResultPage{}, err
		}
		next = &value
	}
	data := model.RenameData{Operation: "rename_preview", Applied: false, SourceMutationCount: 0, ValidationRequired: true, Edits: edits, Conflicts: conflicts, PreviewComplete: complete, ResultToken: resultToken, ResultID: binding.ResultID, ResultDigest: binding.ResultDigest, TotalEdits: int64(len(result.Payload.Edits)), ComputationComplete: true}
	raw, err := json.Marshal(data)
	if err != nil {
		return RenameResultPage{}, ErrResultInvalid
	}
	if _, err = model.DecodeRenameDataJSON(raw, 1024*1024); err != nil {
		return RenameResultPage{}, ErrResultInvalid
	}
	if err = codec.Recheck(ctx, resultClaims, cursorGuard); err != nil {
		return RenameResultPage{}, err
	}
	if cursor != "" {
		if err = codec.Recheck(ctx, pageClaims, cursorGuard); err != nil {
			return RenameResultPage{}, err
		}
	}
	if next != nil {
		if _, err = codec.Verify(ctx, *next, pageBinding, cursorGuard); err != nil {
			return RenameResultPage{}, err
		}
	}
	if err = s.validateResult(result, principal, binding.Scope); err != nil {
		return RenameResultPage{}, err
	}
	if err = resultGuard(ctx, resultGuardFn, result); err != nil {
		return RenameResultPage{}, err
	}
	if err = s.validateResult(result, principal, binding.Scope); err != nil {
		return RenameResultPage{}, err
	}
	if err = codec.Recheck(ctx, resultClaims, cursorGuard); err != nil {
		return RenameResultPage{}, err
	}
	if cursor != "" {
		if err = codec.Recheck(ctx, pageClaims, cursorGuard); err != nil {
			return RenameResultPage{}, err
		}
	}
	if next != nil {
		if _, err = codec.Verify(ctx, *next, pageBinding, cursorGuard); err != nil {
			return RenameResultPage{}, err
		}
	}
	if err = s.validateResult(result, principal, binding.Scope); err != nil {
		return RenameResultPage{}, err
	}
	return RenameResultPage{Data: data, RemainingEdits: int64(remainingEdits), RemainingConflicts: int64(remainingConflicts), NextCursor: next, SerializedItemBytes: used}, nil
}
