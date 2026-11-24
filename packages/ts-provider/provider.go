// Package tsprovider configures the read-only TS/JS worker for the S43 process adapter.
package tsprovider

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

//go:embed worker.cjs
var workerArtifact []byte

//go:embed toolchain-pin.json
var toolchainPin []byte

//go:embed frontend.cjs
var frontendArtifact []byte

// Installation names files in the approved provider installation. These paths
// belong to the state owner and cannot be selected by a target project.
type Installation struct {
	Node, Worker, ScratchParent                     string
	CompilerRoot, CompilerArchive, CompilerManifest string
}

// New binds the exact worker and compiler installation to an explicit S43
// profile. The returned profile can be hashed into the capture before Analyze.
func New(installation Installation, configs []string, mode string) (*providers.NodeProvider, *model.TSProfile, error) {
	var embedded []byte
	if installation.Worker == "" {
		embedded = workerArtifact
	}
	return newProvider(installation, configs, mode, embedded)
}

// NewFrontend selects the pinned worker with the frontend adapter included.
// Its profile distinguishes the combined program from the base TS worker.
func NewFrontend(installation Installation, configs []string, mode string) (*FrontendProvider, *model.TSProfile, error) {
	if installation.Worker != "" {
		return nil, nil, providers.ErrAuthority
	}
	node, profile, err := newProvider(installation, configs, mode, frontendProgram())
	if err != nil {
		return nil, nil, err
	}
	return &FrontendProvider{NodeProvider: node}, profile, nil
}

func frontendProgram() []byte {
	program := make([]byte, 0, len(workerArtifact)+len(frontendArtifact)+1)
	program = append(program, workerArtifact...)
	program = append(program, '\n')
	return append(program, frontendArtifact...)
}

func newProvider(installation Installation, configs []string, mode string, embedded []byte) (*providers.NodeProvider, *model.TSProfile, error) {
	var pins struct {
		Compiler struct {
			ArchiveDigest  string `json:"archive_sha256"`
			ManifestDigest string `json:"file_manifest_sha256"`
		} `json:"compiler"`
		Node struct {
			ExecutableDigest string `json:"executable_sha256"`
		} `json:"node"`
	}
	if err := json.Unmarshal(toolchainPin, &pins); err != nil {
		return nil, nil, err
	}
	artifact := workerArtifact
	if embedded != nil {
		artifact = embedded
	}
	digest := sha256.Sum256(artifact)
	profile := &model.TSProfile{
		SchemaVersion: 1, Language: "typescript_javascript", TsconfigPaths: append([]string{}, configs...),
		NodeDigest: pins.Node.ExecutableDigest, CompilerDigest: pins.Compiler.ArchiveDigest,
		ProviderDigest: hex.EncodeToString(digest[:]), AnalysisMode: mode,
	}
	if _, err := model.ProfileDigest(profile, 32768); err != nil {
		return nil, nil, err
	}
	node := providers.NewNodeProvider(providers.NodeConfig{
		NodePath: installation.Node, Entrypoint: installation.Worker, ScratchParent: installation.ScratchParent,
		NodeDigest: profile.NodeDigest, CompilerDigest: profile.CompilerDigest,
		ProviderDigest: profile.ProviderDigest, ProviderID: ProviderID,
		Compiler: providers.CompilerInstallation{Root: installation.CompilerRoot,
			ArchivePath: installation.CompilerArchive, ManifestPath: installation.CompilerManifest,
			ManifestDigest: pins.Compiler.ManifestDigest},
		EmbeddedWorker: embedded,
	})
	return node, profile, nil
}

const ProviderID = "mce4d-ts"
