package explorer

import (
	"context"
	"errors"
	"net/http"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const (
	previewAttach     = "POST /preview/attach"
	previewBind       = "POST /preview/bind"
	previewStatus     = "POST /preview/status"
	previewMode       = "POST /preview/mode"
	previewAck        = "POST /preview/ack"
	previewInvalidate = "POST /preview/invalidate"
	previewDetach     = "POST /preview/detach"
	previewMapping    = "POST /mapping/resolve"
)

func (s *Server) servePreview(ctx context.Context, w http.ResponseWriter, op, token string, request model.Request) {
	actor, valid := tokenHash(token)
	if !valid {
		s.reject(w, http.StatusUnauthorized, "AUTH_INVALID")
		return
	}
	manager := s.options.Preview
	var reply previewReply
	var err error
	switch r := request.(type) {
	case model.PreviewAttachRequest:
		s.auth.mu.Lock()
		cutoff := s.auth.bearerTime.end
		s.auth.mu.Unlock()
		reply, err = manager.attach(ctx, actor, cutoff, r, s.now)
	case model.PreviewBindRequest:
		reply, err = manager.bind(ctx, actor, r, s.now)
	case model.PreviewStatusRequest:
		reply, err = manager.status(ctx, actor, r.SessionID, s.now)
	case model.PreviewModeRequest:
		reply, err = manager.mode(ctx, actor, r, s.now)
	case model.PreviewAckRequest:
		reply, err = manager.ack(ctx, actor, r, s.now)
	case model.PreviewInvalidateRequest:
		reply, err = manager.invalidate(ctx, actor, r.SessionID, r.ExpectedRevision, false, s.now())
	case model.PreviewDetachRequest:
		reply, err = manager.invalidate(ctx, actor, r.SessionID, 0, true, s.now())
	case model.MappingResolveRequest:
		reply, err = manager.resolveMapping(ctx, actor, r, s.now)
	default:
		s.reject(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrPreviewDenied), errors.Is(err, catalog.ErrPermissionUnavailable):
			s.reject(w, http.StatusForbidden, "PREVIEW_NOT_APPROVED")
		case errors.Is(err, ErrPreviewExpired):
			s.reject(w, http.StatusForbidden, "PERMISSION_EXPIRED")
		case errors.Is(err, ErrPreviewChanged):
			s.reject(w, http.StatusConflict, "BRIDGE_NOT_READY")
		case errors.Is(err, ErrModeAckTimeout):
			s.reject(w, http.StatusConflict, "MODE_ACK_TIMEOUT")
		case errors.Is(err, ErrPreviewMapMissing):
			s.reject(w, http.StatusConflict, "BUILD_MAP_MISSING")
		case errors.Is(err, ErrPreviewCapacity):
			s.reject(w, http.StatusTooManyRequests, "RESOURCE_LIMIT")
		default:
			s.readError(w, op, ctx, err)
		}
		return
	}
	if s.auth.authenticate(token, s.now()) != "" {
		s.reject(w, http.StatusUnauthorized, "AUTH_EXPIRED")
		return
	}
	response := previewResponse(reply)
	body, err := encode(response)
	if err != nil {
		if op == previewMapping {
			s.reject(w, http.StatusRequestEntityTooLarge, "RESULT_TOO_LARGE")
			return
		}
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if _, err := s.registry.DecodeResponse(contracts.HTTP, op, body, int(response.Budget.MaxOutputBytes), 1); err != nil {
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	status := http.StatusOK
	if reply.problem != nil {
		status = http.StatusRequestEntityTooLarge
	}
	if op == previewStatus && reply.scrollAllowed {
		w.Header().Set("X-MCE4D-Preview-Scroll", "1")
	}
	sendJSON(w, status, body)
}

func previewResponse(reply previewReply) model.Response {
	response := emptyResponse()
	response.Data, response.Scope = reply.data, reply.scope
	if data, ok := reply.data.(model.MappingData); ok {
		response.Pagination.ReturnedCount = int64(len(data.Matches))
	}
	if reply.pagination != nil {
		response.Pagination = *reply.pagination
		if reply.pagination.Truncated {
			response.Status = "partial"
		}
	}
	if reply.problem != nil {
		response.Status, response.Error = "error", reply.problem
	}
	response.Budget.MaxOutputBytes = 32768
	if reply.limit > 0 {
		response.Budget.MaxOutputBytes = reply.limit
	}
	return response
}
