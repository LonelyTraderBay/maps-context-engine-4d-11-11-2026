//go:build linux

package compose

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// The ordinary syntax publication remains the fallback. Typed relations are
// admitted only inside the bounded local closure proved by the same captured
// loader used by native rename; the compiler never sees the target path.
type nativeTypedIndex struct {
	relations []model.RelationFact
	selected  int
	proof     *runtime.GoToolchainProof
	program   *flowview.Program
}

type nativeFlowLoad struct {
	directory string
	member    model.ScopeMember
}

func (i *nativeInventory) typedIndex(ctx context.Context, captured nativeCapture, available map[string]golanganalysis.TypedPackageIdentity) (*nativeTypedIndex, error) {
	return i.loadTypedIndex(ctx, captured, available, nil)
}

func (i *nativeInventory) loadTypedIndex(ctx context.Context, captured nativeCapture, available map[string]golanganalysis.TypedPackageIdentity, flow *nativeFlowLoad) (_ *nativeTypedIndex, resultErr error) {
	n := i.scope
	profile := n.binding.Profile
	manifest := captured.snapshot.Manifest()
	files := captured.files
	if profile.AnalysisMode != "typed_when_available" || profile.CgoEnabled || profile.TestsEnabled ||
		len(profile.ModuleRoots) != 1 || profile.ModuleRoots[0] != "." || profile.ModuleMode != "readonly" ||
		len(profile.DependencyRoots) != 0 || len(n.binding.Policy.DependencyRoots) != 0 ||
		len(available) == 0 || len(available) > 32 || len(manifest.Files) > 128 ||
		files.IgnoredCount != 0 || files.DeniedCount != 0 || files.InvalidPathCount != 0 ||
		len(manifest.DependencyInputDigests) != 1 || manifest.DependencyInputDigests[0] != files.InputDigest {
		return nil, nil
	}
	var sourceBytes int64
	for _, item := range manifest.Files {
		if item.Status != "captured" || item.ByteLength == nil || *item.ByteLength > 64<<10 ||
			strings.HasSuffix(item.Path, "_test.go") {
			return nil, nil
		}
		sourceBytes += *item.ByteLength
		if sourceBytes > 2<<20 {
			return nil, nil
		}
	}
	goBinary, ok := nativeRenameExecutable(n.binding.Policy, "go")
	if !ok || filepath.Base(filepath.Dir(goBinary.Path)) != "bin" || runtime.CheckPrivateToolUmask(ctx) != nil {
		return nil, ctx.Err()
	}
	goRoot := filepath.Dir(filepath.Dir(goBinary.Path))
	proof, err := runtime.InspectGoToolchain(ctx, goRoot)
	if err != nil {
		return nil, ctx.Err()
	}
	retained := false
	defer func() {
		if !retained || resultErr != nil {
			resultErr = errors.Join(resultErr, proof.Close())
		}
	}()
	if proof.Digest() != profile.ToolchainDigest {
		return nil, nil
	}
	root, err := paths.OpenRoot(goRoot)
	if err != nil {
		return nil, err
	}
	protected := append(append([]*paths.Root{}, n.state.TargetRoots()...), n.state.StateRoot())
	if err := errors.Join(runtime.CheckCleanupRoot(root, protected), root.Close()); err != nil {
		return nil, err
	}
	scratch, err := newNativeRenameScratch(ctx, n, captured.snapshot, goRoot)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, scratch.Close(ctx)) }()
	bin, err := paths.OpenRoot(filepath.Join(goRoot, "bin"))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, bin.Close()) }()
	tool, err := runtime.OpenGoExecutable(ctx, runtime.GoExecutableInput{BinRoot: bin, ExpectedSHA256: goBinary.SHA256, MaxBytes: 128 << 20,
		Authorize: func(ctx context.Context) error { return proof.CheckPath(ctx, "bin/go") }})
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, tool.Close()) }()
	stdlibRoot, err := paths.OpenRoot(filepath.Join(goRoot, "src"))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, stdlibRoot.Close()) }()
	allowed := proof.StandardPackages()
	scanner := i.request.Scanner
	stdlib, err := golanganalysis.NewTrustedStdlib(ctx, golanganalysis.TrustedStdlibInput{SourceRoot: stdlibRoot, GoRoot: goRoot, Profile: profile, Snapshot: captured.snapshot, DependencyDigest: files.InputDigest, AllowedPackages: allowed, Scanner: scanner, MaxManifestBytes: 1 << 20, MaxFileBytes: 2 << 20, MaxFiles: 8192, MaxSourceBytes: 64 << 20,
		AuthorizePath: func(ctx context.Context, name string) error {
			if model.ValidateRelativePath(name, false) != nil {
				return ErrAuthority
			}
			return proof.CheckPath(ctx, "src/"+name)
		},
		VerifyAuthority: func(ctx context.Context, root, toolchain, dependency string, packages []string) (bool, error) {
			if root != filepath.Join(goRoot, "src") || toolchain != profile.ToolchainDigest || dependency != files.InputDigest || !reflect.DeepEqual(packages, allowed) {
				return false, nil
			}
			err := proof.Check(ctx)
			return err == nil, err
		}})
	if err != nil {
		return nil, err
	}
	authorizeScope := func(ctx context.Context, w, p, d string, e int64) error {
		if w != n.binding.WorktreeID || p != n.binding.ProfileID || d != n.binding.PolicyDigest || e != n.binding.PolicyEpoch {
			return ErrScopeChanged
		}
		return n.recheckBinding(ctx)
	}
	preflight := golanganalysis.LoaderPreflightInput{Snapshot: captured.snapshot, Profile: profile, Inventory: files, PolicyEpoch: n.binding.PolicyEpoch,
		Scanner: scanner, AuthorizeScope: authorizeScope, MaxManifestBytes: 1 << 20, MaxFileBytes: 64 << 10, MaxSourceBytes: 2 << 20,
		AuthorizePath: func(ctx context.Context, w, name, d string, e int64) error {
			if w != n.binding.WorktreeID || d != n.binding.PolicyDigest || e != n.binding.PolicyEpoch ||
				policy.CheckConfiguredPath(name, n.binding.Policy.DeniedGlobs) != nil {
				return ErrAuthority
			}
			return n.boundary(ctx)
		},
		VerifyClosure: func(ctx context.Context, candidate *snapshot.Snapshot, observed inventory.FileMap) (bool, error) {
			if candidate.Manifest().SnapshotDigest != manifest.SnapshotDigest || !sameNativeLoaderInventory(observed, files) {
				return false, nil
			}
			err := errors.Join(scratch.Check(ctx), i.observeCaptured(ctx, captured, candidate, false))
			return err == nil, err
		},
		VerifyToolchain: golanganalysis.LoaderToolchainVerifier(tool, goRoot, func(ctx context.Context, candidate *model.GoProfile) (bool, error) {
			if !reflect.DeepEqual(candidate, profile) {
				return false, nil
			}
			err := proof.Check(ctx)
			return err == nil, err
		}),
		VerifyDependencies: func(ctx context.Context, candidate *model.GoProfile, digests []string) (bool, error) {
			if !reflect.DeepEqual(candidate, profile) || !reflect.DeepEqual(digests, manifest.DependencyInputDigests) {
				return false, nil
			}
			err := proof.Check(ctx)
			return err == nil, err
		}}
	loader := golanganalysis.CapturedLoaderInput{Preflight: preflight, GoRoot: goRoot, ScratchRoot: scratch.path, WorkspaceRoot: filepath.Join(scratch.path, "input"), Tool: tool, Stdlib: stdlib, MaxOutputBytes: 65536,
		VerifyScratch: golanganalysis.LoaderScratchVerifier(scratch.root, scratch.Check)}
	dirs := make([]string, 0, len(available))
	for dir := range available {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	result := &nativeTypedIndex{proof: proof}
	byID := map[string]model.RelationFact{}
	for _, dir := range dirs {
		if flow != nil && dir != flow.directory {
			continue
		}
		unit := available[dir]
		others := make([]golanganalysis.TypedPackageIdentity, 0, len(dirs)-1)
		for _, other := range dirs {
			if other != dir {
				others = append(others, available[other])
			}
		}
		loader.PackageDir = dir
		input := golanganalysis.GraphInput{MaxRelations: 10000, MaxOutputBytes: 8 << 20, Typed: golanganalysis.TypedInput{
			Loader: loader, PackageKey: unit.PackageKey, ModuleKey: unit.ModuleKey, OtherPackageCandidates: others, MaxNodes: 100000, MaxDepth: 512, MaxFacts: 10000, MaxOccurrences: 10000, MaxOutputBytes: 8 << 20}}
		if flow != nil {
			program, err := flowview.Load(ctx, flowview.Input{Graph: input, Member: flow.member, ServeMuxModern: true})
			if err != nil {
				return nil, err
			}
			result.program = program
			retained = true
			return result, nil
		}
		graph, err := golanganalysis.AnalyzeCapturedGraph(ctx, input)
		if errors.Is(err, golanganalysis.ErrSyntaxLimit) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !graph.Typed.Loader.BuildSelectionVerified {
			continue
		}
		if len(graph.Typed.Loader.Packages) != 1 || graph.Typed.Loader.Packages[0].ImportPath != unit.ImportPath {
			return nil, index.ErrCaptureMismatch
		}
		result.selected++
		for _, relation := range graph.Relations {
			if prior, ok := byID[relation.RelationID]; ok && !reflect.DeepEqual(prior, relation) {
				return nil, index.ErrCaptureMismatch
			}
			byID[relation.RelationID] = relation
			if len(byID) > 10000 {
				return nil, nil
			}
		}
	}
	if result.selected == 0 {
		return nil, nil
	}
	for _, relation := range byID {
		result.relations = append(result.relations, relation)
	}
	sort.Slice(result.relations, func(a, b int) bool {
		return result.relations[a].RelationID < result.relations[b].RelationID
	})
	retained = true
	return result, nil
}
