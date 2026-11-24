package pythonprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	native "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

//go:embed toolchain-windows-pin.json
var windowsToolchainPin []byte

//go:embed worker_windows.py
var windowsBootstrap []byte

type windowsPin struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

func loadWindowsPin() (pin windowsPin, err error) {
	err = json.Unmarshal(windowsToolchainPin, &pin)
	return pin, err
}

func WindowsProgram() ([]byte, error) {
	// Keep the enrolled Linux worker bytes intact and share its parser and
	// framing. Windows substitutes only the platform launcher prelude.
	delimiter := []byte("\nMAX_FILE = ")
	pos := bytes.Index(parserProgram, delimiter)
	if pos < 0 || bytes.Count(parserProgram, delimiter) != 1 {
		return nil, providers.ErrAuthority
	}
	return append(bytes.Clone(windowsBootstrap), parserProgram[pos:]...), nil
}

func WindowsProfile(roots []string, mode string) (*model.PythonProfile, error) {
	if runtime.GOARCH != "amd64" {
		return nil, providers.ErrUnsupported
	}
	pin, err := loadWindowsPin()
	if err != nil {
		return nil, err
	}
	program, err := WindowsProgram()
	if err != nil {
		return nil, err
	}
	profile := &model.PythonProfile{SchemaVersion: 1, Language: "python", PythonVersion: pin.Version,
		InterpreterDigest: pin.Files["python.exe"], StdlibDigest: pin.Files["python314.zip"],
		ProviderDigest: fmt.Sprintf("%x", sha256.Sum256(program)), ModuleRoots: append([]string{}, roots...), AnalysisMode: mode}
	if _, err := model.ProfileDigest(profile, 32768); err != nil {
		return nil, err
	}
	return profile, nil
}

func VerifyWindowsProfile(profile *model.PythonProfile) error {
	if profile == nil {
		return providers.ErrAuthority
	}
	expected, err := WindowsProfile(profile.ModuleRoots, profile.AnalysisMode)
	if err != nil {
		return err
	}
	want, _ := model.ProfileDigest(expected, 32768)
	actual, err := model.ProfileDigest(profile, 32768)
	if err != nil || want != actual {
		return providers.ErrAuthority
	}
	return nil
}

func AnalyzeWindows(ctx context.Context, tool, capture *paths.Root, script string, job ports.ProviderJob,
	sink ports.ProviderSink) (model.ProviderDone, error) {
	profile, ok := job.Start.Profile.(*model.PythonProfile)
	if !ok || VerifyWindowsProfile(profile) != nil || job.Start.ProviderID != ProviderID {
		return model.ProviderDone{}, providers.ErrAuthority
	}
	pin, err := loadWindowsPin()
	if err != nil {
		return model.ProviderDone{}, err
	}
	if err := providers.VerifyWindowsMaterialTree(ctx, tool, pin.Files, 64<<20); err != nil {
		return model.ProviderDone{}, err
	}
	home, err := tool.CanonicalPath()
	if err != nil {
		return model.ProviderDone{}, err
	}
	return providers.RunWindowsProvider(ctx, capture, job, native.WindowsJobChild{
		Executable: filepath.Join(home, "python.exe"), Directory: job.Start.SnapshotRoot,
		Arguments: []string{"-I", "-S", "-B", script},
	}, sink)
}
