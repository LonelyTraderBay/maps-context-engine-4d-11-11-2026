package model

import (
	"encoding/json"
	"fmt"
)

type RecordListRequest struct {
	Scope      *ScopeRequest `json:"scope,omitempty"`
	RecordKind *string       `json:"record_kind,omitempty"`
	Status     *string       `json:"status,omitempty"`
	Limit      *int64        `json:"limit,omitempty"`
	Cursor     *string       `json:"cursor,omitempty"`
}
type RecordReadRequest struct {
	Scope    *ScopeRequest `json:"scope,omitempty"`
	RecordID string        `json:"record_id"`
	Version  *int64        `json:"version,omitempty"`
}

// Record request validation does not authorize records, resolve current heads,
// verify cursors or claim support for a record kind.
func DecodeRecordListRequestJSON(data []byte, maxBytes int) (RecordListRequest, error) {
	var r RecordListRequest
	f, err := optionalObject(data, maxBytes, "scope", "record_kind", "status", "limit", "cursor")
	if err != nil {
		return r, err
	}
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return RecordListRequest{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return RecordListRequest{}, err
	}
	var body RecordListRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return RecordListRequest{}, err
	}
	body.Scope = r.Scope
	if !optionalID(body.RecordKind) || !optionalRange(body.Limit, 1, 100) || body.Cursor != nil && !validToken(*body.Cursor) {
		return RecordListRequest{}, fmt.Errorf("invalid record list")
	}
	if body.Status != nil {
		switch *body.Status {
		case "proposed", "approved", "revoked", "superseded", "needs_review":
		default:
			return RecordListRequest{}, fmt.Errorf("invalid record status")
		}
	}
	if body.Limit == nil {
		limit := int64(20)
		body.Limit = &limit
	}
	return body, nil
}
func DecodeRecordReadRequestJSON(data []byte, maxBytes int) (RecordReadRequest, error) {
	var r RecordReadRequest
	f, err := optionalObject(data, maxBytes, "scope", "record_id", "version")
	if err != nil {
		return r, err
	}
	if _, ok := f["record_id"]; !ok {
		return r, fmt.Errorf("missing record id")
	}
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return RecordReadRequest{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return RecordReadRequest{}, err
	}
	var body RecordReadRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return RecordReadRequest{}, err
	}
	body.Scope = r.Scope
	if !uuid(body.RecordID) || !optionalRange(body.Version, 1, maxExactInteger) {
		return RecordReadRequest{}, fmt.Errorf("invalid record read")
	}
	return body, nil
}
