package model

import (
	"encoding/json"
	"fmt"
)

type PackageFact struct {
	PackageKey     string `json:"package_key"`
	ImportPath     string `json:"import_path"`
	ModuleKey      string `json:"module_key"`
	Variant        string `json:"variant"`
	InputDigest    string `json:"input_digest"`
	SemanticStatus string `json:"semantic_status"`
}
type DiagnosticFact struct {
	DiagnosticID     string  `json:"diagnostic_id"`
	Path             *string `json:"path"`
	PackageKey       *string `json:"package_key"`
	Code             string  `json:"code"`
	Severity         string  `json:"severity"`
	Message          string  `json:"message"`
	ScopeDescription string  `json:"scope_description"`
}

func DecodePackageFactJSON(data []byte, maxBytes int) (PackageFact, error) {
	var p PackageFact
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	if _, err := scopeObject(data, []string{"package_key", "import_path", "module_key", "variant", "input_digest", "semantic_status"}, nil); err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return PackageFact{}, err
	}
	if !validID(p.PackageKey) || !boundedString(p.ImportPath, 2048) || !validID(p.ModuleKey) || !lowerHex(p.InputDigest, 64) {
		return PackageFact{}, fmt.Errorf("invalid package fact")
	}
	switch p.Variant {
	case "production", "internal_test", "external_test", "other_supported":
	default:
		return PackageFact{}, fmt.Errorf("invalid package variant")
	}
	switch p.SemanticStatus {
	case "complete_in_profile", "partial", "unsupported", "unknown":
	default:
		return PackageFact{}, fmt.Errorf("invalid package status")
	}
	return p, nil
}
func DecodeDiagnosticFactJSON(data []byte, maxBytes int) (DiagnosticFact, error) {
	var d DiagnosticFact
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return d, err
	}
	if _, err := scopeObject(data, []string{"diagnostic_id", "path", "package_key", "code", "severity", "message", "scope_description"}, map[string]bool{"path": true, "package_key": true}); err != nil {
		return d, err
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return DiagnosticFact{}, err
	}
	if !validID(d.DiagnosticID) || (d.Path != nil && !validSourcePath(*d.Path)) || !optionalID(d.PackageKey) || !validID(d.Code) || !boundedString(d.Message, 1024) || !boundedString(d.ScopeDescription, 1024) {
		return DiagnosticFact{}, fmt.Errorf("invalid diagnostic")
	}
	switch d.Severity {
	case "info", "warning", "error":
	default:
		return DiagnosticFact{}, fmt.Errorf("invalid severity")
	}
	return d, nil
}
