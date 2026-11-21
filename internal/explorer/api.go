package explorer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const (
	bootstrap = "POST /session/bootstrap"
	workspace = "GET /workspace"
	graph     = "POST /graph/query"
	flow      = "POST /flow/query"
	source    = "POST /source/read"
)

func discardLog() *log.Logger { return log.New(io.Discard, "", 0) }

func apiRoutePath(name string) bool { return name == APIPath || strings.HasPrefix(name, APIPath+"/") }

func assetRoute(name string) (string, string) {
	if name == "index.html" {
		return "/", "text/html; charset=utf-8"
	}
	if path.Dir(name) != "assets" {
		return "", ""
	}
	switch path.Ext(name) {
	case ".js":
		return "/" + name, "text/javascript; charset=utf-8"
	case ".css":
		return "/" + name, "text/css; charset=utf-8"
	default:
		return "", ""
	}
}

func (s *Server) sameOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	sites := r.Header.Values("Sec-Fetch-Site")
	if len(sites) > 1 || len(sites) == 1 && sites[0] != "same-origin" {
		return false
	}
	if len(origins) == 1 {
		return origins[0] == s.origin
	}
	// Browser GET fetches from the same origin may omit Origin.
	return len(origins) == 0 && r.Method == http.MethodGet && len(sites) == 1
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.reject(w, http.StatusForbidden, "ACCESS_DENIED")
		return
	}
	if r.Method == http.MethodPost {
		intent := r.Header.Values(CSRFHeader)
		if len(intent) != 1 || intent[0] != CSRFValue {
			s.reject(w, http.StatusForbidden, "ACCESS_DENIED")
			return
		}
	}
	select {
	case s.busy <- struct{}{}:
		defer func() { <-s.busy }()
	default:
		s.reject(w, http.StatusTooManyRequests, "RATE_LIMITED")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.options.QueryTimeout)
	defer cancel()
	op := r.Method + " " + strings.TrimPrefix(r.URL.Path, APIPath)
	var token string
	if op == bootstrap {
		if !s.auth.countAttempt(s.now()) {
			s.reject(w, http.StatusTooManyRequests, "RATE_LIMITED")
			return
		}
	} else {
		var authError string
		token, authError = s.authenticate(r)
		if authError != "" {
			s.reject(w, http.StatusUnauthorized, authError)
			return
		}
	}
	switch op {
	case bootstrap, workspace, graph, source:
	case flow:
		if !s.options.StaticFlow {
			s.reject(w, http.StatusNotFound, "UNSUPPORTED_CAPABILITY")
			return
		}
	case "POST /context/pack":
		if !s.options.SelectionContext {
			s.reject(w, http.StatusNotFound, "UNSUPPORTED_CAPABILITY")
			return
		}
	case previewAttach, previewBind, previewStatus, previewMode, previewAck, previewInvalidate, previewDetach, previewMapping:
		if s.options.Preview == nil {
			s.reject(w, http.StatusNotFound, "UNSUPPORTED_CAPABILITY")
			return
		}
	case recordList, recordRead, mappingPropose, mappingReview:
		if s.options.Records == nil {
			s.reject(w, http.StatusNotFound, "UNSUPPORTED_CAPABILITY")
			return
		}
	default:
		s.reject(w, http.StatusNotFound, "UNSUPPORTED_CAPABILITY")
		return
	}
	entry, err := s.registry.Lookup(contracts.HTTP, op)
	if err != nil {
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	raw, err := requestBytes(w, r, op, entry.RequestHardLimit())
	if err != nil {
		s.reject(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	request, err := s.registry.DecodeRequest(contracts.HTTP, op, raw, entry.RequestHardLimit(), 1)
	if err != nil {
		s.reject(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	if op == bootstrap {
		value, ok := request.(model.BootstrapRequest)
		if !ok {
			s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
			return
		}
		s.exchange(ctx, w, value)
		return
	}
	if strings.HasPrefix(op, "POST /preview/") || op == previewMapping {
		s.servePreview(ctx, w, op, token, request)
		return
	}
	switch op {
	case recordList, recordRead, mappingPropose, mappingReview:
		s.serveRecords(ctx, w, op, token, request)
		return
	}
	id, err := requestID()
	if err != nil {
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	limit := int64(131072)
	if op == graph || op == flow {
		limit = 1048576
	}
	result, err := s.options.Reader.Read(ctx, request, app.CoreCallOptions{RequestID: id, MaxOutputBytes: limit, DeliveryMode: "http"})
	if err != nil {
		s.readError(w, op, ctx, err)
		return
	}
	if result.Recheck == nil || len(result.Body) == 0 {
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	response, err := s.registry.DecodeResponse(contracts.HTTP, op, result.Body, int(limit), 1)
	if err != nil || response.RequestID != id ||
		response.Budget.DeliveryMode != "http" || model.ValidateDeliveredBytes(response.Budget, [][]byte{result.Body}) != nil {
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	status := http.StatusOK
	if response.Error != nil || response.Data == nil {
		switch {
		case response.Error != nil && response.Data == nil && op == "POST /context/pack":
			status = errorHTTPStatus(response.Error.Code)
		case response.Error != nil && response.Data == nil && op == source && response.Error.Code == "RESULT_TOO_LARGE":
			status = http.StatusRequestEntityTooLarge
		default:
			s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
			return
		}
	}
	if err := result.Recheck(ctx); err != nil {
		s.readError(w, op, ctx, err)
		return
	}
	if err := ctx.Err(); err != nil {
		s.readError(w, op, ctx, err)
		return
	}
	if denied := s.auth.authenticate(token, s.now()); denied != "" {
		s.reject(w, http.StatusUnauthorized, denied)
		return
	}
	sendJSON(w, status, result.Body)
}

func (s *Server) authenticate(r *http.Request) (string, string) {
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		return "", "AUTH_REQUIRED"
	}
	if len(values) != 1 || len(values[0]) != len("Bearer ")+tokenLength || !strings.HasPrefix(values[0], "Bearer ") {
		return "", "AUTH_INVALID"
	}
	token := values[0][len("Bearer "):]
	return token, s.auth.authenticate(token, s.now())
}

func requestBytes(w http.ResponseWriter, r *http.Request, op string, limit int) ([]byte, error) {
	if op == workspace {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || len(r.URL.RawQuery) > 32 || r.URL.ForceQuery {
			return nil, app.ErrCoreInput
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return nil, app.ErrCoreInput
		}
		if len(query) == 0 {
			return []byte("{}"), nil
		}
		verify := query["verify"]
		if len(query) != 1 || len(verify) != 1 || verify[0] != "none" && verify[0] != "workspace" {
			return nil, app.ErrCoreInput
		}
		return json.Marshal(model.StatusRequest{Verify: &verify[0]})
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength > int64(limit) {
		return nil, app.ErrCoreInput
	}
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 || len(contentTypes[0]) > 128 || len(r.Header.Values("Content-Encoding")) != 0 {
		return nil, app.ErrCoreInput
	}
	mime, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mime != "application/json" || len(params) > 1 ||
		len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		return nil, app.ErrCoreInput
	}
	reader := http.MaxBytesReader(w, r.Body, int64(limit))
	defer reader.Close()
	return io.ReadAll(reader)
}

func (s *Server) exchange(ctx context.Context, w http.ResponseWriter, request model.BootstrapRequest) {
	if reason := s.auth.validateCode(request.Code, s.now()); reason != "" {
		s.reject(w, http.StatusUnauthorized, reason)
		return
	}
	if err := s.options.Reader.Check(ctx); err != nil {
		s.readError(w, bootstrap, ctx, err)
		return
	}
	if err := ctx.Err(); err != nil {
		s.readError(w, bootstrap, ctx, err)
		return
	}
	bearer, end, code := s.auth.exchange(request.Code, s.now())
	if code != "" {
		status := http.StatusUnauthorized
		if code == "INTERNAL_ERROR" {
			status = http.StatusInternalServerError
		}
		s.reject(w, status, code)
		return
	}
	response := emptyResponse()
	capabilities := []string{"workspace_status", "graph_query", "source_read"}
	if s.options.Preview != nil {
		capabilities = append(capabilities, "preview_attach", "preview_status")
	}
	if s.options.Records != nil {
		capabilities = append(capabilities, "records_list", "mapping_propose")
	}
	if s.options.SelectionContext {
		capabilities = append(capabilities, "context_pack")
	}
	if s.options.StaticFlow {
		capabilities = append(capabilities, "flow_query")
	}
	response.Data = model.BootstrapData{Operation: "bootstrap", Bearer: bearer,
		ExpiresAt: end.Format(time.RFC3339Nano), Capabilities: capabilities}
	body, err := encode(response)
	if err != nil {
		s.auth.revoke()
		s.reject(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	sendJSON(w, http.StatusOK, body)
}

func emptyResponse() model.Response {
	zero := int64(0)
	id, _ := requestID()
	return model.Response{SchemaVersion: 1, RequestID: id, Status: "ok", Readiness: "blocked",
		Freshness:  model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"},
		Coverage:   model.Coverage{Status: "unknown", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "No source context was returned."},
		Pagination: model.Pagination{EnumerationComplete: true, RequiredPending: &zero, OptionalPending: &zero},
		Budget:     model.Budget{MaxOutputBytes: 4096, DeliveryMode: "http"}, Warnings: []model.Gap{}}
}

func requestID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

func encode(response model.Response) ([]byte, error) {
	_, blocks, err := model.SettleSerializedBudget(response.Budget, func(size int64) ([][]byte, error) {
		response.Budget.SerializedBytes = size
		raw, err := json.Marshal(response)
		return [][]byte{raw}, err
	})
	if err != nil {
		return nil, err
	}
	return blocks[0], nil
}

func sendJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) reject(w http.ResponseWriter, status int, code string) {
	message, action := "The Explorer request was rejected.", "none"
	switch code {
	case "AUTH_REQUIRED", "AUTH_INVALID", "AUTH_EXPIRED":
		message, action = "A current Explorer session is required.", "reauthenticate"
	case "RATE_LIMITED":
		message, action = "The Explorer request limit was reached.", "retry_bounded"
	case "INVALID_ARGUMENT":
		message, action = "The Explorer request is invalid.", "fix_request"
	}
	s.errorResponse(w, status, model.ErrorInfo{Code: code, Message: message, SuggestedAction: action,
		Retryable: code == "RATE_LIMITED", Details: []model.Gap{}})
}

func (s *Server) readError(w http.ResponseWriter, op string, ctx context.Context, err error) {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	info := app.CoreErrorInfo(op, err)
	s.errorResponse(w, errorHTTPStatus(info.Code), info)
}

func errorHTTPStatus(code string) int {
	switch code {
	case "ACCESS_DENIED", "SECRET_BLOCKED":
		return http.StatusForbidden
	case "AUTH_REQUIRED":
		return http.StatusUnauthorized
	case "INVALID_ARGUMENT", "BUDGET_TOO_SMALL", "CANCELLED":
		return http.StatusBadRequest
	case "CURSOR_INVALID", "STORE_REPLACED", "SOURCE_PRECONDITION_FAILED", "FRESHNESS_UNKNOWN", "AMBIGUOUS_SYMBOL", "CONTEXT_INCOMPLETE":
		return http.StatusConflict
	case "INDEX_MISSING", "UNSUPPORTED_CAPABILITY":
		return http.StatusNotFound
	case "SNAPSHOT_EXPIRED", "CURSOR_EXPIRED", "RESULT_EXPIRED":
		return http.StatusGone
	case "RESULT_TOO_LARGE":
		return http.StatusRequestEntityTooLarge
	case "RESOURCE_LIMIT":
		return http.StatusTooManyRequests
	case "TIMEOUT":
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) errorResponse(w http.ResponseWriter, status int, info model.ErrorInfo) {
	response := emptyResponse()
	response.Status, response.Error = "error", &info
	body, err := encode(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	sendJSON(w, status, body)
}
