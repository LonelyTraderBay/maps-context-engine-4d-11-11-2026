package golang

import (
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// LoaderEnvironment prepares only a bounded offline environment. It does not
// verify the toolchain, grant scratch access, or start packages.Load.
func LoaderEnvironment(profile *model.GoProfile, goRoot, scratchRoot, workspaceRoot string) ([]string, []string, error) {
	if profile == nil || profile.CgoEnabled || profile.ModuleMode != "readonly" || profile.AnalysisMode != "typed_when_available" {
		return nil, nil, ErrLoaderInput
	}
	if _, err := model.ProfileDigest(profile, 64*1024); err != nil {
		return nil, nil, ErrLoaderInput
	}
	return runtimeadapter.OfflineGoEnvironment(runtimeadapter.GoEnvironmentInput{GoRoot: goRoot, ScratchRoot: scratchRoot, WorkspaceRoot: workspaceRoot, GOOS: profile.Goos, GOARCH: profile.Goarch, BuildTags: profile.BuildTags})
}
