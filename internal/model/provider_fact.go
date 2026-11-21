package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type SymbolFact struct {
	SymbolID      string     `json:"symbol_id"`
	Path          *string    `json:"path"`
	PackageKey    *string    `json:"package_key"`
	Kind          string     `json:"kind"`
	Name          string     `json:"name"`
	QualifiedName string     `json:"qualified_name"`
	ScopeKey      string     `json:"scope_key"`
	Range         *ByteRange `json:"range"`
	Signature     string     `json:"signature"`
	Exported      bool       `json:"exported"`
}
type RelationFact struct {
	RelationID      string    `json:"relation_id"`
	SourceSymbolID  string    `json:"source_symbol_id"`
	TargetSymbolID  *string   `json:"target_symbol_id"`
	Kind            string    `json:"kind"`
	EvidenceKind    string    `json:"evidence_kind"`
	SitePath        string    `json:"site_path"`
	Range           ByteRange `json:"range"`
	Reason          string    `json:"reason"`
	UnresolvedLabel *string   `json:"unresolved_label"`
}

func stringAtMost(s string, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max
}
func decodeByteRange(raw json.RawMessage) (ByteRange, error) {
	var r ByteRange
	if _, err := scopeObject(raw, []string{"start_byte", "end_byte"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ByteRange{}, err
	}
	if r.StartByte < 0 || r.EndByte < r.StartByte || r.EndByte > maxExactInteger {
		return ByteRange{}, fmt.Errorf("invalid byte range")
	}
	return r, nil
}

// DecodeSymbolFactJSON validates shape and boundary identity; source membership
// and range validity against captured bytes are checked by the consumer.
func DecodeSymbolFactJSON(data []byte, maxBytes int) (SymbolFact, error) {
	var s SymbolFact
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return s, err
	}
	f, err := scopeObject(data, []string{"symbol_id", "path", "package_key", "kind", "name", "qualified_name", "scope_key", "range", "signature", "exported"}, map[string]bool{"path": true, "package_key": true, "range": true})
	if err != nil {
		return s, err
	}
	if !bytes.Equal(bytes.TrimSpace(f["range"]), []byte("null")) {
		if _, err := decodeByteRange(f["range"]); err != nil {
			return s, err
		}
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return SymbolFact{}, err
	}
	if !validID(s.SymbolID) || !validID(s.ScopeKey) || !optionalID(s.PackageKey) || (s.Path != nil && !validSourcePath(*s.Path)) || !stringAtMost(s.Name, 512) || !stringAtMost(s.QualifiedName, 2048) || !stringAtMost(s.Signature, 32768) {
		return SymbolFact{}, fmt.Errorf("invalid symbol")
	}
	switch s.Kind {
	case "file", "package", "function", "method", "type", "interface", "field", "variable", "parameter", "receiver", "result", "constant", "closure", "external":
	default:
		return SymbolFact{}, fmt.Errorf("invalid symbol kind")
	}
	if (s.Path == nil) != (s.Range == nil) {
		return SymbolFact{}, fmt.Errorf("symbol path and range disagree")
	}
	if s.Path == nil && (s.Kind != "external" || s.PackageKey == nil || s.QualifiedName == "") {
		return SymbolFact{}, fmt.Errorf("invalid external symbol boundary")
	}
	return s, nil
}

// DecodeRelationFactJSON rejects unresolved boundaries masquerading as exact
// targets. Graph endpoints and source authorization remain consumer checks.
func DecodeRelationFactJSON(data []byte, maxBytes int) (RelationFact, error) {
	var r RelationFact
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"relation_id", "source_symbol_id", "target_symbol_id", "kind", "evidence_kind", "site_path", "range", "reason", "unresolved_label"}, map[string]bool{"target_symbol_id": true, "unresolved_label": true})
	if err != nil {
		return r, err
	}
	if _, err := decodeByteRange(f["range"]); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return RelationFact{}, err
	}
	if !validID(r.RelationID) || !validID(r.SourceSymbolID) || !optionalID(r.TargetSymbolID) || !validSourcePath(r.SitePath) || !stringAtMost(r.Reason, 1024) || (r.UnresolvedLabel != nil && !boundedString(*r.UnresolvedLabel, 512)) {
		return RelationFact{}, fmt.Errorf("invalid relation")
	}
	if !ValidRelationKind(r.Kind) {
		return RelationFact{}, fmt.Errorf("invalid relation kind")
	}
	switch r.EvidenceKind {
	case "syntax_exact", "type_resolved", "type_possible", "explicit_document", "text_match", "unresolved":
	default:
		return RelationFact{}, fmt.Errorf("invalid evidence kind")
	}
	unresolved := r.Kind == "UNRESOLVED_REF" || r.Kind == "UNRESOLVED_CALL"
	if (r.TargetSymbolID == nil) != unresolved || (unresolved && (r.Reason == "" || r.EvidenceKind != "unresolved" || r.UnresolvedLabel == nil)) {
		return RelationFact{}, fmt.Errorf("invalid unresolved relation")
	}
	return r, nil
}

func ValidRelationKind(kind string) bool {
	switch kind {
	case "CONTAINS", "IMPORTS", "REFERS_TO", "READS", "WRITES", "CALLS_DIRECT", "CALLS_INTERFACE", "MAY_DISPATCH_TO", "UNRESOLVED_REF", "UNRESOLVED_CALL", "TEST_REF", "DOC_LINK":
		return true
	default:
		return false
	}
}
