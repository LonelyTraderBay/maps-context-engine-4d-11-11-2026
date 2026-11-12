package model

import (
	"encoding/json"
	"fmt"
	"sort"
)

type AllowedExecutable struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type PolicyConfig struct {
	SchemaVersion        int64               `json:"schema_version"`
	AllowedRoots         []string            `json:"allowed_roots"`
	DeniedGlobs          []string            `json:"denied_globs"`
	TrustedRulePaths     []string            `json:"trusted_rule_paths"`
	DependencyRoots      []string            `json:"dependency_roots"`
	AllowedExecutables   []AllowedExecutable `json:"allowed_executables"`
	AllowTargetExecution bool                `json:"allow_target_execution"`
	AllowRuntimeDownload bool                `json:"allow_runtime_download"`
	MaxFileBytes         int64               `json:"max_file_bytes"`
	MaxSourceBytes       int64               `json:"max_source_bytes"`
	MaxOutputBytes       int64               `json:"max_output_bytes"`
	SecretRulesVersion   string              `json:"secret_rules_version"`
}

func (p PolicyConfig) normalized() (PolicyConfig, error) {
	if p.SchemaVersion != 1 || p.AllowTargetExecution || p.AllowRuntimeDownload || p.MaxFileBytes < 1 || p.MaxFileBytes > maxExactInteger || p.MaxSourceBytes < 1 || p.MaxSourceBytes > maxExactInteger || p.MaxOutputBytes < 4096 || p.MaxOutputBytes > 131072 || !validID(p.SecretRulesVersion) {
		return PolicyConfig{}, fmt.Errorf("invalid policy")
	}
	result := p
	var err error
	if result.AllowedRoots, err = sortedStrings(p.AllowedRoots, 1, 32, 4096, 0); err != nil {
		return PolicyConfig{}, err
	}
	if result.DeniedGlobs, err = sortedStrings(p.DeniedGlobs, 0, 256, 512, 0); err != nil {
		return PolicyConfig{}, err
	}
	if result.DependencyRoots, err = sortedStrings(p.DependencyRoots, 0, 128, 4096, 0); err != nil {
		return PolicyConfig{}, err
	}
	if p.TrustedRulePaths == nil || len(p.TrustedRulePaths) > 128 || p.AllowedExecutables == nil || len(p.AllowedExecutables) > 32 {
		return PolicyConfig{}, fmt.Errorf("invalid policy lists")
	}
	result.TrustedRulePaths = append([]string{}, p.TrustedRulePaths...)
	for _, path := range result.TrustedRulePaths {
		if err := ValidateRelativePath(path, false); err != nil {
			return PolicyConfig{}, err
		}
	}
	result.AllowedExecutables = append([]AllowedExecutable{}, p.AllowedExecutables...)
	for _, exe := range result.AllowedExecutables {
		if !boundedString(exe.Path, 4096) || !lowerHex(exe.SHA256, 64) {
			return PolicyConfig{}, fmt.Errorf("invalid allowed executable")
		}
	}
	sort.Slice(result.AllowedExecutables, func(i, j int) bool {
		a, b := result.AllowedExecutables[i], result.AllowedExecutables[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.SHA256 < b.SHA256
	})
	return result, nil
}

// DecodePolicyConfigJSON checks the policy's closed shape and normalization
// requirements. It does not enforce the policy or authorize any filesystem path.
func DecodePolicyConfigJSON(data []byte, maxBytes int) (PolicyConfig, error) {
	var p PolicyConfig
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	fields, err := scopeObject(data, []string{"schema_version", "allowed_roots", "denied_globs", "trusted_rule_paths", "dependency_roots", "allowed_executables", "allow_target_execution", "allow_runtime_download", "max_file_bytes", "max_source_bytes", "max_output_bytes", "secret_rules_version"}, nil)
	if err != nil {
		return p, err
	}
	var executables []json.RawMessage
	if err := json.Unmarshal(fields["allowed_executables"], &executables); err != nil {
		return p, err
	}
	for _, e := range executables {
		if _, err := scopeObject(e, []string{"path", "sha256"}, nil); err != nil {
			return p, err
		}
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return PolicyConfig{}, err
	}
	if _, err := p.normalized(); err != nil {
		return PolicyConfig{}, err
	}
	return p, nil
}
func PolicyDigest(p PolicyConfig, maxBytes int) (string, error) {
	normalized, err := p.normalized()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	value, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("policy-v1", value, maxBytes)
	return digest, err
}
