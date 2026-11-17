//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"runtime"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

// RegisterNativeRepo registers only an explicitly installation-approved local
// Git root. The default profile uses the embedded syntax analyzer; no Go/Git
// command, target code, hook, dependency download or indexing is performed.
func RegisterNativeRepo(ctx context.Context, installation, selected, rootPath string, cpus int) (result catalog.Registration, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, state.Close())
		if resultErr != nil {
			result = catalog.Registration{}
		}
	}()
	if !state.TargetApproved(rootPath) {
		return result, ErrAuthority
	}
	config, err := protectedConfig(ctx, state, selected, cpus)
	if err != nil {
		return result, err
	}
	effective, _, err := config.StartupConfig()
	if err != nil {
		return result, err
	}
	root, err := paths.OpenRoot(rootPath)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	common, err := runtimeadapter.OpenLocalGitMetadata(root)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, common.Close()) }()
	store, err := state.OpenCatalog(ctx)
	if err != nil {
		return result, err
	}
	operation, err := nativeOperationID()
	if err != nil {
		return result, err
	}
	mutation, err := runtimeadapter.AcquireCatalogMutation(ctx, state.CatalogLease(), operation, 5*time.Second)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, mutation.Close()) }()
	bound, err := store.WithAuthorizedMutation(ctx, mutation, operation, state.Check)
	if err != nil {
		return result, err
	}
	policy := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{rootPath}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: effective.Core.MaxFileBytes, MaxSourceBytes: effective.Core.MaxSourceBytesPerSnapshot, MaxOutputBytes: effective.Core.MaxOutputBytes, SecretRulesVersion: "builtin-v1"}
	toolchain := sha256.Sum256([]byte("MCE4D-EMBEDDED-GO-PARSER1\x00" + runtime.Version() + "\x00" + state.ExecutableDigest()))
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: runtime.GOOS, Goarch: runtime.GOARCH, BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: hex.EncodeToString(toolchain[:]), ProviderDigest: state.ExecutableDigest(), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	p, err := json.Marshal(policy)
	if err != nil {
		return result, err
	}
	prof, err := json.Marshal(profile)
	if err != nil {
		return result, err
	}
	return bound.Register(ctx, root, common, "", "default", p, prof)
}
