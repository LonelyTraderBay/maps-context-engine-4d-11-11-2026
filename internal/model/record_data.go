package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Record summaries are assertions, not proof of authorization or current provenance.
type RecordSummary struct {
	RecordID         string      `json:"record_id"`
	Version          int64       `json:"version"`
	RecordKind       string      `json:"record_kind"`
	DeclaredStatus   string      `json:"declared_status"`
	EffectiveStatus  string      `json:"effective_status"`
	ProvenanceStatus string      `json:"provenance_status"`
	SourceRefs       []SourceRef `json:"source_refs"`
}
type UIMappingPayload struct {
	SourceRef      SourceRef `json:"source_ref"`
	DestinationRef GraphRef  `json:"destination_ref"`
	Rationale      string    `json:"rationale"`
}
type RecordListData struct {
	Operation string          `json:"operation"`
	Records   []RecordSummary `json:"records"`
}
type RecordReadData struct {
	Operation   string            `json:"operation"`
	Record      RecordSummary     `json:"record"`
	UIMapping   *UIMappingPayload `json:"ui_mapping"`
	Limitations []Gap             `json:"limitations"`
}

func DecodeRecordSummaryJSON(data []byte, maxBytes int) (RecordSummary, error) {
	var r RecordSummary
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"record_id", "version", "record_kind", "declared_status", "effective_status", "provenance_status", "source_refs"}, nil)
	if err != nil {
		return r, err
	}
	if _, err = decodeSourceRefs(f["source_refs"], 128, maxBytes); err != nil {
		return r, err
	}
	if err = json.Unmarshal(data, &r); err != nil {
		return RecordSummary{}, err
	}
	if !uuid(r.RecordID) || r.Version < 1 || r.Version > maxExactInteger || !validID(r.RecordKind) || r.SourceRefs == nil {
		return RecordSummary{}, fmt.Errorf("invalid record summary")
	}
	switch r.DeclaredStatus {
	case "proposed", "approved", "revoked", "superseded":
	default:
		return RecordSummary{}, fmt.Errorf("invalid declared status")
	}
	switch r.EffectiveStatus {
	case "proposed", "approved", "revoked", "superseded", "needs_review":
	default:
		return RecordSummary{}, fmt.Errorf("invalid effective status")
	}
	switch r.ProvenanceStatus {
	case "retained_snapshot", "snapshot_expired", "source_changed", "access_denied", "unverified":
	default:
		return RecordSummary{}, fmt.Errorf("invalid provenance status")
	}
	if r.EffectiveStatus == "approved" && (r.DeclaredStatus != "approved" || r.ProvenanceStatus == "snapshot_expired") {
		return RecordSummary{}, fmt.Errorf("record approval inconsistent with provenance")
	}
	return r, nil
}
func DecodeUIMappingPayloadJSON(data []byte, maxBytes int) (UIMappingPayload, error) {
	var r UIMappingPayload
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"source_ref", "destination_ref", "rationale"}, nil)
	if err != nil {
		return r, err
	}
	r.SourceRef, err = DecodeSourceRefJSON(f["source_ref"], maxBytes)
	if err != nil {
		return UIMappingPayload{}, err
	}
	r.DestinationRef, err = DecodeGraphRefJSON(f["destination_ref"], maxBytes)
	if err != nil {
		return UIMappingPayload{}, err
	}
	if err = json.Unmarshal(f["rationale"], &r.Rationale); err != nil {
		return UIMappingPayload{}, err
	}
	if !stringAtMost(r.Rationale, 2048) || r.Rationale == "" {
		return UIMappingPayload{}, fmt.Errorf("invalid rationale")
	}
	return r, nil
}
func DecodeRecordListDataJSON(data []byte, maxBytes int) (RecordListData, error) {
	var r RecordListData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "records"}, nil)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(f["operation"], &r.Operation); err != nil || r.Operation != "records_list" {
		return RecordListData{}, fmt.Errorf("invalid record list operation")
	}
	var items []json.RawMessage
	if err = json.Unmarshal(f["records"], &items); err != nil {
		return r, err
	}
	if items == nil || len(items) > 100 {
		return r, fmt.Errorf("invalid records")
	}
	r.Records = make([]RecordSummary, 0, len(items))
	for _, item := range items {
		v, e := DecodeRecordSummaryJSON(item, maxBytes)
		if e != nil {
			return RecordListData{}, e
		}
		r.Records = append(r.Records, v)
	}
	return r, nil
}
func DecodeRecordReadDataJSON(data []byte, maxBytes int) (RecordReadData, error) {
	var r RecordReadData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "record", "ui_mapping", "limitations"}, map[string]bool{"ui_mapping": true})
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(f["operation"], &r.Operation); err != nil || r.Operation != "records_read" {
		return RecordReadData{}, fmt.Errorf("invalid record read operation")
	}
	r.Record, err = DecodeRecordSummaryJSON(f["record"], maxBytes)
	if err != nil {
		return RecordReadData{}, err
	}
	r.Limitations, err = decodeGaps(f["limitations"], 32, maxBytes)
	if err != nil {
		return RecordReadData{}, err
	}
	if !bytes.Equal(bytes.TrimSpace(f["ui_mapping"]), []byte("null")) {
		v, e := DecodeUIMappingPayloadJSON(f["ui_mapping"], maxBytes)
		if e != nil {
			return RecordReadData{}, e
		}
		if r.Record.RecordKind != "ui_mapping" {
			return RecordReadData{}, fmt.Errorf("mapping payload has different record kind")
		}
		r.UIMapping = &v
	}
	return r, nil
}
