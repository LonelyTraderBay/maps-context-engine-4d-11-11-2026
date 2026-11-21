package explorer

import (
	"context"
	"errors"
	"net/http"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const (
	recordList     = "POST /records/list"
	recordRead     = "POST /records/read"
	mappingPropose = "POST /mapping/propose"
	mappingReview  = "POST /mapping/review"
)

func (s *Server) serveRecords(ctx context.Context, w http.ResponseWriter, op, token string, request model.Request) {
	response := emptyResponse()
	response.Budget.MaxOutputBytes = s.options.RecordOutputCap
	service := s.options.Records
	guard := func(ctx context.Context) error {
		if s.auth.authenticate(token, s.now()) != "" {
			return app.ErrCoreAuthentication
		}
		return ctx.Err()
	}
	var err error
	switch r := request.(type) {
	case model.RecordListRequest:
		var result app.RecordListResult
		result, err = service.List(ctx, r)
		response.Data, response.Scope = result.Data, result.Scope
		response.Pagination.ReturnedCount = int64(len(result.Data.Records))
		if result.Cursor != nil {
			reason := "limit"
			response.Status = "partial"
			response.Pagination.Truncated = true
			response.Pagination.EnumerationComplete = false
			response.Pagination.RequiredPending = nil
			response.Pagination.NextCursor = result.Cursor
			response.Pagination.LimitReason = &reason
		}
	case model.RecordReadRequest:
		var result app.RecordReadResult
		result, err = service.Read(ctx, r)
		response.Data, response.Scope = result.Data, result.Scope
		response.Pagination.ReturnedCount = 1
	case model.MappingProposeRequest:
		response.Data, err = s.writeMapping(ctx, r.Scope, r, guard)
	case model.MappingReviewRequest:
		response.Data, err = s.writeMapping(ctx, r.Scope, r, guard)
	default:
		err = app.ErrCoreInput
	}
	if err != nil {
		switch {
		case errors.Is(err, model.ErrRecordConflict), errors.Is(err, app.ErrRecordsChanged):
			s.errorResponse(w, http.StatusConflict, model.ErrorInfo{Code: "RECORD_CONFLICT", Message: "Reload the record and its current scope.",
				SuggestedAction: "retry_bounded", Details: []model.Gap{}})
		case errors.Is(err, app.ErrRecordsMissing):
			s.reject(w, http.StatusNotFound, "RECORD_NOT_FOUND")
		default:
			s.readError(w, op, ctx, err)
		}
		return
	}
	if err := guard(ctx); err != nil {
		s.readError(w, op, ctx, err)
		return
	}
	raw, err := encode(response)
	if err != nil {
		s.reject(w, http.StatusRequestEntityTooLarge, "RESULT_TOO_LARGE")
		return
	}
	if _, err := s.registry.DecodeResponse(contracts.HTTP, op, raw, int(response.Budget.MaxOutputBytes), 1); err != nil {
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	sendJSON(w, http.StatusOK, raw)
}

func (s *Server) writeMapping(ctx context.Context, scope *model.ScopeRequest, input model.Request,
	guard func(context.Context) error) (model.ResponseData, error) {
	if scope == nil || scope.PinnedVector == nil {
		return nil, app.ErrCoreInput
	}
	return s.options.Records.ApplyMapping(ctx, input, guard)
}
