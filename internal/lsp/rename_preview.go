package lsp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// RenameInitialInput uses trusted adapters for current principal, complete
// scope, captured closure and result authority. Neither the result store nor
// cursor codec establishes those authorities by itself.
type RenameInitialInput struct {
	Session         SessionConfig
	Scope           model.ScopeVector
	PrincipalID     string
	PrincipalDigest string
	BindingDigest   string
	ProviderDigest  string
	Request         model.RenameInitialRequest
	Store           *ResultStore
	Codec           *contextpack.CursorCodec
	CursorGuard     contextpack.CursorGuard
	ResultGuard     ResultGuard
	Wire            RenameWireOptions
	// FinishSession releases caller-owned scratch/cache-growth reservations
	// after Preview has stopped the LSP process and before spool publication.
	// Session.Check must remain meaningful afterwards (scope/provider/current
	// source), even when the finished scratch has been removed.
	FinishSession func(context.Context) error
}

// RenameInitial completes one LSP computation and postvalidation before
// installing an immutable spool and delivering its first bounded wire page.
// A failed spool/token/page operation never returns a successful preview.
func RenameInitial(ctx context.Context, in RenameInitialInput) (model.Response, [][]byte, error) {
	if ctx == nil || in.Store == nil || in.Codec == nil || in.CursorGuard == nil || in.ResultGuard == nil || in.Session.Check == nil || in.Session.Limiter == nil || !resultHash(in.PrincipalDigest) || !resultHash(in.BindingDigest) || !resultHash(in.ProviderDigest) || in.PrincipalID == "" || len(in.PrincipalID) > 1024 || len(in.Request.NewName) > 1024 || len(in.Request.Selector.Kind) > 192 || len(in.Request.Selector.Path) > 16384 || !resultHash(in.Request.Selector.ContentHash) || len(in.Session.Documents) > 128 {
		return model.Response{}, nil, ErrResultInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return model.Response{}, nil, ErrResultInvalid
	}
	scopeNormalized, err := model.NormalizeScopeVector(in.Scope, int(resultHardBytes))
	if err != nil {
		return model.Response{}, nil, ErrLSPAuthority
	}
	scopeRaw, err := json.Marshal(scopeNormalized)
	if err != nil {
		return model.Response{}, nil, ErrLSPAuthority
	}
	scopeCopy, err := model.DecodeScopeVectorJSON(scopeRaw, int(resultHardBytes))
	if err != nil {
		return model.Response{}, nil, ErrLSPAuthority
	}
	scope := scopeCopy
	if len(scope.Members) != 1 || len(scope.UnavailableMembers) != 0 {
		return model.Response{}, nil, ErrLSPAuthority
	}
	if in.Request.Scope != nil {
		for _, field := range []*string{in.Request.Scope.WorktreeID, in.Request.Scope.ProfileID, in.Request.Scope.GenerationID, in.Request.Scope.GroupID} {
			if field != nil && len(*field) > 64 {
				return model.Response{}, nil, ErrResultInvalid
			}
		}
		if in.Request.Scope.PinnedVector != nil {
			if _, err := model.NormalizeScopeVector(*in.Request.Scope.PinnedVector, int(resultHardBytes)); err != nil {
				return model.Response{}, nil, ErrLSPAuthority
			}
		}
	}
	requestRaw, err := json.Marshal(in.Request)
	if err != nil {
		return model.Response{}, nil, ErrResultInvalid
	}
	decoded, err := model.DecodeRenameRequestJSON(requestRaw, int(resultHardBytes))
	if err != nil {
		return model.Response{}, nil, ErrResultInvalid
	}
	request, ok := decoded.(*model.RenameInitialRequest)
	if !ok {
		return model.Response{}, nil, ErrResultInvalid
	}
	if request.Scope != nil && request.Scope.PinnedVector != nil {
		if err := model.ComparePinnedScope(scope, *request.Scope.PinnedVector, int(resultHardBytes)); err != nil {
			return model.Response{}, nil, ErrLSPAuthority
		}
		normalized, err := model.NormalizeScopeVector(*request.Scope.PinnedVector, int(resultHardBytes))
		if err != nil {
			return model.Response{}, nil, ErrLSPAuthority
		}
		request.Scope.PinnedVector = &normalized
	}
	// Page budget is a continuation control, not part of the query identity.
	fingerprint := *request
	fingerprint.MaxOutputBytes = nil
	requestDigest, err := model.RequestDigest("rename_preview", in.PrincipalID, "RenameRequest", &fingerprint, int(resultHardBytes), runtime.GoWorkerLimit())
	if err != nil {
		return model.Response{}, nil, ErrResultInvalid
	}
	documents, err := capturedDocuments(ctx, in.Session.Environment.WorkspaceRoot, in.Session.Documents, int(resultHardBytes))
	if err != nil {
		return model.Response{}, nil, err
	}
	in.Session.Documents = make([]CapturedDocument, 0, len(documents))
	member := scope.Members[0]
	for _, doc := range documents {
		if doc.Ref.WorktreeID != member.WorktreeID || doc.Ref.ProfileID != member.ProfileID || doc.Ref.GenerationID != member.GenerationID {
			return model.Response{}, nil, ErrLSPAuthority
		}
		in.Session.Documents = append(in.Session.Documents, doc)
	}
	session, err := StartSession(ctx, in.Session)
	if err != nil {
		return model.Response{}, nil, err
	}
	validated, err := session.Preview(ctx, request.Selector, request.NewName)
	if err != nil {
		return model.Response{}, nil, err
	}
	conflicts, err := InspectRenameContracts(ctx, in.Session.Documents, validated.Edits, request.PreserveContracts)
	if err != nil {
		return model.Response{}, nil, err
	}
	if err := in.Session.Check(ctx); err != nil {
		return model.Response{}, nil, err
	}
	if in.FinishSession != nil {
		if err := in.FinishSession(ctx); err != nil {
			return model.Response{}, nil, err
		}
	}
	payload := RenameResultPayload{InitialRequest: *request, ProviderDigest: in.ProviderDigest, FilePreconditions: validated.FilePreconditions, Edits: validated.Edits, Conflicts: conflicts}
	header, err := in.Store.Create(ctx, scope, in.PrincipalDigest, payload, in.ResultGuard)
	if err != nil {
		return model.Response{}, nil, err
	}
	binding := contextpack.CursorBinding{Purpose: "rename-result", PrincipalDigest: in.PrincipalDigest, BindingDigest: in.BindingDigest, RequestDigest: requestDigest, OrderVersion: "rename-v1", Scope: scope, ResultID: header.ResultID, ResultDigest: header.ResultDigest}
	resultToken, err := in.Store.IssueResultToken(ctx, in.Codec, binding, in.CursorGuard, in.ResultGuard)
	if err != nil {
		return model.Response{}, nil, err
	}
	if request.MaxOutputBytes != nil {
		in.Wire.MaxOutputBytes = *request.MaxOutputBytes
	}
	response, blocks, err := in.Store.ReadResultWirePage(ctx, in.Codec, resultToken, "", in.PrincipalDigest, in.CursorGuard, in.ResultGuard, in.Wire)
	if err != nil {
		return model.Response{}, nil, err
	}
	if in.Session.Check(ctx) != nil {
		if ctx.Err() != nil {
			return model.Response{}, nil, ctx.Err()
		}
		return model.Response{}, nil, ErrLSPAuthority
	}
	if _, err := in.Store.ValidateResultToken(ctx, in.Codec, resultToken, in.PrincipalDigest, in.CursorGuard, in.ResultGuard); err != nil {
		return model.Response{}, nil, err
	}
	return response, blocks, nil
}

// RenameContinue only reads the existing signed spool. It does not start a
// language server or accept another selector or new name.
func RenameContinue(ctx context.Context, store *ResultStore, codec *contextpack.CursorCodec, request model.RenameContinueRequest, principal string, cursorGuard contextpack.CursorGuard, resultGuard ResultGuard, options RenameWireOptions) (model.Response, [][]byte, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return model.Response{}, nil, ErrResultInvalid
	}
	decoded, err := model.DecodeRenameRequestJSON(raw, int(resultHardBytes))
	if err != nil {
		return model.Response{}, nil, ErrResultInvalid
	}
	validated, ok := decoded.(*model.RenameContinueRequest)
	if !ok {
		return model.Response{}, nil, ErrResultInvalid
	}
	if validated.MaxOutputBytes != nil {
		options.MaxOutputBytes = *validated.MaxOutputBytes
	}
	return store.ReadResultWirePage(ctx, codec, validated.ResultToken, validated.Cursor, principal, cursorGuard, resultGuard, options)
}

// ValidateResultToken rechecks the original complete result and current guard
// without extending its TTL. The host must separately retain and verify all
// delivered pages before considering a change; validation never applies one.
func (s *ResultStore) ValidateResultToken(ctx context.Context, codec *contextpack.CursorCodec, token, principal string, cursorGuard contextpack.CursorGuard, resultGuard ResultGuard) (RenameResultHeader, error) {
	if s == nil || ctx == nil || codec == nil || cursorGuard == nil || resultGuard == nil {
		return RenameResultHeader{}, ErrResultInvalid
	}
	claims, err := codec.VerifyPrincipal(ctx, token, "rename-result", principal, cursorGuard)
	if err != nil {
		return RenameResultHeader{}, err
	}
	stored, err := s.Read(ctx, claims.Binding.ResultID, principal, claims.Binding.Scope, resultGuard)
	if err != nil || stored.Header.ResultDigest != claims.Binding.ResultDigest {
		return RenameResultHeader{}, errors.Join(ErrResultInvalid, err)
	}
	if _, err := codec.VerifyPrincipal(ctx, token, "rename-result", principal, cursorGuard); err != nil {
		return RenameResultHeader{}, err
	}
	rechecked, err := s.Read(ctx, claims.Binding.ResultID, principal, claims.Binding.Scope, resultGuard)
	if err != nil {
		return RenameResultHeader{}, err
	}
	if rechecked.Header.ResultDigest != stored.Header.ResultDigest || rechecked.Header.PayloadHash != stored.Header.PayloadHash || rechecked.Header.IssuedAt != stored.Header.IssuedAt || rechecked.Header.ExpiresAt != stored.Header.ExpiresAt {
		return RenameResultHeader{}, ErrResultStoreUnsafe
	}
	if _, err := codec.VerifyPrincipal(ctx, token, "rename-result", principal, cursorGuard); err != nil {
		return RenameResultHeader{}, err
	}
	return stored.Header, nil
}
