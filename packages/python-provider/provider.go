// Package pythonprovider prepares the installation-owned CPython AST adapter.
package pythonprovider

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

//go:embed worker.py
var parserProgram []byte

//go:embed toolchain-pin.json
var toolchainPin []byte

const ProviderID = "mce4d-python-syntax"

type ToolchainPin struct {
	Version     string `json:"version"`
	Interpreter string `json:"interpreter_sha256"`
	Stdlib      string `json:"stdlib_fingerprint"`
}

func PinnedToolchain() (pin ToolchainPin, err error) {
	err = json.Unmarshal(toolchainPin, &pin)
	return pin, err
}

// Installation paths are selected by the tool owner. Captured projects cannot
// select a virtualenv, startup hook, module path, executable, or parser.
type Installation struct {
	Interpreter, Stdlib, ScratchParent string
}

// New returns the worker and the closed profile to bind into profile-v1 before
// writing a capture. Analyze verifies the pinned installation before use.
func New(installation Installation, moduleRoots []string) (*providers.PythonProvider, *model.PythonProfile, error) {
	return newProvider(installation, moduleRoots, "syntax")
}

// NewTyped selects the independently verified static enrichment profile.
func NewTyped(installation Installation, moduleRoots []string) (*providers.PythonProvider, *model.PythonProfile, error) {
	return newProvider(installation, moduleRoots, "typed_when_available")
}

func newProvider(installation Installation, moduleRoots []string, mode string) (*providers.PythonProvider, *model.PythonProfile, error) {
	pin, err := PinnedToolchain()
	if err != nil {
		return nil, nil, err
	}
	program := sha256.Sum256(parserProgram)
	profile := &model.PythonProfile{SchemaVersion: 1, Language: "python", PythonVersion: pin.Version,
		InterpreterDigest: pin.Interpreter, StdlibDigest: pin.Stdlib,
		ProviderDigest: hex.EncodeToString(program[:]), ModuleRoots: append([]string{}, moduleRoots...), AnalysisMode: mode}
	if _, err := model.ProfileDigest(profile, 32768); err != nil {
		return nil, nil, err
	}
	return providers.NewPythonProvider(providers.PythonConfig{Interpreter: installation.Interpreter,
		Stdlib: installation.Stdlib, ScratchParent: installation.ScratchParent, Version: profile.PythonVersion,
		InterpreterDigest: profile.InterpreterDigest, StdlibDigest: profile.StdlibDigest, ProviderDigest: profile.ProviderDigest,
		ProviderID: ProviderID, AnalysisMode: mode, Program: parserProgram}), profile, nil
}
