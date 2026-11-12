package golang

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/mod/module"
	"golang.org/x/tools/go/packages"
)

type CapturedLoaderInput struct {
	Preflight     LoaderPreflightInput
	GoRoot        string
	ScratchRoot   string
	WorkspaceRoot string
	PackageDir    string // capture-relative package directory; empty means "."
	Tool          *runtimeadapter.GoExecutable
	Stdlib        *TrustedStdlib
	// VerifyScratch checks authorized, owned, confined directories and that
	// workspace membership cannot add inputs outside the supplied capture.
	VerifyScratch  func(context.Context, string, string, *snapshot.Snapshot) (bool, error)
	MaxOutputBytes int
	// consumeTypes is package-private: compiler objects are consumed within
	// this one load and are never exposed to another load or external caller.
	consumeTypes func(context.Context, *packages.Package, *CapturedOverlay, map[*packages.Package][]string) error
}

type LoadedPackage struct {
	ImportPath string   `json:"import_path"`
	Name       string   `json:"name"`
	Files      []string `json:"files"`
}

type CapturedLoaderResult struct {
	SnapshotDigest         string          `json:"snapshot_digest"`
	Status                 string          `json:"status"`
	Reasons                []string        `json:"reasons"`
	Packages               []LoadedPackage `json:"packages"`
	BuildSelectionVerified bool            `json:"build_selection_verified"`
}

// LoadCapturedPackages is a narrowly bounded, local-module package-selection
// primitive. The supplied verifiers must establish real toolchain, scratch and
// capture closure authority; this method does not provide an OS sandbox.
// Unapproved imports, cgo, tests, multi-module workspaces and unavailable
// proof explicitly fall back to partial. Typed compiler objects stay here.
func LoadCapturedPackages(ctx context.Context, input CapturedLoaderInput) (CapturedLoaderResult, error) {
	if ctx == nil || input.Preflight.Profile == nil || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 1024*1024 {
		return CapturedLoaderResult{}, ErrLoaderInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return CapturedLoaderResult{}, ErrLoaderInput
	}
	if err := ctx.Err(); err != nil {
		return CapturedLoaderResult{}, err
	}
	if input.Preflight.MaxFileBytes > 64*1024 || input.Preflight.MaxSourceBytes > 2*1024*1024 || input.Preflight.MaxManifestBytes > 1024*1024 {
		return CapturedLoaderResult{}, ErrLoaderInput
	}
	packageDir := input.PackageDir
	if packageDir == "" {
		packageDir = "."
	}
	if model.ValidateRelativePath(packageDir, true) != nil {
		return CapturedLoaderResult{}, ErrLoaderInput
	}
	var err error
	input.Preflight, err = copyLoaderPreflight(input.Preflight)
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	plan, err := PreflightLoader(ctx, input.Preflight)
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	result := CapturedLoaderResult{SnapshotDigest: plan.SnapshotDigest, Status: "partial", Reasons: append([]string(nil), plan.Reasons...), Packages: []LoadedPackage{}}
	manifest := input.Preflight.Snapshot.Manifest()
	recheckScope := func() error {
		if err := input.Preflight.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, input.Preflight.PolicyEpoch); err != nil {
			return loaderAuthorityError(ctx)
		}
		return ctx.Err()
	}
	partial := func(reason string) (CapturedLoaderResult, error) {
		if err := recheckScope(); err != nil {
			return CapturedLoaderResult{}, err
		}
		result.Reasons = append(result.Reasons, reason)
		sort.Strings(result.Reasons)
		return boundedLoaderResult(result, input.MaxOutputBytes)
	}
	if plan.Status != "eligible_for_guarded_load" {
		if err := recheckScope(); err != nil {
			return CapturedLoaderResult{}, err
		}
		return boundedLoaderResult(result, input.MaxOutputBytes)
	}
	if input.Tool == nil {
		return partial("TOOLCHAIN_UNVERIFIED")
	}
	profile := input.Preflight.Profile
	if profile.TestsEnabled {
		return partial("GO_TEST_VARIANTS_UNVERIFIED")
	}
	if len(profile.ModuleRoots) != 1 || profile.ModuleRoots[0] != "." {
		return partial("MULTI_MODULE_UNVERIFIED")
	}
	if len(manifest.Files) > 128 {
		return partial("LOADER_FILE_LIMIT")
	}
	if len(plan.Modules) != 1 {
		return partial("GO_MOD_INVALID")
	}
	modulePath := plan.Modules[0].ImportPath
	expectedPackage := modulePath
	if packageDir != "." {
		expectedPackage += "/" + packageDir
		if module.CheckImportPath(expectedPackage) != nil {
			return partial("GO_PACKAGE_UNVERIFIED")
		}
	}
	if input.Stdlib != nil {
		ok, err := input.Stdlib.Check(ctx, profile, input.Preflight.Snapshot, input.GoRoot)
		if err != nil {
			return CapturedLoaderResult{}, err
		}
		if !ok {
			return partial("STDLIB_UNVERIFIED")
		}
	}
	availableDirs := map[string][]model.ManifestFile{}
	for _, item := range manifest.Files {
		if strings.HasSuffix(item.Path, ".go") && !strings.HasSuffix(item.Path, "_test.go") {
			dir := path.Dir(item.Path)
			availableDirs[dir] = append(availableDirs[dir], item)
		}
	}
	if len(availableDirs[packageDir]) == 0 {
		return partial("GO_SOURCE_MISSING")
	}
	// Inspect only the selected package and its captured local import closure.
	// Inactive production files remain conservative inputs; tests are disabled.
	pending := []string{packageDir}
	seenDirs := map[string]bool{}
	queuedDirs := map[string]bool{packageDir: true}
	for len(pending) > 0 {
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true
		for _, item := range availableDirs[dir] {
			if err := ctx.Err(); err != nil {
				return CapturedLoaderResult{}, err
			}
			file, ok := input.Preflight.Snapshot.File(item.Path)
			if !ok {
				return CapturedLoaderResult{}, ErrInputMismatch
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), item.Path, file.Bytes(), parser.ImportsOnly)
			if err != nil {
				return partial("GO_IMPORTS_UNVERIFIED")
			}
			for _, declaration := range parsed.Imports {
				name, err := strconv.Unquote(declaration.Path.Value)
				if err != nil || module.CheckImportPath(name) != nil {
					return partial("GO_IMPORTS_UNVERIFIED")
				}
				if err := input.Preflight.Scanner.CheckContent([]byte(name)); err != nil {
					return CapturedLoaderResult{}, err
				}
				if name == modulePath || strings.HasPrefix(name, modulePath+"/") {
					relative := "."
					if name != modulePath {
						relative = strings.TrimPrefix(name, modulePath+"/")
					}
					if len(availableDirs[relative]) == 0 {
						return partial("GO_LOCAL_DEPENDENCY_MISSING")
					}
					if !queuedDirs[relative] {
						pending = append(pending, relative)
						queuedDirs[relative] = true
					}
				} else if input.Stdlib == nil || !input.Stdlib.AllowsPackage(name) {
					return partial("GO_IMPORTS_UNVERIFIED")
				}
			}
		}
	}
	if input.VerifyScratch == nil {
		return partial("SCRATCH_UNVERIFIED")
	}
	checkScratch := func() (bool, error) {
		ok, err := input.VerifyScratch(ctx, input.ScratchRoot, input.WorkspaceRoot, input.Preflight.Snapshot)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, ErrLoaderAuthority
		}
		return ok, nil
	}
	ok, err := checkScratch()
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	if !ok {
		return partial("SCRATCH_UNVERIFIED")
	}
	env, flags, err := LoaderEnvironment(profile, input.GoRoot, input.ScratchRoot, input.WorkspaceRoot)
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	overlay, err := NewCapturedOverlay(ctx, CapturedOverlayInput{Snapshot: input.Preflight.Snapshot, Profile: profile, SyntheticRoot: input.WorkspaceRoot, PolicyEpoch: input.Preflight.PolicyEpoch, Scanner: input.Preflight.Scanner, AuthorizeScope: input.Preflight.AuthorizeScope, AuthorizePath: input.Preflight.AuthorizePath, MaxManifestBytes: input.Preflight.MaxManifestBytes, MaxFileBytes: input.Preflight.MaxFileBytes, MaxOverlayBytes: 4 * 1024 * 1024})
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	bytes, err := overlay.Overlay()
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	if err := input.Tool.CheckForLoad(ctx, input.GoRoot); err != nil {
		if ctx.Err() != nil {
			return CapturedLoaderResult{}, ctx.Err()
		}
		return partial("TOOLCHAIN_UNVERIFIED")
	}
	if input.Stdlib != nil && !input.Stdlib.begin() {
		return partial("STDLIB_SCOPE_REUSED")
	}
	parseFile := boundedLoaderParser(ctx, runtimeadapter.GoWorkerLimit(), func(fset *token.FileSet, filename string, source []byte) (*ast.File, error) {
		if _, ok := overlay.files[filename]; ok {
			return overlay.ParseFile(fset, filename, source)
		}
		if input.Stdlib != nil {
			return input.Stdlib.parseFile(ctx, fset, filename, source)
		}
		return nil, ErrInputMismatch
	})
	packageDirectory := filepath.Join(input.WorkspaceRoot, filepath.FromSlash(packageDir))
	loaded, loadErr := packages.Load(&packages.Config{Context: ctx, Dir: packageDirectory, Env: env, BuildFlags: flags, Tests: false, Overlay: bytes, ParseFile: parseFile, Logf: func(string, ...any) {},
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}, ".")
	if err := ctx.Err(); err != nil {
		return CapturedLoaderResult{}, err
	}
	if err := input.Tool.CheckForLoad(ctx, input.GoRoot); err != nil {
		if ctx.Err() != nil {
			return CapturedLoaderResult{}, ctx.Err()
		}
		return partial("TOOLCHAIN_CHANGED")
	}
	ok, err = checkScratch()
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	if !ok {
		return partial("SCRATCH_CHANGED")
	}
	if input.Stdlib != nil {
		ok, err := input.Stdlib.Check(ctx, profile, input.Preflight.Snapshot, input.GoRoot)
		if err != nil {
			return CapturedLoaderResult{}, err
		}
		if !ok {
			return partial("STDLIB_CHANGED")
		}
	}
	post, err := PreflightLoader(ctx, input.Preflight)
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	if post.Status != "eligible_for_guarded_load" || post.SnapshotDigest != plan.SnapshotDigest || post.ProfileID != plan.ProfileID {
		return partial("CLOSURE_CHANGED")
	}
	if loadErr != nil || len(loaded) != 1 || len(loaded[0].Errors) != 0 || loaded[0].Types == nil || loaded[0].TypesInfo == nil {
		return partial("GO_PACKAGE_LOAD_PARTIAL")
	}
	pkg := loaded[0]
	if pkg.PkgPath != expectedPackage {
		return partial("GO_PACKAGE_LOAD_PARTIAL")
	}
	files, ok := validateLoadedGraph(pkg, modulePath, overlay, input.Stdlib, input.Preflight.Scanner)
	if !ok {
		return partial("GO_PACKAGE_LOAD_PARTIAL")
	}
	if input.consumeTypes != nil {
		if err := input.consumeTypes(ctx, pkg, overlay, files); err != nil {
			return CapturedLoaderResult{}, err
		}
		// Extraction may take time. Recheck every authority after it before
		// the caller can release any facts derived from compiler objects.
		if err := input.Tool.CheckForLoad(ctx, input.GoRoot); err != nil {
			if ctx.Err() != nil {
				return CapturedLoaderResult{}, ctx.Err()
			}
			return partial("TOOLCHAIN_CHANGED")
		}
		ok, err := checkScratch()
		if err != nil {
			return CapturedLoaderResult{}, err
		}
		if !ok {
			return partial("SCRATCH_CHANGED")
		}
		if input.Stdlib != nil {
			ok, err := input.Stdlib.Check(ctx, profile, input.Preflight.Snapshot, input.GoRoot)
			if err != nil {
				return CapturedLoaderResult{}, err
			}
			if !ok {
				return partial("STDLIB_CHANGED")
			}
		}
		post, err := PreflightLoader(ctx, input.Preflight)
		if err != nil {
			return CapturedLoaderResult{}, err
		}
		if post.Status != "eligible_for_guarded_load" || post.SnapshotDigest != plan.SnapshotDigest || post.ProfileID != plan.ProfileID {
			return partial("CLOSURE_CHANGED")
		}
	}
	selected := LoadedPackage{ImportPath: pkg.PkgPath, Name: pkg.Name, Files: files[pkg]}
	for _, filename := range selected.Files {
		if err := input.Preflight.AuthorizePath(ctx, manifest.WorktreeID, filename, manifest.PolicyDigest, input.Preflight.PolicyEpoch); err != nil {
			return CapturedLoaderResult{}, loaderAuthorityError(ctx)
		}
	}
	if err := recheckScope(); err != nil {
		return CapturedLoaderResult{}, err
	}
	result.Packages = append(result.Packages, selected)
	result.BuildSelectionVerified = true
	if input.consumeTypes == nil {
		result.Reasons = []string{"TYPED_FACT_EXTRACTION_PENDING"}
	} else {
		result.Reasons = []string{"SEMANTIC_RELATIONS_PENDING"}
	}
	return boundedLoaderResult(result, input.MaxOutputBytes)
}

// The loader uses one shared limit across captured and stdlib parser calls.
// A canceled waiter never starts another parse or publishes its result.
func boundedLoaderParser(ctx context.Context, workers int, parse func(*token.FileSet, string, []byte) (*ast.File, error)) func(*token.FileSet, string, []byte) (*ast.File, error) {
	if ctx == nil || workers < 1 || workers > 4 || parse == nil {
		return func(*token.FileSet, string, []byte) (*ast.File, error) { return nil, ErrLoaderInput }
	}
	slots := make(chan struct{}, workers)
	return func(fset *token.FileSet, filename string, source []byte) (*ast.File, error) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parsed, err := parse(fset, filename, source)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return parsed, err
	}
}

func validateLoadedGraph(root *packages.Package, modulePath string, overlay *CapturedOverlay, stdlib *TrustedStdlib, scanner interface{ CheckContent([]byte) error }) (map[*packages.Package][]string, bool) {
	files := make(map[*packages.Package][]string)
	ids := make(map[string]*packages.Package)
	stack := []*packages.Package{root}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == nil || len(ids) >= 1024 {
			return nil, false
		}
		if prior, exists := ids[current.ID]; exists {
			if prior != current {
				return nil, false
			}
			continue
		}
		local := current.PkgPath == modulePath || strings.HasPrefix(current.PkgPath, modulePath+"/")
		standard := !local && stdlib != nil && stdlib.AllowsPackage(current.PkgPath)
		if current.ID == "" || !local && !standard || current.Name == "" || len(current.Errors) != 0 || current.Types == nil {
			return nil, false
		}
		maxFiles := 128
		if standard {
			maxFiles = 512
		}
		if current.PkgPath != "unsafe" && (current.TypesInfo == nil || len(current.CompiledGoFiles) == 0 || len(current.CompiledGoFiles) > maxFiles || len(current.Syntax) != len(current.CompiledGoFiles)) {
			return nil, false
		}
		if current.PkgPath == "unsafe" && (len(current.CompiledGoFiles) != 0 || len(current.Syntax) != 0 || len(current.Imports) != 0) {
			return nil, false
		}
		for _, text := range []string{current.PkgPath, current.Name} {
			if scanner.CheckContent([]byte(text)) != nil {
				return nil, false
			}
		}
		ids[current.ID] = current
		seen := map[string]bool{}
		for _, absolute := range current.CompiledGoFiles {
			if filepath.Clean(absolute) != absolute {
				return nil, false
			}
			if standard {
				if !stdlib.fileVerified(absolute) || seen[absolute] {
					return nil, false
				}
				seen[absolute] = true
				continue
			}
			relative, exists := overlay.files[absolute]
			if !exists || !strings.HasSuffix(relative, ".go") || seen[relative] {
				return nil, false
			}
			seen[relative] = true
			files[current] = append(files[current], relative)
		}
		sort.Strings(files[current])
		keys := make([]string, 0, len(current.Imports))
		for name := range current.Imports {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			if name != modulePath && !strings.HasPrefix(name, modulePath+"/") && (stdlib == nil || !stdlib.AllowsPackage(name)) {
				return nil, false
			}
			if current.Imports[name] == nil || current.Imports[name].PkgPath != name {
				return nil, false
			}
			stack = append(stack, current.Imports[name])
		}
	}
	return files, true
}

func boundedLoaderResult(result CapturedLoaderResult, max int) (CapturedLoaderResult, error) {
	if result.Reasons == nil {
		result.Reasons = []string{}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return CapturedLoaderResult{}, err
	}
	if len(data) > max {
		return CapturedLoaderResult{}, ErrSyntaxLimit
	}
	return result, nil
}
