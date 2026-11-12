package golang

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

var (
	ErrLoaderInput     = errors.New("invalid Go loader preflight input")
	ErrLoaderAuthority = errors.New("Go loader authority could not be verified")
)

func loaderAuthorityError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrLoaderAuthority
}

// ClosureVerifier must independently establish that build-relevant ignored,
// denied, missing, nested, dependency and generated inputs cannot be read
// outside this exact capture and that the inventory is still current. The
// callback is a trust boundary; a successful preflight is not that proof.
type ClosureVerifier func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error)

// ToolchainVerifier must verify the allowlisted executable and complete
// toolchain identity for this profile, including target GOOS/GOARCH. This
// preflight does not execute or attest any binary by itself.
type ToolchainVerifier func(context.Context, *model.GoProfile) (bool, error)

// DependencyVerifier must prove every supplied external root and digest is
// authorized and bound to the complete toolchain/dependency input closure.
type DependencyVerifier func(context.Context, *model.GoProfile, []string) (bool, error)

type LoaderPreflightInput struct {
	Snapshot           *snapshot.Snapshot
	Profile            *model.GoProfile
	Inventory          inventory.FileMap
	PolicyEpoch        int64
	Scanner            *policy.Scanner
	AuthorizeScope     func(context.Context, string, string, string, int64) error
	AuthorizePath      func(context.Context, string, string, string, int64) error
	VerifyClosure      ClosureVerifier
	VerifyToolchain    ToolchainVerifier
	VerifyDependencies DependencyVerifier
	MaxManifestBytes   int
	MaxFileBytes       int
	MaxSourceBytes     int64
}

// The profile and inventory are observations for one load. Copy them before
// invoking caller callbacks so later mutations cannot change that load's
// configuration or the baseline against which closure is checked.
func copyLoaderPreflight(input LoaderPreflightInput) (LoaderPreflightInput, error) {
	if input.Profile == nil || input.MaxManifestBytes < 1 || input.MaxManifestBytes > 64*1024*1024 || input.Inventory.Entries == nil || len(input.Inventory.Entries) > 100000 {
		return LoaderPreflightInput{}, ErrLoaderInput
	}
	profileInput := input.Profile
	if len(profileInput.Language) > 32 || len(profileInput.Goos) > 128 || len(profileInput.Goarch) > 128 || len(profileInput.ToolchainDigest) > 64 || len(profileInput.ProviderDigest) > 64 || len(profileInput.ModuleMode) > 128 || len(profileInput.AnalysisMode) > 128 ||
		len(profileInput.BuildTags) > 128 || len(profileInput.ModuleRoots) > 64 || len(profileInput.DependencyRoots) > 128 {
		return LoaderPreflightInput{}, ErrLoaderInput
	}
	for _, value := range profileInput.BuildTags {
		if len(value) > 128*4 {
			return LoaderPreflightInput{}, ErrLoaderInput
		}
	}
	for _, values := range [][]string{profileInput.ModuleRoots, profileInput.DependencyRoots} {
		for _, value := range values {
			if len(value) > 4096*4 {
				return LoaderPreflightInput{}, ErrLoaderInput
			}
		}
	}
	for _, entry := range input.Inventory.Entries {
		if len(entry.Path) > 16384 || len(entry.Stages) > 4 {
			return LoaderPreflightInput{}, ErrLoaderInput
		}
	}
	data, err := json.Marshal(input.Profile)
	if err != nil {
		return LoaderPreflightInput{}, ErrLoaderInput
	}
	decoded, err := model.DecodeEffectiveProfileJSON(data, input.MaxManifestBytes)
	if err != nil {
		return LoaderPreflightInput{}, ErrLoaderInput
	}
	profile, ok := decoded.(*model.GoProfile)
	if !ok {
		return LoaderPreflightInput{}, ErrLoaderInput
	}
	input.Profile = profile
	input.Inventory.Entries = append([]inventory.FileMapEntry{}, input.Inventory.Entries...)
	for i := range input.Inventory.Entries {
		input.Inventory.Entries[i].Stages = append([]uint8(nil), input.Inventory.Entries[i].Stages...)
	}
	return input, nil
}

type LoaderModule struct {
	Root       string
	ImportPath string
}

// LoaderPreflight is only an eligibility check. It does not execute a loader,
// prove its execution environment, or establish typed semantic completeness.
type LoaderPreflight struct {
	SnapshotDigest string
	ProfileID      string
	Status         string // eligible_for_guarded_load or partial
	Reasons        []string
	Modules        []LoaderModule
}

// PreflightLoader checks immutable capture, policy and a conservative package
// input boundary. No target filesystem or executable is opened by this method.
// Missing proof is reported as partial and never authorizes a loader run.
func PreflightLoader(ctx context.Context, input LoaderPreflightInput) (LoaderPreflight, error) {
	if ctx == nil || input.Snapshot == nil || input.Profile == nil || input.Scanner == nil || input.AuthorizeScope == nil || input.AuthorizePath == nil ||
		input.PolicyEpoch < 1 || input.PolicyEpoch > 9007199254740991 || input.MaxManifestBytes < 1 || input.MaxManifestBytes > 64*1024*1024 || input.MaxFileBytes < 1 || input.MaxFileBytes > 2*1024*1024 || input.MaxSourceBytes < 1 || input.MaxSourceBytes > 512*1024*1024 ||
		input.Inventory.Entries == nil || len(input.Inventory.Entries) > 100000 || input.Inventory.DeniedCount < 0 || input.Inventory.InvalidPathCount < 0 || input.Inventory.IgnoredCount < 0 {
		return LoaderPreflight{}, ErrLoaderInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return LoaderPreflight{}, ErrLoaderInput
	}
	if err := ctx.Err(); err != nil {
		return LoaderPreflight{}, err
	}
	var err error
	input, err = copyLoaderPreflight(input)
	if err != nil {
		return LoaderPreflight{}, err
	}
	manifest := input.Snapshot.Manifest()
	profileID, err := model.ProfileDigest(input.Profile, input.MaxManifestBytes)
	if err != nil || model.VerifyCaptureDigest(manifest, input.MaxManifestBytes) != nil || profileID != manifest.ProfileID || input.Profile.ProviderDigest != manifest.ProviderDigest {
		return LoaderPreflight{}, ErrInputMismatch
	}
	if err := input.AuthorizeScope(ctx, manifest.WorktreeID, profileID, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
		return LoaderPreflight{}, loaderAuthorityError(ctx)
	}
	result := LoaderPreflight{SnapshotDigest: manifest.SnapshotDigest, ProfileID: profileID, Status: "partial", Reasons: []string{}, Modules: []LoaderModule{}}
	reasons := map[string]bool{}
	if input.Profile.AnalysisMode != "typed_when_available" {
		reasons["SYNTAX_PROFILE"] = true
	}
	if input.Profile.CgoEnabled {
		reasons["CGO_UNVERIFIED"] = true
	}
	if input.Profile.ModuleMode != "readonly" {
		reasons["VENDOR_UNVERIFIED"] = true
	}
	needsDependencies := len(input.Profile.DependencyRoots) != 0 || len(manifest.DependencyInputDigests) != 0
	entries := make(map[string]model.ManifestFile, len(manifest.Files))
	var total int64
	goFiles := 0
	for _, item := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return LoaderPreflight{}, err
		}
		entries[item.Path] = item
		if policy.CheckPath(item.Path) != nil {
			return LoaderPreflight{}, ErrInputMismatch
		}
		if err := input.Scanner.CheckContent([]byte(item.Path)); err != nil {
			return LoaderPreflight{}, err
		}
		if err := input.AuthorizePath(ctx, manifest.WorktreeID, item.Path, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
			return LoaderPreflight{}, loaderAuthorityError(ctx)
		}
		if item.Status != "captured" {
			reasons["CAPTURE_INCOMPLETE"] = true
			continue
		}
		file, exists := input.Snapshot.File(item.Path)
		if !exists || item.ContentHash == nil || item.ByteLength == nil || file.ContentHash() != *item.ContentHash || int64(file.Len()) != *item.ByteLength {
			return LoaderPreflight{}, ErrInputMismatch
		}
		if file.Len() > input.MaxFileBytes || int64(file.Len()) > input.MaxSourceBytes-total {
			return LoaderPreflight{}, ErrSyntaxLimit
		}
		total += int64(file.Len())
		if err := input.Scanner.CheckContent(file.Bytes()); err != nil {
			return LoaderPreflight{}, err
		}
		if strings.HasSuffix(item.Path, ".go") {
			goFiles++
		}
	}
	if goFiles == 0 {
		reasons["GO_SOURCE_MISSING"] = true
	}
	seen := make(map[string]bool, len(input.Inventory.Entries))
	for _, item := range input.Inventory.Entries {
		if seen[item.Path] || model.ValidateRelativePath(item.Path, false) != nil {
			return LoaderPreflight{}, ErrLoaderInput
		}
		seen[item.Path] = true
		entry, exists := entries[item.Path]
		if !exists || item.Directory || item.State != "candidate" || entry.Status != "captured" {
			reasons["INVENTORY_INCOMPLETE"] = true
		}
	}
	if len(seen) != len(entries) {
		reasons["INVENTORY_INCOMPLETE"] = true
	}
	for _, root := range input.Profile.ModuleRoots {
		filename := path.Join(root, "go.mod")
		entry, exists := entries[filename]
		if !exists || entry.Status != "captured" {
			reasons["GO_MOD_MISSING"] = true
			continue
		}
		file, exists := input.Snapshot.File(filename)
		if !exists {
			return LoaderPreflight{}, ErrInputMismatch
		}
		parsed, err := modfile.Parse("go.mod", file.Bytes(), nil)
		if err != nil || parsed.Module == nil || module.CheckPath(parsed.Module.Mod.Path) != nil {
			reasons["GO_MOD_INVALID"] = true
			continue
		}
		if err := input.Scanner.CheckContent([]byte(parsed.Module.Mod.Path)); err != nil {
			return LoaderPreflight{}, err
		}
		result.Modules = append(result.Modules, LoaderModule{Root: root, ImportPath: parsed.Module.Mod.Path})
		if len(parsed.Replace) != 0 {
			reasons["GO_REPLACE_UNVERIFIED"] = true
		}
		if len(parsed.Require) != 0 || len(parsed.Tool) != 0 {
			reasons["GO_DEPENDENCIES_UNVERIFIED"] = true
		}
	}
	// Defensively copy profile and inventory before invoking proof callbacks.
	profileJSON, err := json.Marshal(input.Profile)
	if err != nil {
		return LoaderPreflight{}, ErrLoaderInput
	}
	decoded, err := model.DecodeEffectiveProfileJSON(profileJSON, input.MaxManifestBytes)
	if err != nil {
		return LoaderPreflight{}, ErrLoaderInput
	}
	profileCopy, ok := decoded.(*model.GoProfile)
	if !ok {
		return LoaderPreflight{}, ErrLoaderInput
	}
	inventoryCopy := input.Inventory
	inventoryCopy.Entries = append([]inventory.FileMapEntry(nil), input.Inventory.Entries...)
	for i := range inventoryCopy.Entries {
		inventoryCopy.Entries[i].Stages = append([]uint8(nil), input.Inventory.Entries[i].Stages...)
	}
	if input.VerifyClosure == nil {
		reasons["CLOSURE_UNVERIFIED"] = true
	} else {
		verified, err := input.VerifyClosure(ctx, input.Snapshot, inventoryCopy)
		if err != nil {
			if ctx.Err() != nil {
				return LoaderPreflight{}, ctx.Err()
			}
			return LoaderPreflight{}, loaderAuthorityError(ctx)
		}
		if !verified {
			reasons["CLOSURE_UNVERIFIED"] = true
		}
	}
	if input.VerifyToolchain == nil {
		reasons["TOOLCHAIN_UNVERIFIED"] = true
	} else {
		verified, err := input.VerifyToolchain(ctx, profileCopy)
		if err != nil {
			if ctx.Err() != nil {
				return LoaderPreflight{}, ctx.Err()
			}
			return LoaderPreflight{}, loaderAuthorityError(ctx)
		}
		if !verified {
			reasons["TOOLCHAIN_UNVERIFIED"] = true
		}
	}
	if needsDependencies {
		if input.VerifyDependencies == nil {
			reasons["DEPENDENCY_ROOTS_UNVERIFIED"] = true
		} else {
			// Each verifier receives an independent copy; a verifier cannot
			// accidentally change the profile observed by another verifier.
			decoded, err := model.DecodeEffectiveProfileJSON(profileJSON, input.MaxManifestBytes)
			if err != nil {
				return LoaderPreflight{}, ErrLoaderInput
			}
			dependencyProfile, ok := decoded.(*model.GoProfile)
			if !ok {
				return LoaderPreflight{}, ErrLoaderInput
			}
			verified, err := input.VerifyDependencies(ctx, dependencyProfile, append([]string(nil), manifest.DependencyInputDigests...))
			if err != nil {
				return LoaderPreflight{}, loaderAuthorityError(ctx)
			}
			if !verified {
				reasons["DEPENDENCY_ROOTS_UNVERIFIED"] = true
			}
		}
	}
	for _, item := range manifest.Files {
		if err := input.AuthorizePath(ctx, manifest.WorktreeID, item.Path, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
			return LoaderPreflight{}, loaderAuthorityError(ctx)
		}
	}
	if err := input.AuthorizeScope(ctx, manifest.WorktreeID, profileID, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
		return LoaderPreflight{}, loaderAuthorityError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return LoaderPreflight{}, err
	}
	for reason := range reasons {
		result.Reasons = append(result.Reasons, reason)
	}
	sort.Strings(result.Reasons)
	sort.Slice(result.Modules, func(i, j int) bool { return result.Modules[i].Root < result.Modules[j].Root })
	if len(result.Reasons) == 0 {
		result.Status = "eligible_for_guarded_load"
	}
	return result, nil
}
