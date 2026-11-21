package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"unicode/utf8"
)

var pythonVersionPattern = regexp.MustCompile(`^3\.[0-9]{1,2}\.[0-9]{1,3}$`)

type EffectiveProfile interface {
	normalizedProfile() (EffectiveProfile, error)
}
type GoProfile struct {
	SchemaVersion   int64    `json:"schema_version"`
	Language        string   `json:"language"`
	Goos            string   `json:"goos"`
	Goarch          string   `json:"goarch"`
	BuildTags       []string `json:"build_tags"`
	CgoEnabled      bool     `json:"cgo_enabled"`
	TestsEnabled    bool     `json:"tests_enabled"`
	ModuleRoots     []string `json:"module_roots"`
	ToolchainDigest string   `json:"toolchain_digest"`
	ProviderDigest  string   `json:"provider_digest"`
	ModuleMode      string   `json:"module_mode"`
	AnalysisMode    string   `json:"analysis_mode"`
	DependencyRoots []string `json:"dependency_roots"`
}
type TSProfile struct {
	SchemaVersion       int64    `json:"schema_version"`
	Language            string   `json:"language"`
	TsconfigPaths       []string `json:"tsconfig_paths"`
	CompilerDigest      string   `json:"compiler_digest"`
	NodeDigest          string   `json:"node_digest"`
	ProviderDigest      string   `json:"provider_digest"`
	AllowProjectPlugins bool     `json:"allow_project_plugins"`
	AnalysisMode        string   `json:"analysis_mode"`
}
type PythonProfile struct {
	SchemaVersion       int64    `json:"schema_version"`
	Language            string   `json:"language"`
	PythonVersion       string   `json:"python_version"`
	InterpreterDigest   string   `json:"interpreter_digest"`
	StdlibDigest        string   `json:"stdlib_digest"`
	ProviderDigest      string   `json:"provider_digest"`
	ModuleRoots         []string `json:"module_roots"`
	AllowProjectImports bool     `json:"allow_project_imports"`
	AnalysisMode        string   `json:"analysis_mode"`
}

func sortedStrings(values []string, min, max, maxLen int, pathKind int) ([]string, error) {
	if values == nil || len(values) < min || len(values) > max {
		return nil, fmt.Errorf("invalid array length")
	}
	out := append([]string{}, values...)
	for _, v := range out {
		n := utf8.RuneCountInString(v)
		if !utf8.ValidString(v) || n < 1 || n > maxLen {
			return nil, fmt.Errorf("invalid array item")
		}
		if pathKind != 0 {
			if err := ValidateRelativePath(v, pathKind == 2); err != nil {
				return nil, err
			}
		}
	}
	sort.Strings(out)
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			return nil, fmt.Errorf("duplicate array item")
		}
	}
	return out, nil
}
func boundedString(s string, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= max
}
func (p *GoProfile) normalizedProfile() (EffectiveProfile, error) {
	if p == nil || p.SchemaVersion != 1 || p.Language != "go" || !boundedString(p.Goos, 32) || !boundedString(p.Goarch, 32) || !lowerHex(p.ToolchainDigest, 64) || !lowerHex(p.ProviderDigest, 64) || (p.ModuleMode != "readonly" && p.ModuleMode != "vendor_explicit") || (p.AnalysisMode != "syntax" && p.AnalysisMode != "typed_when_available") {
		return nil, fmt.Errorf("invalid Go profile")
	}
	result := *p
	var err error
	if result.BuildTags, err = sortedStrings(p.BuildTags, 0, 128, 128, 0); err != nil {
		return nil, err
	}
	if result.ModuleRoots, err = sortedStrings(p.ModuleRoots, 1, 64, 4096, 2); err != nil {
		return nil, err
	}
	if result.DependencyRoots, err = sortedStrings(p.DependencyRoots, 0, 128, 4096, 0); err != nil {
		return nil, err
	}
	return &result, nil
}
func (p *TSProfile) normalizedProfile() (EffectiveProfile, error) {
	if p == nil || p.SchemaVersion != 1 || p.Language != "typescript_javascript" || !lowerHex(p.CompilerDigest, 64) || !lowerHex(p.NodeDigest, 64) || !lowerHex(p.ProviderDigest, 64) || p.AllowProjectPlugins || (p.AnalysisMode != "syntax" && p.AnalysisMode != "typed_when_available") {
		return nil, fmt.Errorf("invalid TS profile")
	}
	result := *p
	paths, err := sortedStrings(p.TsconfigPaths, 1, 64, 4096, 1)
	if err != nil {
		return nil, err
	}
	result.TsconfigPaths = paths
	return &result, nil
}
func (p *PythonProfile) normalizedProfile() (EffectiveProfile, error) {
	if p == nil || p.SchemaVersion != 1 || p.Language != "python" || !pythonVersionPattern.MatchString(p.PythonVersion) ||
		!lowerHex(p.InterpreterDigest, 64) || !lowerHex(p.StdlibDigest, 64) || !lowerHex(p.ProviderDigest, 64) ||
		p.AllowProjectImports || (p.AnalysisMode != "syntax" && p.AnalysisMode != "typed_when_available") {
		return nil, fmt.Errorf("invalid Python profile")
	}
	result := *p
	roots, err := sortedStrings(p.ModuleRoots, 1, 64, 4096, 2)
	if err != nil {
		return nil, err
	}
	result.ModuleRoots = roots
	return &result, nil
}

func DecodeEffectiveProfileJSON(data []byte, maxBytes int) (EffectiveProfile, error) {
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return nil, err
	}
	var head struct {
		Language string `json:"language"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	var result EffectiveProfile
	var fields []string
	switch head.Language {
	case "go":
		fields = []string{"schema_version", "language", "goos", "goarch", "build_tags", "cgo_enabled", "tests_enabled", "module_roots", "toolchain_digest", "provider_digest", "module_mode", "analysis_mode", "dependency_roots"}
		result = &GoProfile{}
	case "typescript_javascript":
		fields = []string{"schema_version", "language", "tsconfig_paths", "compiler_digest", "node_digest", "provider_digest", "allow_project_plugins", "analysis_mode"}
		result = &TSProfile{}
	case "python":
		fields = []string{"schema_version", "language", "python_version", "interpreter_digest", "stdlib_digest", "provider_digest", "module_roots", "allow_project_imports", "analysis_mode"}
		result = &PythonProfile{}
	default:
		return nil, fmt.Errorf("unknown profile language")
	}
	if _, err := scopeObject(data, fields, nil); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, err
	}
	if _, err := result.normalizedProfile(); err != nil {
		return nil, err
	}
	return result, nil
}

// ProfileDigest hashes the validated, normalized effective profile; it does not
// prove that the named toolchains or paths are trusted or available.
func ProfileDigest(profile EffectiveProfile, maxBytes int) (string, error) {
	if profile == nil {
		return "", fmt.Errorf("missing profile")
	}
	normalized, err := profile.normalizedProfile()
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
	_, digest, err := EncodeCanonical("profile-v1", value, maxBytes)
	return digest, err
}
