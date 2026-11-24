package compose

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

type windowsTypedGo struct {
	proof              *runtime.GoToolchainProof
	relations          []model.RelationFact
	selected, packages int
}

func (c windowsIndexCapture) analyzeTypedGo(ctx context.Context, frozen *snapshot.Snapshot, limit int64) (_ *windowsTypedGo, resultErr error) {
	b := c.scope.Binding
	p := b.Profile
	if p == nil || p.AnalysisMode != "typed_when_available" || c.scope.OpenGoScratch == nil || p.Goos != "windows" ||
		p.Goarch != "amd64" || p.CgoEnabled || p.TestsEnabled || p.ModuleMode != "readonly" ||
		!slices.Equal(p.ModuleRoots, []string{"."}) || len(p.DependencyRoots) != 0 || len(b.Policy.DependencyRoots) != 0 {
		return nil, nil
	}
	manifest := frozen.Manifest()
	if len(manifest.Files) == 0 || len(manifest.Files) > 128 {
		return nil, nil
	}
	modules := nativeSyntaxModules(frozen, p)
	candidates := map[string]golanganalysis.TypedPackageIdentity{}
	for _, item := range manifest.Files {
		if item.Status != "captured" || item.ByteLength == nil || *item.ByteLength > 64<<10 {
			return nil, nil
		}
		if !strings.HasSuffix(item.Path, ".go") || strings.HasSuffix(item.Path, "_test.go") {
			continue
		}
		location, err := nativeGoFileLocation(frozen, b, modules, item.Path)
		if err != nil {
			return nil, err
		}
		if previous, ok := candidates[location.directory]; location.PackageKey == "" || ok && previous != location.TypedPackageIdentity {
			return nil, nil
		}
		candidates[location.directory] = location.TypedPackageIdentity
	}
	if len(candidates) == 0 || len(candidates) > 32 {
		return nil, nil
	}
	goTool, ok := nativeRenameExecutable(b.Policy, "go.exe")
	if !ok || filepath.Base(filepath.Dir(goTool.Path)) != "bin" {
		return nil, nil
	}
	goRoot := filepath.Dir(filepath.Dir(goTool.Path))
	common, err := c.scope.CommonGit.CanonicalPath()
	if err != nil {
		return nil, err
	}
	for _, protected := range append(append([]string{}, c.scope.ApprovedRoots...), common) {
		if windowsPathsOverlap(goRoot, protected) {
			return nil, nil
		}
	}
	inv, err := c.inventory(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(manifest.DependencyInputDigests, []string{inv.InputDigest}) {
		return nil, errWindowsSnapshotChanged
	}
	if inv.DeniedCount != 0 || inv.IgnoredCount != 0 || inv.InvalidPathCount != 0 {
		return nil, nil
	}
	// Ordinary directories are bound in the inventory digest. A nested,
	// unreadable or sparse directory still prevents proving the closure.
	for _, item := range inv.Entries {
		if item.Directory && item.State != "candidate" {
			return nil, nil
		}
	}
	inv.Entries = slices.DeleteFunc(inv.Entries, func(item inventory.FileMapEntry) bool { return item.Directory })
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
	if proof.Digest() != p.ToolchainDigest {
		return nil, nil
	}
	scratch, err := c.scope.OpenGoScratch(ctx, frozen, limit)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, scratch.Close(ctx)) }()
	dirs := make([]string, 0, len(candidates))
	for dir := range candidates {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	result := &windowsTypedGo{proof: proof, packages: len(dirs)}
	relations := map[string]model.RelationFact{}
	err = withWindowsCapturedGo(ctx, b, frozen, inv, scratch, goRoot, goTool, proof, c.scanner, dirs,
		c.scope.Check, func(ctx context.Context) error { return c.revalidate(ctx, frozen) },
		golanganalysis.WindowsLoaderScratchVerifier(scratch.Root, scratch.Reader, scratch.Check),
		func(loader golanganalysis.CapturedLoaderInput) error {
			unit := candidates[loader.PackageDir]
			others := make([]golanganalysis.TypedPackageIdentity, 0, len(dirs)-1)
			for _, dir := range dirs {
				if dir != loader.PackageDir {
					others = append(others, candidates[dir])
				}
			}
			graph, err := golanganalysis.AnalyzeCapturedGraph(ctx, golanganalysis.GraphInput{
				MaxRelations: 10000, MaxOutputBytes: 8 << 20,
				Typed: golanganalysis.TypedInput{Loader: loader, PackageKey: unit.PackageKey, ModuleKey: unit.ModuleKey,
					OtherPackageCandidates: others, MaxNodes: 100000, MaxDepth: 512, MaxFacts: 10000,
					MaxOccurrences: 10000, MaxOutputBytes: 8 << 20},
			})
			if err != nil || !graph.Typed.Loader.BuildSelectionVerified {
				return err
			}
			if len(graph.Typed.Loader.Packages) != 1 || graph.Typed.Loader.Packages[0].ImportPath != unit.ImportPath {
				return index.ErrCaptureMismatch
			}
			result.selected++
			for _, relation := range graph.Relations {
				switch relation.Kind {
				case "CALLS_DIRECT", "CALLS_INTERFACE", "MAY_DISPATCH_TO", "UNRESOLVED_CALL":
				default:
					continue
				}
				if prior, exists := relations[relation.RelationID]; exists && !reflect.DeepEqual(prior, relation) {
					return index.ErrCaptureMismatch
				}
				relations[relation.RelationID] = relation
				if len(relations) > 10000 {
					return golanganalysis.ErrSyntaxLimit
				}
			}
			return nil
		})
	if errors.Is(err, golanganalysis.ErrSyntaxLimit) {
		return nil, nil
	}
	if err != nil || result.selected == 0 {
		return nil, err
	}
	for _, relation := range relations {
		result.relations = append(result.relations, relation)
	}
	sort.Slice(result.relations, func(a, b int) bool {
		return result.relations[a].RelationID < result.relations[b].RelationID
	})
	retained = true
	return result, nil
}
