package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type ResponseData interface{ responseData() }

func (StatusData) responseData()                 {}
func (LocateData) responseData()                 {}
func (PackData) responseData()                   {}
func (ValidateData) responseData()               {}
func (RenameData) responseData()                 {}
func (GraphData) responseData()                  {}
func (SourceData) responseData()                 {}
func (MappingData) responseData()                {}
func (FlowData) responseData()                   {}
func (BootstrapData) responseData()              {}
func (PreviewData) responseData()                {}
func (RecordData) responseData()                 {}
func (PreviewStatusData) responseData()          {}
func (PreviewPermissionAdminData) responseData() {}
func (GroupAdminData) responseData()             {}
func (RuntimeConfigData) responseData()          {}
func (RecordListData) responseData()             {}
func (RecordReadData) responseData()             {}
func (ProfileAdminData) responseData()           {}
func (PolicyAdminData) responseData()            {}

type Response struct {
	SchemaVersion int64        `json:"schema_version"`
	RequestID     string       `json:"request_id"`
	Status        string       `json:"status"`
	Scope         *ScopeVector `json:"scope"`
	Freshness     Freshness    `json:"freshness"`
	Coverage      Coverage     `json:"coverage"`
	Readiness     string       `json:"readiness"`
	Data          ResponseData `json:"data"`
	Pagination    Pagination   `json:"pagination"`
	Budget        Budget       `json:"budget"`
	Warnings      []Gap        `json:"warnings"`
	Error         *ErrorInfo   `json:"error"`
}

func responseValue[T ResponseData](value T, err error) (ResponseData, error) {
	if err != nil {
		return nil, err
	}
	return value, nil
}

func DecodeResponseDataJSON(data []byte, maxBytes, cpus int) (ResponseData, error) {
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return nil, err
	}
	var f map[string]json.RawMessage
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	var op string
	if err := json.Unmarshal(f["operation"], &op); err != nil {
		return nil, fmt.Errorf("missing data operation")
	}
	switch op {
	case "status":
		return responseValue(DecodeStatusDataJSON(data, maxBytes))
	case "locate":
		return responseValue(DecodeLocateDataJSON(data, maxBytes))
	case "context_pack", "expand_pack":
		return responseValue(DecodePackDataJSON(data, maxBytes))
	case "validate_pack":
		return responseValue(DecodeValidateDataJSON(data, maxBytes))
	case "rename_preview":
		return responseValue(DecodeRenameDataJSON(data, maxBytes))
	case "graph_query":
		return responseValue(DecodeGraphDataJSON(data, maxBytes))
	case "source_read":
		return responseValue(DecodeSourceDataJSON(data, maxBytes))
	case "mapping_resolve":
		return responseValue(DecodeMappingDataJSON(data, maxBytes))
	case "flow_query":
		return responseValue(DecodeFlowDataJSON(data, maxBytes))
	case "bootstrap":
		return responseValue(DecodeBootstrapDataJSON(data, maxBytes))
	case "preview_attach", "preview_bind", "preview_mode", "preview_ack", "preview_invalidate", "preview_detach":
		return responseValue(DecodePreviewDataJSON(data, maxBytes))
	case "mapping_propose", "mapping_review":
		return responseValue(DecodeRecordDataJSON(data, maxBytes))
	case "preview_status":
		return responseValue(DecodePreviewStatusDataJSON(data, maxBytes))
	case "preview_permission_admin":
		return responseValue(DecodePreviewPermissionAdminDataJSON(data, maxBytes))
	case "group_admin":
		return responseValue(DecodeGroupAdminDataJSON(data, maxBytes))
	case "runtime_config":
		return responseValue(DecodeRuntimeConfigDataJSON(data, maxBytes, cpus))
	case "records_list":
		return responseValue(DecodeRecordListDataJSON(data, maxBytes))
	case "records_read":
		return responseValue(DecodeRecordReadDataJSON(data, maxBytes))
	case "profile_admin":
		return responseValue(DecodeProfileAdminDataJSON(data, maxBytes))
	case "policy_admin":
		return responseValue(DecodePolicyAdminDataJSON(data, maxBytes))
	default:
		return nil, fmt.Errorf("unknown response operation")
	}
}

// allowFailureData is supplied from the specific operation contract, never
// from the response itself. This checks wire invariants, not evidence authority,
// authorization, byte accounting, freshness or continuation correctness.
func DecodeResponseJSON(data []byte, maxBytes, cpus int, expectedOperation string, allowFailureData bool) (Response, error) {
	var r Response
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"schema_version", "request_id", "status", "scope", "freshness", "coverage", "readiness", "data", "pagination", "budget", "warnings", "error"}, map[string]bool{"scope": true, "data": true, "error": true})
	if err != nil {
		return r, err
	}
	for _, entry := range []struct {
		key  string
		dest any
	}{{"schema_version", &r.SchemaVersion}, {"request_id", &r.RequestID}, {"status", &r.Status}, {"readiness", &r.Readiness}} {
		if err = json.Unmarshal(f[entry.key], entry.dest); err != nil {
			return Response{}, err
		}
	}
	if r.SchemaVersion != 1 || !uuid(r.RequestID) || (r.Status != "ok" && r.Status != "partial" && r.Status != "blocked" && r.Status != "error") || (r.Readiness != "reviewable" && r.Readiness != "needs_context" && r.Readiness != "blocked") {
		return Response{}, fmt.Errorf("invalid response envelope")
	}
	if !bytes.Equal(bytes.TrimSpace(f["scope"]), []byte("null")) {
		v, e := DecodeScopeVectorJSON(f["scope"], maxBytes)
		if e != nil {
			return Response{}, e
		}
		r.Scope = &v
	}
	r.Freshness, err = DecodeFreshnessJSON(f["freshness"], maxBytes)
	if err != nil {
		return Response{}, err
	}
	r.Coverage, err = DecodeCoverageJSON(f["coverage"], maxBytes)
	if err != nil {
		return Response{}, err
	}
	r.Pagination, err = DecodePaginationJSON(f["pagination"], maxBytes)
	if err != nil {
		return Response{}, err
	}
	r.Budget, err = DecodeBudgetJSON(f["budget"], maxBytes)
	if err != nil {
		return Response{}, err
	}
	r.Warnings, err = decodeGaps(f["warnings"], 64, maxBytes)
	if err != nil {
		return Response{}, err
	}
	if !bytes.Equal(bytes.TrimSpace(f["error"]), []byte("null")) {
		v, e := DecodeErrorInfoJSON(f["error"], maxBytes)
		if e != nil {
			return Response{}, e
		}
		r.Error = &v
	}
	if !bytes.Equal(bytes.TrimSpace(f["data"]), []byte("null")) {
		var header struct {
			Operation string `json:"operation"`
		}
		if err = json.Unmarshal(f["data"], &header); err != nil {
			return Response{}, err
		}
		if expectedOperation == "" || header.Operation != expectedOperation {
			return Response{}, fmt.Errorf("response operation mismatch")
		}
		r.Data, err = DecodeResponseDataJSON(f["data"], maxBytes, cpus)
		if err != nil {
			return Response{}, err
		}
	}
	if expectedOperation == "status" && r.Freshness.Scope == "returned_files" {
		return Response{}, fmt.Errorf("status has no returned files to verify")
	}
	if expectedOperation == "bootstrap" && (r.Scope != nil || r.Freshness.Status != "not_checked" || r.Coverage.Status != "unknown" || r.Readiness != "blocked") {
		return Response{}, fmt.Errorf("invalid bootstrap context")
	}
	if (r.Status == "error" || r.Status == "blocked") && (r.Error == nil || r.Readiness != "blocked" || r.Data != nil && !allowFailureData) || r.Status == "ok" && r.Error != nil {
		return Response{}, fmt.Errorf("inconsistent response status")
	}
	return r, nil
}
