package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type MappingRef interface{ mappingRef() }

func (*SymbolRef) mappingRef()    {}
func (*EntityRef) mappingRef()    {}
func (*ModuleRef) mappingRef()    {}
func (*AnchorRef) mappingRef()    {}
func (*FileRef) mappingRef()      {}
func (*DOMAnchorRef) mappingRef() {}

type MappingResolveRequest struct {
	Scope          *ScopeRequest `json:"scope,omitempty"`
	SessionID      string        `json:"session_id"`
	BuildMapID     string        `json:"build_map_id"`
	DocumentEpoch  int64         `json:"document_epoch"`
	HMREpoch       int64         `json:"hmr_epoch"`
	Ref            MappingRef    `json:"ref"`
	Cursor         *string       `json:"cursor,omitempty"`
	Limit          int64         `json:"limit"`
	MaxOutputBytes *int64        `json:"max_output_bytes,omitempty"`
}
type MappingMatch struct {
	MatchID       string        `json:"match_id"`
	AnchorRef     AnchorRef     `json:"anchor_ref"`
	TargetRef     GraphRef      `json:"target_ref"`
	SourceRefs    []SourceRef   `json:"source_refs"`
	MappingStatus string        `json:"mapping_status"`
	EvidenceRefs  []EvidenceRef `json:"evidence_refs"`
}
type MappingData struct {
	Operation      string         `json:"operation"`
	RequestedRef   MappingRef     `json:"requested_ref"`
	Matches        []MappingMatch `json:"matches"`
	BuildMapID     string         `json:"build_map_id"`
	BindingID      string         `json:"binding_id"`
	ResolverDigest string         `json:"resolver_digest"`
	MappingStatus  string         `json:"mapping_status"`
	WhyMatched     string         `json:"why_matched"`
}

func decodeMappingRef(raw json.RawMessage, maxBytes int) (MappingRef, error) {
	ref, err := DecodeGraphRefJSON(raw, maxBytes)
	if err == nil {
		switch v := ref.(type) {
		case *SymbolRef:
			return v, nil
		case *EntityRef:
			return v, nil
		case *ModuleRef:
			return v, nil
		case *AnchorRef:
			return v, nil
		case *FileRef:
			return v, nil
		}
	}
	dom, err := DecodeDOMAnchorRefJSON(raw, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid mapping ref")
	}
	return &dom, nil
}
func mappingRefMatchesMap(ref MappingRef, id string) bool {
	switch r := ref.(type) {
	case *DOMAnchorRef:
		return r != nil && r.BuildMapID == id
	case *AnchorRef:
		return r != nil && r.BuildMapID == id
	default:
		return ref != nil
	}
}
func DecodeMappingResolveRequestJSON(data []byte, maxBytes int) (MappingResolveRequest, error) {
	var r MappingResolveRequest
	f, err := optionalObject(data, maxBytes, "scope", "session_id", "build_map_id", "document_epoch", "hmr_epoch", "ref", "cursor", "limit", "max_output_bytes")
	if err != nil {
		return r, err
	}
	for _, key := range []string{"session_id", "build_map_id", "document_epoch", "hmr_epoch", "ref"} {
		if _, ok := f[key]; !ok {
			return r, fmt.Errorf("missing mapping request field")
		}
	}
	r.Ref, err = decodeMappingRef(f["ref"], maxBytes)
	if err != nil {
		return r, err
	}
	delete(f, "ref")
	r.Scope, err = decodeOptionalScope(f, maxBytes)
	if err != nil {
		return MappingResolveRequest{}, err
	}
	_, hasLimit := f["limit"]
	raw, err := json.Marshal(f)
	if err != nil {
		return r, err
	}
	var body MappingResolveRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return r, err
	}
	body.Ref = r.Ref
	body.Scope = r.Scope
	if !hasLimit {
		body.Limit = 20
	}
	if !uuid(body.SessionID) || !lowerHex(body.BuildMapID, 64) || body.DocumentEpoch < 1 || body.DocumentEpoch > maxExactInteger || body.HMREpoch < 0 || body.HMREpoch > maxExactInteger || body.Limit < 1 || body.Limit > 100 || !validOutputCap(body.MaxOutputBytes) || body.Cursor != nil && !validToken(*body.Cursor) || !mappingRefMatchesMap(body.Ref, body.BuildMapID) {
		return MappingResolveRequest{}, fmt.Errorf("invalid mapping request")
	}
	return body, nil
}
func DecodeMappingMatchJSON(data []byte, maxBytes int) (MappingMatch, error) {
	var r MappingMatch
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"match_id", "anchor_ref", "target_ref", "source_refs", "mapping_status", "evidence_refs"}, map[string]bool{"target_ref": true})
	if err != nil {
		return r, err
	}
	anchor, err := DecodeGraphRefJSON(f["anchor_ref"], maxBytes)
	if err != nil {
		return r, err
	}
	typed, ok := anchor.(*AnchorRef)
	if !ok {
		return r, fmt.Errorf("anchor ref required")
	}
	r.AnchorRef = *typed
	if !bytes.Equal(bytes.TrimSpace(f["target_ref"]), []byte("null")) {
		r.TargetRef, err = DecodeGraphRefJSON(f["target_ref"], maxBytes)
		if err != nil {
			return MappingMatch{}, err
		}
	}
	var sources, evidence []json.RawMessage
	if err := json.Unmarshal(f["source_refs"], &sources); err != nil {
		return r, err
	}
	if err := json.Unmarshal(f["evidence_refs"], &evidence); err != nil {
		return r, err
	}
	if sources == nil || evidence == nil || len(sources) > 16 || len(evidence) > 16 {
		return r, fmt.Errorf("invalid match evidence arrays")
	}
	r.SourceRefs = make([]SourceRef, 0, len(sources))
	for _, s := range sources {
		ref, err := DecodeSourceRefJSON(s, maxBytes)
		if err != nil {
			return MappingMatch{}, err
		}
		r.SourceRefs = append(r.SourceRefs, ref)
	}
	r.EvidenceRefs = make([]EvidenceRef, 0, len(evidence))
	for _, e := range evidence {
		item, err := DecodeEvidenceRefJSON(e, maxBytes)
		if err != nil {
			return MappingMatch{}, err
		}
		r.EvidenceRefs = append(r.EvidenceRefs, item)
	}
	if err := json.Unmarshal(f["match_id"], &r.MatchID); err != nil {
		return MappingMatch{}, err
	}
	if err := json.Unmarshal(f["mapping_status"], &r.MappingStatus); err != nil {
		return MappingMatch{}, err
	}
	if !lowerHex(r.MatchID, 64) {
		return MappingMatch{}, fmt.Errorf("invalid match id")
	}
	switch r.MappingStatus {
	case "exact_in_supported_scope", "candidate", "ancestor_fallback", "unmapped", "unsupported":
	default:
		return MappingMatch{}, fmt.Errorf("invalid match status")
	}
	return r, nil
}

// DecodeMappingDataJSON preserves anchor/target pairs and validates binding
// consistency, but it does not prove source authority or pagination completeness.
func DecodeMappingDataJSON(data []byte, maxBytes int) (MappingData, error) {
	var r MappingData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "requested_ref", "matches", "build_map_id", "binding_id", "resolver_digest", "mapping_status", "why_matched"}, nil)
	if err != nil {
		return r, err
	}
	r.RequestedRef, err = decodeMappingRef(f["requested_ref"], maxBytes)
	if err != nil {
		return r, err
	}
	delete(f, "requested_ref")
	var matches []json.RawMessage
	if err := json.Unmarshal(f["matches"], &matches); err != nil {
		return r, err
	}
	if matches == nil || len(matches) > 100 {
		return r, fmt.Errorf("invalid matches")
	}
	delete(f, "matches")
	raw, err := json.Marshal(f)
	if err != nil {
		return r, err
	}
	var body MappingData
	if err := json.Unmarshal(raw, &body); err != nil {
		return r, err
	}
	body.RequestedRef = r.RequestedRef
	body.Matches = make([]MappingMatch, 0, len(matches))
	if body.Operation != "mapping_resolve" || !lowerHex(body.BuildMapID, 64) || !lowerHex(body.BindingID, 64) || !lowerHex(body.ResolverDigest, 64) || !boundedString(body.WhyMatched, 1024) || !mappingRefMatchesMap(body.RequestedRef, body.BuildMapID) {
		return MappingData{}, fmt.Errorf("invalid mapping data")
	}
	switch body.MappingStatus {
	case "exact_in_supported_scope", "candidate", "ambiguous", "ancestor_fallback", "unmapped", "stale", "unsupported":
	default:
		return MappingData{}, fmt.Errorf("invalid mapping status")
	}
	if requested, ok := body.RequestedRef.(*AnchorRef); ok && requested.BindingID != body.BindingID {
		return MappingData{}, fmt.Errorf("requested binding mismatch")
	}
	for _, item := range matches {
		match, err := DecodeMappingMatchJSON(item, maxBytes)
		if err != nil {
			return MappingData{}, err
		}
		if match.AnchorRef.BuildMapID != body.BuildMapID || match.AnchorRef.BindingID != body.BindingID {
			return MappingData{}, fmt.Errorf("mapping binding mismatch")
		}
		body.Matches = append(body.Matches, match)
	}
	if body.MappingStatus == "exact_in_supported_scope" {
		var targetDigest string
		for _, match := range body.Matches {
			if match.TargetRef == nil || match.MappingStatus != "exact_in_supported_scope" {
				return MappingData{}, fmt.Errorf("exact mapping has non-exact visible evidence")
			}
			digest, err := GraphRefDigest(match.TargetRef, maxBytes)
			if err != nil || targetDigest != "" && targetDigest != digest {
				return MappingData{}, fmt.Errorf("exact mapping has multiple visible targets")
			}
			targetDigest = digest
		}
	}
	return body, nil
}
