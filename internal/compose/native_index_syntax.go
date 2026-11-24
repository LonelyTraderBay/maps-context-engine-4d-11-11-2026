//go:build linux || windows

package compose

import (
	"context"
	"crypto/rand"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"strings"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

func stageNativeFiles(ctx context.Context, store *index.Store, capture index.CaptureWrite, authorize index.GenerationAuthorizer) error {
	files := capture.Snapshot.Manifest().Files
	classes := make([]index.FileClassification, len(files))
	for j, file := range files {
		kind := "text"
		generated := false
		if strings.HasSuffix(file.Path, ".go") {
			kind = "go"
			if source, ok := capture.Snapshot.File(file.Path); ok {
				parsed, _ := parser.ParseFile(token.NewFileSet(), file.Path, source.Bytes(), parser.PackageClauseOnly|parser.ParseComments)
				generated = parsed != nil && ast.IsGenerated(parsed)
			}
		}
		classes[j] = index.FileClassification{Path: file.Path, Kind: kind, Generated: generated}
	}
	for offset := 0; offset < len(classes); offset += 256 {
		end := min(offset+256, len(classes))
		if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Offset: offset, Files: classes[offset:end]}); err != nil {
			return err
		}
	}
	if err := stageNativeSearch(ctx, store, capture, authorize, files); err != nil {
		return err
	}
	return nil
}

type nativeSyntaxStage struct {
	sequence           int64
	analyzed           map[string]string
	candidates         map[string]golanganalysis.TypedPackageIdentity
	unavailableModules int64
	ambiguousPackages  bool
}

func stageNativeGoSyntax(ctx context.Context, store *index.Store, write index.CaptureWrite, stream index.ProviderStageStream, binding catalog.Binding, publishContainment bool) (nativeSyntaxStage, error) {
	const maxFrameBytes = 16 << 20
	const maxFilesPerBatch = 32
	manifest := write.Snapshot.Manifest()
	batch := model.ProviderBatch{
		ProviderProtocol: 1, Type: "analyze_batch", JobID: write.JobID, Sequence: 1,
		Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{},
		Relations: []model.RelationFact{}, Diagnostics: []model.DiagnosticFact{},
	}
	filesInBatch := 0
	flush := func() error {
		if filesInBatch == 0 {
			return nil
		}
		if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: write, Stream: stream, Batch: batch, MaxFrameBytes: maxFrameBytes}); err != nil {
			return err
		}
		batch.Sequence++
		batch.Packages = batch.Packages[:0]
		batch.Symbols = batch.Symbols[:0]
		batch.Relations = batch.Relations[:0]
		batch.Diagnostics = batch.Diagnostics[:0]
		filesInBatch = 0
		return nil
	}
	packages := map[string]bool{}
	analyzed := map[string]string{}
	candidates := map[string]golanganalysis.TypedPackageIdentity{}
	ambiguousPackages := false
	modules := nativeSyntaxModules(write.Snapshot, binding.Profile)
	var unavailableModules int64
	for _, file := range manifest.Files {
		if file.Status != "captured" || !strings.HasSuffix(file.Path, ".go") || strings.HasSuffix(file.Path, "_test.go") && !binding.Profile.TestsEnabled {
			continue
		}
		location, err := nativeGoFileLocation(write.Snapshot, binding, modules, file.Path)
		if err != nil {
			return nativeSyntaxStage{}, err
		}
		if location.PackageKey == "" {
			unavailableModules++
			continue
		}
		directory, candidate := location.directory, location.TypedPackageIdentity
		if previous, exists := candidates[directory]; exists && previous != candidate {
			ambiguousPackages = true
		}
		candidates[directory] = candidate
		result, err := golanganalysis.AnalyzeSyntaxFile(ctx, golanganalysis.SyntaxInput{Snapshot: write.Snapshot, Profile: binding.Profile, Path: file.Path, PackageKey: location.PackageKey, ImportPath: location.ImportPath, ModuleKey: location.ModuleKey, PackageName: location.packageName, Variant: location.variant, Scanner: write.Scanner, MaxManifestBytes: 64 << 20, MaxFileBytes: 2 << 20, MaxNodes: 1000000, MaxDepth: 512, MaxFacts: 100000, MaxOutputBytes: 16 << 20})
		if err != nil {
			return nativeSyntaxStage{}, err
		}
		var relations []model.RelationFact
		if publishContainment {
			relations, err = golanganalysis.SyntaxContainment(ctx, result)
			if err != nil {
				return nativeSyntaxStage{}, err
			}
		}
		addResult := func(frame model.ProviderBatch) model.ProviderBatch {
			if !packages[location.PackageKey] {
				frame.Packages = append(frame.Packages, result.Package)
			}
			frame.Symbols = append(frame.Symbols, result.Symbols...)
			frame.Relations = append(frame.Relations, relations...)
			frame.Diagnostics = append(frame.Diagnostics, result.Diagnostics...)
			return frame
		}
		next := addResult(batch)
		if filesInBatch > 0 && !index.ProviderBatchWithinBudget(next, maxFrameBytes) {
			if err := flush(); err != nil {
				return nativeSyntaxStage{}, err
			}
			next = addResult(batch)
		}
		batch = next
		filesInBatch++
		packages[location.PackageKey] = true
		analyzed[file.Path] = result.ContentHash
		var failures []index.ParseFailure
		for _, d := range result.Diagnostics {
			if d.Code == "GO_PARSE_ERROR" {
				failures = append(failures, index.ParseFailure{Path: file.Path, DiagnosticID: d.DiagnosticID})
			}
		}
		// The diagnostic must be staged before the file can be marked.
		if filesInBatch == maxFilesPerBatch || len(failures) > 0 {
			if err := flush(); err != nil {
				return nativeSyntaxStage{}, err
			}
		}
		if len(failures) > 0 {
			if err := store.MarkParseErrors(ctx, index.MarkParseErrorsRequest{Capture: write, Failures: failures}); err != nil {
				return nativeSyntaxStage{}, err
			}
		}
	}
	if err := flush(); err != nil {
		return nativeSyntaxStage{}, err
	}
	return nativeSyntaxStage{batch.Sequence, analyzed, candidates, unavailableModules, ambiguousPackages}, nil
}

type nativeGoLocation struct {
	golanganalysis.TypedPackageIdentity
	directory, packageName, variant string
}

func nativeGoFileLocation(captured *snapshot.Snapshot, binding catalog.Binding, modules map[string]nativeSyntaxModule, name string) (nativeGoLocation, error) {
	source, _ := captured.File(name)
	parsed, _ := parser.ParseFile(token.NewFileSet(), name, source.Bytes(), parser.PackageClauseOnly)
	packageName := "invalid"
	if parsed != nil && parsed.Name != nil && token.IsIdentifier(parsed.Name.Name) {
		packageName = parsed.Name.Name
	}
	directory := path.Dir(name)
	moduleRoot, modulePath, ok := nativeSyntaxModuleFor(modules, directory)
	if !ok {
		return nativeGoLocation{}, nil
	}
	variant := "production"
	if strings.HasSuffix(name, "_test.go") {
		variant = "internal_test"
		if strings.HasSuffix(packageName, "_test") {
			packageName = strings.TrimSuffix(packageName, "_test")
			variant = "external_test"
		}
	}
	moduleKey, err := overview.GoModuleID(binding.WorktreeID, path.Join(moduleRoot, "go.mod"))
	if err != nil {
		return nativeGoLocation{}, err
	}
	parts := make([]model.CanonicalValue, 0, 5)
	for _, value := range []string{moduleKey, moduleRoot, directory, packageName, variant} {
		part, err := model.CanonicalString(value)
		if err != nil {
			return nativeGoLocation{}, err
		}
		parts = append(parts, part)
	}
	_, packageKey, err := model.EncodeCanonical("go-syntax-package-location-v1", model.CanonicalArray(parts), 32768)
	if err != nil {
		return nativeGoLocation{}, err
	}
	suffix := strings.TrimPrefix(strings.TrimPrefix(directory, moduleRoot), "/")
	if moduleRoot == "." {
		suffix = directory
	}
	importPath := modulePath
	if suffix != "" && suffix != "." {
		importPath += "/" + suffix
	}
	if variant == "external_test" {
		importPath += "_test"
	}
	candidate := golanganalysis.TypedPackageIdentity{ImportPath: importPath, PackageKey: packageKey, ModuleKey: moduleKey}
	return nativeGoLocation{TypedPackageIdentity: candidate, directory: directory, packageName: packageName, variant: variant}, nil
}

func stageNativeSearch(ctx context.Context, store *index.Store, capture index.CaptureWrite, authorize index.GenerationAuthorizer, files []model.ManifestFile) error {
	const maxBatchBytes = 16 << 20
	names := make([]string, 0, 256)
	var bytes int64
	flush := func() error {
		if len(names) == 0 {
			return nil
		}
		err := store.StageSearchDocuments(ctx, index.StageSearchDocumentsRequest{
			Capture: capture, AuthorizeGeneration: authorize, Paths: names, MaxBatchBytes: maxBatchBytes,
		})
		names, bytes = names[:0], 0
		return err
	}
	for _, file := range files {
		if file.Status != "captured" || file.ByteLength == nil {
			continue
		}
		// Account for both path copies, the display name and fixed identity.
		estimated := *file.ByteLength + int64(3*len(file.Path)+256)
		if len(names) == cap(names) || estimated > maxBatchBytes-bytes {
			if err := flush(); err != nil {
				return err
			}
		}
		names = append(names, file.Path)
		bytes += estimated
	}
	return flush()
}

type nativeSyntaxModule struct {
	importPath string
	configured bool
}

func nativeSyntaxModules(captured *snapshot.Snapshot, profile *model.GoProfile) map[string]nativeSyntaxModule {
	configured := map[string]bool{}
	for _, root := range profile.ModuleRoots {
		configured[root] = true
	}
	modules := map[string]nativeSyntaxModule{}
	for _, entry := range captured.Manifest().Files {
		if path.Base(entry.Path) != "go.mod" {
			continue
		}
		root := path.Dir(entry.Path)
		item := nativeSyntaxModule{configured: configured[root]}
		if entry.Status == "captured" {
			if file, ok := captured.File(entry.Path); ok {
				parsed, err := modfile.Parse(entry.Path, file.Bytes(), nil)
				if err == nil && parsed.Module != nil && module.CheckPath(parsed.Module.Mod.Path) == nil && len(parsed.Module.Mod.Path) <= 512 {
					item.importPath = parsed.Module.Mod.Path
				}
			}
		}
		modules[root] = item
	}
	return modules
}

func nativeSyntaxModuleFor(modules map[string]nativeSyntaxModule, directory string) (string, string, bool) {
	for current := directory; ; current = path.Dir(current) {
		if item, ok := modules[current]; ok {
			return current, item.importPath, item.configured && item.importPath != ""
		}
		if current == "." {
			return "", "", false
		}
	}
}

type NativeIndexProgress struct {
	GenerationID, SnapshotDigest string
	Files                        int
	Unchanged, Typed             bool
	ProviderID, ProviderStatus   string
}

func nativeTextPath(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".mod", ".sum", ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".xml", ".html", ".css", ".js", ".jsx", ".ts", ".tsx", ".sql", ".sh":
		return true
	}
	switch strings.ToLower(filepath.Base(name)) {
	case "readme", "license", "makefile", "dockerfile", "go.work", ".gitignore", ".gitattributes", ".editorconfig":
		return true
	}
	return false
}

func nativeOperationID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6], id[8] = id[6]&15|64, id[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}
