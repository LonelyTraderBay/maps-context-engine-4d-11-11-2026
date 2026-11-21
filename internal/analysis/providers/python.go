package providers

import "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"

// PythonConfig is supplied by the installation owner. The target cannot
// select the interpreter, its libraries, the parser, or process arguments.
type PythonConfig struct {
	Interpreter, Stdlib, ScratchParent string
	Version, InterpreterDigest         string
	StdlibDigest, ProviderDigest       string
	ProviderID                         string
	AnalysisMode                       string
	Program                            []byte
}

type PythonProvider struct{ config PythonConfig }

func NewPythonProvider(config PythonConfig) *PythonProvider {
	config.Program = append([]byte(nil), config.Program...)
	return &PythonProvider{config: config}
}

var _ ports.LanguageProvider = (*PythonProvider)(nil)
