package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type RecordRef struct {
	RecordID string `json:"record_id"`
	Version  int64  `json:"version"`
}

// EvidenceRef labels the origin and kind of a claim; the label does not prove it.
type EvidenceRef struct {
	EvidenceID string      `json:"evidence_id"`
	Origin     string      `json:"origin"`
	Kind       string      `json:"kind"`
	SourceRefs []SourceRef `json:"source_refs"`
	RecordRef  *RecordRef  `json:"record_ref"`
	ArtifactID *string     `json:"artifact_id"`
}

func validEvidenceKind(origin, kind string) bool {
	switch origin {
	case "static_analyzer":
		return kind == "syntax_exact" || kind == "type_resolved" || kind == "type_possible" || kind == "explicit_document" || kind == "text_match" || kind == "unresolved"
	case "build_instrumentation":
		return kind == "build_anchored" || kind == "unresolved"
	case "runtime_observation":
		return kind == "runtime_observed" || kind == "unresolved"
	case "manual_approved":
		return kind == "manual_declared"
	case "ai_proposal":
		return kind == "proposed"
	default:
		return false
	}
}
func DecodeEvidenceRefJSON(data []byte, maxBytes int) (EvidenceRef, error) {
	var result EvidenceRef
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return result, err
	}
	fields, err := scopeObject(data, []string{"evidence_id", "origin", "kind", "source_refs", "record_ref", "artifact_id"}, map[string]bool{"record_ref": true, "artifact_id": true})
	if err != nil {
		return result, err
	}
	var refs []json.RawMessage
	if err := json.Unmarshal(fields["source_refs"], &refs); err != nil {
		return result, err
	}
	if len(refs) > 16 {
		return result, fmt.Errorf("too many source refs")
	}
	for _, ref := range refs {
		if _, err := DecodeSourceRefJSON(ref, maxBytes); err != nil {
			return result, err
		}
	}
	if !bytes.Equal(bytes.TrimSpace(fields["record_ref"]), []byte("null")) {
		if _, err := scopeObject(fields["record_ref"], []string{"record_id", "version"}, nil); err != nil {
			return result, err
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return EvidenceRef{}, err
	}
	if !validID(result.EvidenceID) || !validEvidenceKind(result.Origin, result.Kind) || result.SourceRefs == nil || (result.ArtifactID != nil && !lowerHex(*result.ArtifactID, 64)) || (result.RecordRef != nil && (!uuid(result.RecordRef.RecordID) || result.RecordRef.Version < 1 || result.RecordRef.Version > maxExactInteger)) {
		return EvidenceRef{}, fmt.Errorf("invalid evidence ref")
	}
	if result.Kind == "explicit_document" && len(result.SourceRefs) == 0 && result.RecordRef == nil && result.ArtifactID == nil {
		return EvidenceRef{}, fmt.Errorf("document evidence has no source reference")
	}
	return result, nil
}
