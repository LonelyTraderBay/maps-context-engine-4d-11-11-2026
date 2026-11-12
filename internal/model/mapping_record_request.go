package model

import (
	"encoding/json"
	"fmt"
)

type MappingProposeRequest struct {
	Scope            *ScopeRequest `json:"scope,omitempty"`
	SourceRef        SourceRef     `json:"source_ref"`
	DestinationRef   GraphRef      `json:"destination_ref"`
	Rationale        string        `json:"rationale"`
	ClientMutationID string        `json:"client_mutation_id"`
}
type MappingReviewRequest struct {
	Scope            *ScopeRequest `json:"scope,omitempty"`
	RecordID         string        `json:"record_id"`
	ExpectedVersion  int64         `json:"expected_version"`
	Action           string        `json:"action"`
	Reason           string        `json:"reason"`
	ClientMutationID string        `json:"client_mutation_id"`
}

// Parsing a proposal or review is not authority to persist or approve it.
func DecodeMappingProposeRequestJSON(data []byte, maxBytes int) (MappingProposeRequest, error) {
	var r MappingProposeRequest
	f, err := optionalObject(data, maxBytes, "scope", "source_ref", "destination_ref", "rationale", "client_mutation_id")
	if err != nil {
		return r, err
	}
	for _, k := range []string{"source_ref", "destination_ref", "rationale", "client_mutation_id"} {
		if _, ok := f[k]; !ok {
			return r, fmt.Errorf("missing proposal field")
		}
	}
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return MappingProposeRequest{}, err
	}
	r.SourceRef, err = DecodeSourceRefJSON(f["source_ref"], maxBytes)
	if err != nil {
		return MappingProposeRequest{}, err
	}
	r.DestinationRef, err = DecodeGraphRefJSON(f["destination_ref"], maxBytes)
	if err != nil {
		return MappingProposeRequest{}, err
	}
	if err = json.Unmarshal(f["rationale"], &r.Rationale); err != nil {
		return MappingProposeRequest{}, err
	}
	if err = json.Unmarshal(f["client_mutation_id"], &r.ClientMutationID); err != nil {
		return MappingProposeRequest{}, err
	}
	if !boundedString(r.Rationale, 2048) || !uuid(r.ClientMutationID) {
		return MappingProposeRequest{}, fmt.Errorf("invalid proposal")
	}
	return r, nil
}
func DecodeMappingReviewRequestJSON(data []byte, maxBytes int) (MappingReviewRequest, error) {
	var r MappingReviewRequest
	f, err := optionalObject(data, maxBytes, "scope", "record_id", "expected_version", "action", "reason", "client_mutation_id")
	if err != nil {
		return r, err
	}
	for _, k := range []string{"record_id", "expected_version", "action", "reason", "client_mutation_id"} {
		if _, ok := f[k]; !ok {
			return r, fmt.Errorf("missing review field")
		}
	}
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return MappingReviewRequest{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return MappingReviewRequest{}, err
	}
	var body MappingReviewRequest
	if err = json.Unmarshal(raw, &body); err != nil {
		return MappingReviewRequest{}, err
	}
	body.Scope = r.Scope
	if !uuid(body.RecordID) || body.ExpectedVersion < 1 || body.ExpectedVersion > maxExactInteger || !uuid(body.ClientMutationID) || !boundedString(body.Reason, 2048) {
		return MappingReviewRequest{}, fmt.Errorf("invalid review")
	}
	switch body.Action {
	case "approve", "revoke", "supersede":
	default:
		return MappingReviewRequest{}, fmt.Errorf("invalid review action")
	}
	return body, nil
}
