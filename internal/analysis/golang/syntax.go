// Package golang analyzes only immutable, already-authorized captured inputs.
// It does not load packages, execute code, or assert typed semantic coverage.
package golang

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrInvalidInput  = errors.New("invalid Go syntax input")
	ErrInputMismatch = errors.New("Go syntax input does not match the captured scope")
	ErrSyntaxLimit   = errors.New("Go syntax analysis budget exceeded")
)

type SyntaxInput struct {
	Snapshot         *snapshot.Snapshot
	Profile          *model.GoProfile
	Path             string
	PackageKey       string
	ImportPath       string
	ModuleKey        string
	PackageName      string // production package name, without the external _test suffix
	Variant          string
	Scanner          *policy.Scanner
	Cache            *SyntaxCache // optional private parse cache; never stores typed facts
	MaxManifestBytes int
	MaxFileBytes     int
	MaxNodes         int
	MaxDepth         int
	MaxFacts         int
	MaxOutputBytes   int
}

type Scope struct {
	ID        string
	ParentID  string
	Kind      string
	Path      string
	StartByte int64
	EndByte   int64
}

type SyntaxImport struct {
	ImportPath string
	Alias      string // empty means that the imported name was not resolved
	Range      model.ByteRange
}

type SyntaxTag struct {
	SymbolID string
	Literal  string
	Range    model.ByteRange
}

// SyntaxOccurrence links variant-specific symbol IDs to the same physical
// declaration occurrence. It is not proof of identical typed semantics.
type SyntaxOccurrence struct {
	SymbolID     string
	OccurrenceID string
}

type SyntaxResult struct {
	Path                   string
	ContentHash            string
	SnapshotDigest         string
	Package                model.PackageFact
	Symbols                []model.SymbolFact
	Occurrences            []SyntaxOccurrence
	Scopes                 []Scope
	Imports                []SyntaxImport
	Tags                   []SyntaxTag
	Diagnostics            []model.DiagnosticFact
	Generated              *bool // nil if parsing could not establish the marker
	BuildSelectionVerified bool  // always false until the package loader proves selection
}

// AnalyzeSyntaxFile parses one captured Go file under an explicitly supplied
// package variant. Package membership/build selection and typed bindings are
// not inferred or certified here. Errors and cancellation return no facts.
func AnalyzeSyntaxFile(ctx context.Context, input SyntaxInput) (SyntaxResult, error) {
	if ctx == nil || input.Snapshot == nil || input.Profile == nil || input.Scanner == nil || input.MaxManifestBytes < 1 || input.MaxManifestBytes > 64*1024*1024 || input.MaxFileBytes < 1 || input.MaxFileBytes > 2*1024*1024 || input.MaxNodes < 1 || input.MaxNodes > 1000000 || input.MaxDepth < 1 || input.MaxDepth > 512 || input.MaxFacts < 1 || input.MaxFacts > 100000 || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 16*1024*1024 || !strings.HasSuffix(input.Path, ".go") || !token.IsIdentifier(input.PackageName) || input.PackageName == "_" {
		return SyntaxResult{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return SyntaxResult{}, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return SyntaxResult{}, ErrInvalidInput
	}
	manifest := input.Snapshot.Manifest()
	if model.VerifyCaptureDigest(manifest, input.MaxManifestBytes) != nil {
		return SyntaxResult{}, ErrInputMismatch
	}
	profileID, err := model.ProfileDigest(input.Profile, input.MaxManifestBytes)
	if err != nil || profileID != manifest.ProfileID || input.Profile.ProviderDigest != manifest.ProviderDigest {
		return SyntaxResult{}, ErrInputMismatch
	}
	file, ok := input.Snapshot.File(input.Path)
	if !ok {
		return SyntaxResult{}, ErrInputMismatch
	}
	if file.Len() > input.MaxFileBytes {
		return SyntaxResult{}, ErrSyntaxLimit
	}
	source := file.Bytes()
	if err := input.Scanner.CheckContent([]byte(input.Path)); err != nil {
		return SyntaxResult{}, err
	}
	if err := input.Scanner.CheckContent(source); err != nil {
		return SyntaxResult{}, err
	}
	pkg := model.PackageFact{PackageKey: input.PackageKey, ImportPath: input.ImportPath, ModuleKey: input.ModuleKey, Variant: input.Variant, InputDigest: manifest.SnapshotDigest, SemanticStatus: "partial"}
	raw, err := json.Marshal(pkg)
	if err != nil {
		return SyntaxResult{}, err
	}
	if _, err := model.DecodePackageFactJSON(raw, 65536); err != nil {
		return SyntaxResult{}, ErrInvalidInput
	}
	for _, text := range []string{input.PackageKey, input.ImportPath, input.ModuleKey, input.PackageName} {
		if err := input.Scanner.CheckContent([]byte(text)); err != nil {
			return SyntaxResult{}, err
		}
	}
	isTest := strings.HasSuffix(path.Base(input.Path), "_test.go")
	switch input.Variant {
	case "production":
		if isTest {
			return SyntaxResult{}, ErrInputMismatch
		}
	case "internal_test":
		if !input.Profile.TestsEnabled {
			return SyntaxResult{}, ErrInputMismatch
		}
	case "external_test":
		if !input.Profile.TestsEnabled || !isTest {
			return SyntaxResult{}, ErrInputMismatch
		}
	default:
		return SyntaxResult{}, ErrInvalidInput
	}
	key := syntaxCacheKey{worktree: manifest.WorktreeID, profile: manifest.ProfileID, policy: manifest.PolicyDigest, provider: input.Profile.ProviderDigest, toolchain: input.Profile.ToolchainDigest, path: input.Path, content: file.ContentHash(), options: "go-parser-comments-v1"}
	var files *token.FileSet
	var parsed *ast.File
	var parseErr error
	hit := false
	var cacheEpoch uint64
	if input.Cache != nil {
		files, parsed, hit, cacheEpoch = input.Cache.lookup(key, source)
	}
	if !hit {
		files = token.NewFileSet()
		parsed, parseErr = parser.ParseFile(files, input.Path, source, parser.ParseComments)
	}
	if err := ctx.Err(); err != nil {
		return SyntaxResult{}, err
	}
	expectedName := input.PackageName
	if input.Variant == "external_test" {
		expectedName += "_test"
	}
	if parsed != nil && parsed.Name != nil && parsed.Name.Name != expectedName {
		return SyntaxResult{}, ErrInputMismatch
	}
	result := SyntaxResult{Path: input.Path, ContentHash: file.ContentHash(), SnapshotDigest: manifest.SnapshotDigest, Package: pkg, Symbols: []model.SymbolFact{}, Occurrences: []SyntaxOccurrence{}, Scopes: []Scope{}, Imports: []SyntaxImport{}, Tags: []SyntaxTag{}, Diagnostics: []model.DiagnosticFact{}}
	if parseErr != nil {
		id, err := identityHash("go-diagnostic-v1", input.PackageKey, input.Variant, input.Path, "GO_PARSE_ERROR", "", 0, 0)
		if err != nil {
			return SyntaxResult{}, err
		}
		filePath, packageKey := input.Path, input.PackageKey
		result.Diagnostics = append(result.Diagnostics, model.DiagnosticFact{DiagnosticID: id, Path: &filePath, PackageKey: &packageKey, Code: "GO_PARSE_ERROR", Severity: "error", Message: "Go source could not be parsed.", ScopeDescription: "captured file"})
		raw, err := json.Marshal(result)
		if err != nil {
			return SyntaxResult{}, err
		}
		if len(raw) > input.MaxOutputBytes {
			return SyntaxResult{}, ErrSyntaxLimit
		}
		if err := ctx.Err(); err != nil {
			return SyntaxResult{}, err
		}
		return result, nil
	}
	if parsed == nil || parsed.Name == nil || parsed.Name.Name != expectedName || !utf8.Valid(source) {
		return SyntaxResult{}, ErrInputMismatch
	}
	generated := ast.IsGenerated(parsed)
	result.Generated = &generated
	result, err = collectSyntax(ctx, input, files, parsed, source, result)
	if err != nil {
		return SyntaxResult{}, err
	}
	if input.Cache != nil && !hit {
		if err := input.Cache.remember(ctx, key, source, files, parsed, input.MaxNodes, cacheEpoch); err != nil {
			return SyntaxResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return SyntaxResult{}, err
	}
	return result, nil
}
