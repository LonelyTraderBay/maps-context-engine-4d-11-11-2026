package explorer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func (s *PreviewSessions) resolveMapping(ctx context.Context, actor [32]byte, r model.MappingResolveRequest, now func() time.Time) (previewReply, error) {
	row, err := s.preview(ctx, actor, r.SessionID, now())
	if err != nil {
		return previewReply{}, err
	}
	if r.BuildMapID != row.build || r.DocumentEpoch != 1 || r.HMREpoch != 0 ||
		row.state != "bound" || row.currentMode(now()) != "inspect" || row.binding.ID == "" {
		return previewReply{}, ErrPreviewChanged
	}
	if r.Scope != nil {
		scope, err := s.authority.Resolve(ctx, r.Scope)
		if err != nil {
			return previewReply{}, err
		}
		if model.ComparePinnedScope(row.scope, scope, 32768) != nil {
			return previewReply{}, ErrPreviewDenied
		}
	}
	if err := s.recheck(ctx, row, now()); err != nil {
		return previewReply{}, err
	}
	binding, err := s.authority.Binding(ctx, row.scope, row.build)
	if err != nil {
		return previewReply{}, err
	}
	if binding != row.binding {
		return previewReply{}, ErrPreviewMapMissing
	}
	limit := int64(24576)
	if r.MaxOutputBytes != nil {
		limit = *r.MaxOutputBytes
	}
	limit = min(limit, s.outputCap, 131072)
	data := model.MappingData{Operation: "mapping_resolve", RequestedRef: r.Ref, Matches: []model.MappingMatch{},
		BuildMapID: row.build, BindingID: row.binding.ID, ResolverDigest: row.binding.ResolverDigest,
		MappingStatus: "unmapped", WhyMatched: "No link in the pinned build binding."}
	switch ref := r.Ref.(type) {
	case *model.DOMAnchorRef:
		if r.Cursor != nil || ref.BuildMapID != row.build {
			return previewReply{}, contextpack.ErrCursorInvalid
		}
		match, err := s.authority.ResolveAnchor(ctx, row.scope, binding, *ref)
		if err != nil {
			return previewReply{}, err
		}
		if match != nil {
			data.Matches = append(data.Matches, *match)
			data.MappingStatus = match.MappingStatus
			data.WhyMatched = "The build anchor matches source bytes in the pinned generation."
		}
	case *model.FileRef, *model.SymbolRef:
		return s.reverseMapping(ctx, actor, row, r, data, limit, now)
	default:
		return previewReply{}, app.ErrCoreUnsupported
	}
	if err := s.checkMappingSession(ctx, actor, row, now); err != nil {
		return previewReply{}, err
	}
	return previewReply{scope: &row.scope, data: data, limit: limit}, nil
}

func (s *PreviewSessions) checkMappingSession(ctx context.Context, actor [32]byte, row previewSession, now func() time.Time) error {
	if err := s.recheck(ctx, row, now()); err != nil {
		return err
	}
	current, err := s.preview(ctx, actor, row.id, now())
	if err != nil {
		return err
	}
	if current.revision != row.revision || current.expired || current.currentMode(now()) != "inspect" {
		return ErrPreviewChanged
	}
	return ctx.Err()
}

func (s *PreviewSessions) reverseMapping(ctx context.Context, actor [32]byte, row previewSession, r model.MappingResolveRequest,
	data model.MappingData, limit int64, now func() time.Time) (previewReply, error) {
	if s.cursors == nil {
		return previewReply{}, app.ErrCoreUnsupported
	}
	if r.Scope == nil || r.Scope.PinnedVector == nil || r.Limit < 1 || r.Limit > 100 {
		return previewReply{}, app.ErrCoreInput
	}
	target, ok := r.Ref.(model.GraphRef)
	if !ok {
		return previewReply{}, app.ErrCoreInput
	}
	if file, ok := target.(*model.FileRef); ok && file.ContentHash == nil {
		return previewReply{}, app.ErrValidationPrecondition
	}
	if _, err := model.MatchPinnedMember(row.scope, target, nil); err != nil {
		return previewReply{}, contextpack.ErrCursorInvalid
	}
	targetDigest, err := model.GraphRefDigest(target, 8192)
	if err != nil {
		return previewReply{}, app.ErrCoreInput
	}
	principal := hex.EncodeToString(actor[:])
	normalized := r
	normalized.Scope, normalized.Cursor, normalized.MaxOutputBytes, normalized.Limit = nil, nil, nil, 20
	requestDigest, err := model.RequestDigest("mapping_resolve", principal, "MappingResolveRequest", normalized, 32768, 1)
	if err != nil {
		return previewReply{}, app.ErrCoreInput
	}
	identity, err := json.Marshal([]any{row.id, row.build, row.binding.ID, row.binding.ResolverDigest,
		row.revision, row.epoch, row.permission.Version, row.permission.Digest, row.scope.VectorDigest})
	if err != nil {
		return previewReply{}, err
	}
	value, err := model.DecodeIdentityJSON(identity, 8192)
	if err != nil {
		return previewReply{}, err
	}
	_, bindingDigest, err := model.EncodeCanonical("preview-mapping-session-v1", value, 8192)
	if err != nil {
		return previewReply{}, err
	}
	binding := contextpack.CursorBinding{Purpose: "mapping", PrincipalDigest: principal, BindingDigest: bindingDigest,
		RequestDigest: requestDigest, OrderVersion: "ui-mapping-v1", Scope: row.scope}
	guard := func(ctx context.Context, _ contextpack.CursorBinding) error {
		return s.checkMappingSession(ctx, actor, row, now)
	}
	position := contextpack.SignedPosition{LastKey: []string{}}
	after := ""
	var claims contextpack.SignedCursor
	if r.Cursor != nil {
		claims, err = s.cursors.Verify(ctx, *r.Cursor, binding, guard)
		if err != nil {
			return previewReply{}, err
		}
		position = claims.Position
		if position.NextItem < 1 || len(position.LastKey) != 3 || !model.ValidDigest(position.LastKey[0]) ||
			position.LastKey[1] != targetDigest || position.LastKey[2] != position.LastKey[0] ||
			position.NextCoverageGap != 0 || position.NextWarningGap != 0 || position.NextByteOffset != 0 {
			return previewReply{}, contextpack.ErrCursorInvalid
		}
		after = position.LastKey[0]
	}
	matches, total, err := s.authority.ResolveTarget(ctx, row.scope, row.binding, row.build, target, after, int(r.Limit))
	if err != nil {
		return previewReply{}, err
	}
	if len(matches) > int(r.Limit) || int64(len(matches)) != min(r.Limit, total-position.NextItem) ||
		position.NextItem > 0 && len(matches) == 0 {
		return previewReply{}, contextpack.ErrCursorInvalid
	}
	if total != 0 {
		data.WhyMatched = "The pinned build map links these source sites to the requested target."
		data.MappingStatus = "exact_in_supported_scope"
		if total > 1 {
			data.MappingStatus = "candidate"
		}
	}
	reply, err := s.fitMappingPage(ctx, row.scope, data, matches, total, limit, position, binding, targetDigest, guard)
	if err != nil {
		return previewReply{}, err
	}
	if r.Cursor != nil {
		err = s.cursors.Recheck(ctx, claims, guard)
	} else {
		err = guard(ctx, binding)
	}
	if err != nil {
		return previewReply{}, err
	}
	return reply, nil
}

func (s *PreviewSessions) fitMappingPage(ctx context.Context, scope model.ScopeVector, data model.MappingData, matches []model.MappingMatch,
	total, limit int64, previous contextpack.SignedPosition, binding contextpack.CursorBinding, targetDigest string,
	guard contextpack.CursorGuard) (previewReply, error) {
	zero := int64(0)
	for {
		count := int64(len(matches))
		remaining := total - previous.NextItem - count
		page := model.Pagination{ReturnedCount: count, EnumerationComplete: remaining == 0, Truncated: remaining > 0,
			RequiredPending: &remaining, OptionalPending: &zero}
		if remaining > 0 {
			last := matches[len(matches)-1]
			key := []string{last.AnchorRef.AnchorID, targetDigest, last.MatchID}
			position := contextpack.SignedPosition{NextItem: previous.NextItem + count, LastKey: key}
			if !contextpack.CursorAdvances(previous, position) {
				return previewReply{}, contextpack.ErrCursorInvalid
			}
			cursor, err := s.cursors.Issue(ctx, binding, position, guard)
			if err != nil {
				return previewReply{}, err
			}
			reason := "limit"
			page.NextCursor, page.LimitReason = &cursor, &reason
		}
		data.Matches = append([]model.MappingMatch{}, matches...)
		reply := previewReply{scope: &scope, data: data, limit: limit, pagination: &page}
		if _, err := encode(previewResponse(reply)); err == nil {
			return reply, nil
		}
		if len(matches) <= 1 {
			problem := app.CoreErrorInfo(previewMapping, contextpack.ErrItemTooLarge)
			reply = previewReply{scope: &scope, limit: limit, problem: &problem}
			if len(matches) == 1 {
				problem.Details = []model.Gap{{Code: "MAPPING_MATCH_TOO_LARGE", Reason: "Request a larger budget for this mapping match.",
					SourceRefs: matches[0].SourceRefs}}
			}
			if _, err := encode(previewResponse(reply)); err != nil {
				return previewReply{}, contextpack.ErrItemTooLarge
			}
			return reply, nil
		}
		matches = matches[:len(matches)/2]
	}
}
