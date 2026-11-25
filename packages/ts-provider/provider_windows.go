package tsprovider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	native "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type windowsPin struct {
	Compiler struct {
		Archive  string `json:"archive_sha256"`
		Manifest string `json:"file_manifest_sha256"`
	} `json:"compiler"`
	Node struct {
		Executable string `json:"executable_sha256"`
		License    string `json:"license_sha256"`
	} `json:"node_windows"`
}

func loadWindowsPin() (pin windowsPin, err error) {
	err = json.Unmarshal(toolchainPin, &pin)
	return pin, err
}

func WindowsProfile(configs []string, mode string) (*model.TSProfile, error) {
	if runtime.GOARCH != "amd64" {
		return nil, providers.ErrUnsupported
	}
	pin, err := loadWindowsPin()
	if err != nil {
		return nil, err
	}
	profile := &model.TSProfile{SchemaVersion: 1, Language: "typescript_javascript", TsconfigPaths: append([]string{}, configs...),
		NodeDigest: pin.Node.Executable, CompilerDigest: pin.Compiler.Archive, ProviderDigest: fmt.Sprintf("%x", sha256.Sum256(workerArtifact)), AnalysisMode: mode}
	if _, err := model.ProfileDigest(profile, 32768); err != nil {
		return nil, err
	}
	return profile, nil
}

func VerifyWindowsProfile(profile *model.TSProfile) error {
	if profile == nil {
		return providers.ErrAuthority
	}
	expected, err := WindowsProfile(profile.TsconfigPaths, profile.AnalysisMode)
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

func WindowsProgram() []byte { return append([]byte(nil), workerArtifact...) }

func AnalyzeWindows(ctx context.Context, tool, capture *paths.Root, script string, job ports.ProviderJob,
	sink ports.ProviderSink) (model.ProviderDone, error) {
	profile, ok := job.Start.Profile.(*model.TSProfile)
	if !ok || VerifyWindowsProfile(profile) != nil || job.Start.ProviderID != ProviderID || job.Capture == nil {
		return model.ProviderDone{}, providers.ErrAuthority
	}
	for _, config := range profile.TsconfigPaths {
		if _, err := job.Capture.CheckCapturedRange(config, model.ByteRange{}); err != nil {
			return model.ProviderDone{}, err
		}
	}
	if err := verifyWindowsInstallation(ctx, tool); err != nil {
		return model.ProviderDone{}, err
	}
	home, err := tool.CanonicalPath()
	if err != nil || strings.ContainsAny(home+script+job.Start.SnapshotRoot, "*,") {
		return model.ProviderDone{}, errors.Join(providers.ErrAuthority, err)
	}
	compiler := filepath.Join(home, "compiler")
	return providers.RunWindowsProvider(ctx, capture, job, native.WindowsJobChild{
		Executable: filepath.Join(home, "node.exe"), Directory: home,
		Arguments: []string{"--permission", "--max-old-space-size=512", "--allow-fs-read=" + script,
			"--allow-fs-read=" + job.Start.SnapshotRoot, "--allow-fs-read=" + compiler, script},
		Environment: []string{"MCE4D_COMPILER_ROOT=" + compiler, "TZ=UTC"},
	}, sink)
}

func verifyWindowsInstallation(ctx context.Context, tool *paths.Root) error {
	pin, err := loadWindowsPin()
	if err != nil {
		return err
	}
	manifestDigest := pin.Compiler.Manifest
	file, err := tool.OpenRegular("compiler-files.json")
	if err != nil {
		return errors.Join(providers.ErrAuthority, err)
	}
	const limit = 128 << 10
	raw, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	if err := errors.Join(readErr, file.Close()); err != nil || len(raw) > limit || fmt.Sprintf("%x", sha256.Sum256(raw)) != manifestDigest {
		return errors.Join(providers.ErrAuthority, err)
	}
	var files map[string]string
	if json.Unmarshal(raw, &files) != nil {
		return providers.ErrAuthority
	}
	material := map[string]string{"node.exe": pin.Node.Executable, "LICENSE": pin.Node.License,
		"typescript-5.9.3.tgz": pin.Compiler.Archive, "compiler-files.json": manifestDigest}
	for name, hash := range files {
		material["compiler/"+name] = hash
	}
	return providers.VerifyWindowsMaterialTree(ctx, tool, material, 160<<20)
}
