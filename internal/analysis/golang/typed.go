package golang

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"golang.org/x/tools/go/packages"
)

type TypedInput struct {
	Loader        CapturedLoaderInput
	PackageKey    string
	ModuleKey     string
	OtherPackages []TypedPackageIdentity
	// OtherPackageCandidates supplies capture-derived identities for packages
	// that may be selected by this load. Only its verified local closure is used.
	OtherPackageCandidates   []TypedPackageIdentity
	SyntaxCache              *SyntaxCache // optional parse-only cache, never typed facts
	IncludeStdlibCallTargets bool         // opt-in evidence for the static flow adapter
	MaxNodes                 int
	MaxDepth                 int
	MaxFacts                 int
	MaxOccurrences           int
	MaxOutputBytes           int
}

// Package identities are supplied by the coordinator, never guessed from
// a basename. Each entry must identify an already validated local dependency.
type TypedPackageIdentity struct {
	ImportPath string
	PackageKey string
	ModuleKey  string
}

// TypedOccurrence is an internal, source-bound binding observation. An empty
// SymbolID is an explicit boundary, not proof that the reference has no target.
// Selection distinguishes field/method selection without duplicating its use.
type TypedOccurrence struct {
	Path       string          `json:"path"`
	Range      model.ByteRange `json:"range"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"` // definition or use
	SymbolID   *string         `json:"symbol_id"`
	ScopeID    string          `json:"scope_id"`
	Resolution string          `json:"resolution"` // captured, namespace, other_package, external, builtin, unmapped_local
	Selection  string          `json:"selection"`  // empty, field, method_value, method_expression
	ObjectKind string          `json:"object_kind"`
	Dispatch   string          `json:"dispatch"` // empty, concrete, interface
	Indirect   bool            `json:"indirect"`
	Candidates []string        `json:"candidates"` // same-load proven method IDs; scoped, not exhaustive
}

// TypedCallSite distinguishes a function call from a type conversion without
// publishing go/types objects or an inferred runtime target.
type TypedCallSite struct {
	Path         string            `json:"path"`
	Range        model.ByteRange   `json:"range"` // callable expression, not arguments
	Kind         string            `json:"kind"`  // call, conversion, unknown
	StdlibTarget *StdlibCallTarget `json:"stdlib_target,omitempty"`
}

type TypedResult struct {
	Loader      CapturedLoaderResult `json:"loader"`
	Status      string               `json:"status"`
	Reasons     []string             `json:"reasons"`
	Syntax      []SyntaxResult       `json:"syntax"`
	Occurrences []TypedOccurrence    `json:"occurrences"`
	Calls       []TypedCallSite      `json:"calls"`
}

// AnalyzeTypedCaptured consumes compiler identities only inside a single
// guarded load. A partial load returns no typed facts; no *types.Object or
// AST pointer is retained in or returned by TypedResult.
func AnalyzeTypedCaptured(ctx context.Context, input TypedInput) (TypedResult, error) {
	if ctx == nil || input.MaxNodes < 1 || input.MaxNodes > 1000000 || input.MaxDepth < 1 || input.MaxDepth > 512 || input.MaxFacts < 1 || input.MaxFacts > 100000 ||
		input.MaxOccurrences < 1 || input.MaxOccurrences > 100000 || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 16*1024*1024 || len(input.OtherPackages)+len(input.OtherPackageCandidates) > 127 ||
		len(input.PackageKey) < 1 || len(input.PackageKey) > 192*4 || len(input.ModuleKey) < 1 || len(input.ModuleKey) > 192*4 {
		return TypedResult{}, ErrLoaderInput
	}
	for _, identity := range append(append([]TypedPackageIdentity(nil), input.OtherPackages...), input.OtherPackageCandidates...) {
		if len(identity.ImportPath) < 1 || len(identity.ImportPath) > 2048*4 || len(identity.PackageKey) < 1 || len(identity.PackageKey) > 192*4 || len(identity.ModuleKey) < 1 || len(identity.ModuleKey) > 192*4 {
			return TypedResult{}, ErrLoaderInput
		}
	}
	if err := ctx.Err(); err != nil {
		return TypedResult{}, err
	}
	var err error
	input.Loader.Preflight, err = copyLoaderPreflight(input.Loader.Preflight)
	if err != nil {
		return TypedResult{}, err
	}
	input.OtherPackages = append([]TypedPackageIdentity(nil), input.OtherPackages...)
	input.OtherPackageCandidates = append([]TypedPackageIdentity(nil), input.OtherPackageCandidates...)
	var extracted *TypedResult
	loader := input.Loader
	loader.consumeTypes = func(ctx context.Context, pkg *packages.Package, overlay *CapturedOverlay, files map[*packages.Package][]string) error {
		result, err := collectTyped(ctx, input, pkg, overlay, files)
		if err != nil {
			return err
		}
		extracted = &result
		return nil
	}
	loaded, err := LoadCapturedPackages(ctx, loader)
	if err != nil {
		return TypedResult{}, err
	}
	result := TypedResult{Loader: loaded, Status: "partial", Reasons: append([]string(nil), loaded.Reasons...), Syntax: []SyntaxResult{}, Occurrences: []TypedOccurrence{}, Calls: []TypedCallSite{}}
	if loaded.BuildSelectionVerified {
		if extracted == nil {
			return TypedResult{}, ErrInputMismatch
		}
		result = *extracted
		result.Loader = loaded
		// Graph construction, coverage and publication are later tasks.
		result.Reasons = append(result.Reasons, "SEMANTIC_RELATIONS_PENDING")
		sort.Strings(result.Reasons)
	}
	manifest := input.Loader.Preflight.Snapshot.Manifest()
	for _, syntax := range result.Syntax {
		if err := input.Loader.Preflight.AuthorizePath(ctx, manifest.WorktreeID, syntax.Path, manifest.PolicyDigest, input.Loader.Preflight.PolicyEpoch); err != nil {
			return TypedResult{}, loaderAuthorityError(ctx)
		}
	}
	if err := input.Loader.Preflight.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, input.Loader.Preflight.PolicyEpoch); err != nil {
		return TypedResult{}, loaderAuthorityError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return TypedResult{}, err
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > input.MaxOutputBytes {
		return TypedResult{}, ErrSyntaxLimit
	}
	return result, nil
}

type typedSite struct {
	path       string
	start, end int64
	name       string
}

type typedRange struct {
	path       string
	start, end int64
}

type typedScope struct {
	scope        Scope
	prefixMaxEnd int64
}

type typedFileScopes struct {
	packageID string
	levels    [][]typedScope
}

func (f *typedFileScopes) add(scope Scope, depth int) {
	if depth == 0 {
		f.packageID = scope.ID
		return
	}
	for len(f.levels) < depth {
		f.levels = append(f.levels, nil)
	}
	f.levels[depth-1] = append(f.levels[depth-1], typedScope{scope: scope})
}

func (f *typedFileScopes) index() {
	for _, level := range f.levels {
		sort.Slice(level, func(i, j int) bool {
			return level[i].scope.StartByte < level[j].scope.StartByte
		})
		var maxEnd int64
		for i := range level {
			maxEnd = max(maxEnd, level[i].scope.EndByte)
			level[i].prefixMaxEnd = maxEnd
		}
	}
}

type typedSelection struct {
	kind, dispatch string
	indirect       bool
	candidates     []string
}

type typedCollector struct {
	ctx             context.Context
	input           TypedInput
	pkg             *packages.Package
	overlay         *CapturedOverlay
	selected        map[string]bool
	localPackages   map[string]bool
	candidates      map[typedSite][]model.SymbolFact
	caseScopes      map[typedRange]string
	caseSymbols     map[string][]model.SymbolFact
	fileScopes      map[string]typedFileScopes
	objects         map[types.Object]string
	output          TypedResult
	usedBytes       int
	unmapped        bool
	crossPending    bool
	scopeChecks     int
	candidateTypes  []*types.Named
	candidateChecks int
	interfaceSeen   bool
}

type typedPackage struct {
	pkg      *packages.Package
	identity TypedPackageIdentity
	files    []string
}

func collectTyped(ctx context.Context, input TypedInput, pkg *packages.Package, overlay *CapturedOverlay, graph map[*packages.Package][]string) (TypedResult, error) {
	if pkg == nil || pkg.Types == nil || pkg.TypesInfo == nil || pkg.Fset == nil || overlay == nil || len(graph[pkg]) == 0 || len(graph[pkg]) > 128 {
		return TypedResult{}, ErrInputMismatch
	}
	available := map[string]*packages.Package{}
	for current, files := range graph {
		if current == nil || len(files) == 0 || current.Fset != pkg.Fset || current.Types == nil || current.TypesInfo == nil || available[current.PkgPath] != nil {
			return TypedResult{}, ErrInputMismatch
		}
		available[current.PkgPath] = current
	}
	identities := map[string]TypedPackageIdentity{pkg.PkgPath: {ImportPath: pkg.PkgPath, PackageKey: input.PackageKey, ModuleKey: input.ModuleKey}}
	seenKeys := map[string]bool{input.PackageKey: true}
	addIdentity := func(identity TypedPackageIdentity) error {
		if identity.ImportPath == "" || identity.PackageKey == "" || identity.ModuleKey == "" || identities[identity.ImportPath].ImportPath != "" || available[identity.ImportPath] == nil || seenKeys[identity.PackageKey] {
			return ErrInputMismatch
		}
		identities[identity.ImportPath] = identity
		seenKeys[identity.PackageKey] = true
		return nil
	}
	for _, identity := range input.OtherPackages {
		if err := addIdentity(identity); err != nil {
			return TypedResult{}, ErrInputMismatch
		}
	}
	for _, identity := range input.OtherPackageCandidates {
		if available[identity.ImportPath] != nil {
			if err := addIdentity(identity); err != nil {
				return TypedResult{}, err
			}
		}
	}
	var units []typedPackage
	localPackages := make(map[string]bool, len(available))
	for importPath, current := range available {
		localPackages[importPath] = true
		if identity, selected := identities[importPath]; selected {
			units = append(units, typedPackage{pkg: current, identity: identity, files: graph[current]})
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].pkg.PkgPath < units[j].pkg.PkgPath })
	c := &typedCollector{ctx: ctx, input: input, pkg: pkg, overlay: overlay, selected: map[string]bool{}, localPackages: localPackages, candidates: map[typedSite][]model.SymbolFact{}, caseScopes: map[typedRange]string{}, caseSymbols: map[string][]model.SymbolFact{}, fileScopes: map[string]typedFileScopes{}, objects: map[types.Object]string{},
		output: TypedResult{Status: "partial", Reasons: []string{}, Syntax: []SyntaxResult{}, Occurrences: []TypedOccurrence{}, Calls: []TypedCallSite{}}}
	for _, unit := range units {
		current := unit.pkg
		variant, packageName := "production", current.Name
		if current.ForTest != "" {
			switch current.PkgPath {
			case current.ForTest:
				variant = "internal_test"
			case current.ForTest + "_test":
				variant = "external_test"
				if !strings.HasSuffix(packageName, "_test") {
					return TypedResult{}, ErrInputMismatch
				}
				packageName = strings.TrimSuffix(packageName, "_test")
			default:
				return TypedResult{}, ErrInputMismatch
			}
		}
		for _, filename := range unit.files {
			if c.selected[filename] {
				return TypedResult{}, ErrInputMismatch
			}
			c.selected[filename] = true
			syntax, err := AnalyzeSyntaxFile(ctx, SyntaxInput{Snapshot: input.Loader.Preflight.Snapshot, Profile: input.Loader.Preflight.Profile, Path: filename, PackageKey: unit.identity.PackageKey,
				ImportPath: current.PkgPath, ModuleKey: unit.identity.ModuleKey, PackageName: packageName, Variant: variant, Scanner: input.Loader.Preflight.Scanner,
				Cache: input.SyntaxCache, MaxManifestBytes: input.Loader.Preflight.MaxManifestBytes, MaxFileBytes: input.Loader.Preflight.MaxFileBytes, MaxNodes: input.MaxNodes, MaxDepth: input.MaxDepth, MaxFacts: input.MaxFacts, MaxOutputBytes: input.MaxOutputBytes})
			if err != nil || len(syntax.Diagnostics) != 0 {
				if err != nil {
					return TypedResult{}, err
				}
				return TypedResult{}, ErrInputMismatch
			}
			syntax.BuildSelectionVerified = true
			if err := c.reserve(syntax); err != nil {
				return TypedResult{}, err
			}
			c.output.Syntax = append(c.output.Syntax, syntax)
			depths := map[string]int{}
			var fileScopes typedFileScopes
			for _, scope := range syntax.Scopes {
				depth := 0
				if scope.ParentID != "" {
					parentDepth, exists := depths[scope.ParentID]
					if !exists || scope.Path != filename {
						return TypedResult{}, ErrInputMismatch
					}
					depth = parentDepth + 1
				} else if scope.Kind != "package" || scope.Path != "" || len(depths) != 0 {
					return TypedResult{}, ErrInputMismatch
				}
				if _, exists := depths[scope.ID]; exists {
					return TypedResult{}, ErrInputMismatch
				}
				depths[scope.ID] = depth
				fileScopes.add(scope, depth)
				if scope.Kind != "case" {
					continue
				}
				key := typedRange{path: filename, start: scope.StartByte, end: scope.EndByte}
				if c.caseScopes[key] != "" {
					return TypedResult{}, ErrInputMismatch
				}
				c.caseScopes[key] = scope.ID
			}
			fileScopes.index()
			c.fileScopes[filename] = fileScopes
			for _, symbol := range syntax.Symbols {
				if symbol.Path == nil || symbol.Range == nil || symbol.Kind == "file" || symbol.Kind == "package" || symbol.Kind == "closure" {
					continue
				}
				key := typedSite{path: *symbol.Path, start: symbol.Range.StartByte, end: symbol.Range.EndByte, name: symbol.Name}
				c.candidates[key] = append(c.candidates[key], symbol)
				c.caseSymbols[symbol.ScopeKey+"\x00"+symbol.Name] = append(c.caseSymbols[symbol.ScopeKey+"\x00"+symbol.Name], symbol)
			}
		}
	}
	for _, unit := range units {
		c.pkg = unit.pkg
		for ident, object := range unit.pkg.TypesInfo.Defs {
			if err := ctx.Err(); err != nil {
				return TypedResult{}, err
			}
			if ident == nil || ident.Name == "_" || object == nil {
				continue
			}
			if _, isPackageName := object.(*types.PkgName); isPackageName {
				continue
			}
			site, err := c.identSite(ident)
			if err != nil {
				return TypedResult{}, err
			}
			matches := c.matchingSymbols(site, object)
			if len(matches) != 1 {
				c.unmapped = true
				if err := c.appendOccurrence(site, "definition", "", "unmapped_local", "", object, typedSelection{}); err != nil {
					return TypedResult{}, err
				}
				continue
			}
			if err := c.recordObject(object, matches[0].SymbolID); err != nil {
				return TypedResult{}, err
			}
			if err := c.appendOccurrence(site, "definition", matches[0].SymbolID, "captured", matches[0].ScopeKey, object, typedSelection{}); err != nil {
				return TypedResult{}, err
			}
		}
	}
	for _, unit := range units {
		c.pkg = unit.pkg
		for node, object := range unit.pkg.TypesInfo.Implicits {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				continue
			}
			variable, ok := object.(*types.Var)
			if !ok || variable.Name() == "_" {
				continue
			}
			bindingSite, err := c.identSite(&ast.Ident{NamePos: variable.Pos(), Name: variable.Name()})
			if err != nil {
				return TypedResult{}, err
			}
			filename, start, end, err := c.positionRange(clause.Pos(), clause.End())
			if err != nil {
				return TypedResult{}, err
			}
			scope := c.caseScopes[typedRange{path: filename, start: start, end: end}]
			var matches []model.SymbolFact
			for _, symbol := range c.caseSymbols[scope+"\x00"+variable.Name()] {
				if symbol.Kind == "variable" && symbol.Path != nil && symbol.Range != nil {
					matches = append(matches, symbol)
				}
			}
			if scope == "" || len(matches) != 1 {
				c.unmapped = true
				if err := c.appendOccurrence(bindingSite, "definition", "", "unmapped_local", "", variable, typedSelection{}); err != nil {
					return TypedResult{}, err
				}
				continue
			}
			symbol := matches[0]
			if *symbol.Path != bindingSite.path || symbol.Range.StartByte != bindingSite.start || symbol.Range.EndByte != bindingSite.end {
				return TypedResult{}, ErrInputMismatch
			}
			if err := c.recordObject(variable, symbol.SymbolID); err != nil {
				return TypedResult{}, err
			}
			if err := c.appendOccurrence(bindingSite, "definition", symbol.SymbolID, "captured", symbol.ScopeKey, variable, typedSelection{}); err != nil {
				return TypedResult{}, err
			}
		}
	}
	seenTypes := map[*types.Named]bool{}
	for _, unit := range units {
		for _, object := range unit.pkg.TypesInfo.Defs {
			name, ok := object.(*types.TypeName)
			if !ok || name.IsAlias() {
				continue
			}
			named, ok := types.Unalias(name.Type()).(*types.Named)
			if !ok || seenTypes[named] || named.TypeParams().Len() != 0 {
				continue
			}
			if _, isInterface := named.Underlying().(*types.Interface); isInterface {
				continue
			}
			seenTypes[named] = true
			c.candidateTypes = append(c.candidateTypes, named)
			if len(c.candidateTypes) > 512 {
				return TypedResult{}, ErrSyntaxLimit
			}
		}
	}
	for _, unit := range units {
		pkg := unit.pkg
		c.pkg = pkg
		for _, file := range pkg.Syntax {
			nodes := 0
			var walkErr error
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil || walkErr != nil {
					return walkErr == nil
				}
				nodes++
				if nodes > input.MaxNodes {
					walkErr = ErrSyntaxLimit
					return false
				}
				if nodes%64 == 0 && ctx.Err() != nil {
					walkErr = ctx.Err()
					return false
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				filename, start, end, err := c.positionRange(call.Fun.Pos(), call.Fun.End())
				if err != nil {
					walkErr = err
					return false
				}
				kind := "unknown"
				if value, exists := pkg.TypesInfo.Types[call.Fun]; exists {
					kind = "call"
					if value.IsType() {
						kind = "conversion"
					}
				}
				item := TypedCallSite{Path: filename, Range: model.ByteRange{StartByte: start, EndByte: end}, Kind: kind}
				if input.IncludeStdlibCallTargets && kind == "call" {
					item.StdlibTarget = stdlibCallTarget(pkg.TypesInfo, call.Fun)
				}
				if len(c.output.Occurrences)+len(c.output.Calls) >= input.MaxOccurrences {
					walkErr = ErrSyntaxLimit
					return false
				}
				if err := c.reserve(item); err != nil {
					walkErr = err
					return false
				}
				c.output.Calls = append(c.output.Calls, item)
				return true
			})
			if walkErr != nil {
				return TypedResult{}, walkErr
			}
		}
		selections := make(map[*ast.Ident]typedSelection, len(pkg.TypesInfo.Selections))
		for selector, selection := range pkg.TypesInfo.Selections {
			if selector == nil || selector.Sel == nil || selection == nil || originObject(pkg.TypesInfo.Uses[selector.Sel]) != originObject(selection.Obj()) {
				return TypedResult{}, ErrInputMismatch
			}
			kind := ""
			switch selection.Kind() {
			case types.FieldVal:
				kind = "field"
			case types.MethodVal:
				kind = "method_value"
			case types.MethodExpr:
				kind = "method_expression"
			default:
				return TypedResult{}, ErrInputMismatch
			}
			observation := typedSelection{kind: kind, indirect: selection.Indirect()}
			if selection.Kind() != types.FieldVal {
				if selection.Recv() == nil {
					return TypedResult{}, ErrInputMismatch
				}
				receiver := types.Unalias(selection.Recv())
				if pointer, ok := receiver.(*types.Pointer); ok {
					receiver = types.Unalias(pointer.Elem())
				}
				if _, ok := receiver.Underlying().(*types.Interface); ok {
					observation.dispatch = "interface"
					c.interfaceSeen = true
					if _, generic := receiver.(*types.TypeParam); !generic {
						iface := receiver.Underlying().(*types.Interface)
						if iface.IsMethodSet() {
							var err error
							observation.candidates, err = c.interfaceCandidates(iface, selection.Obj())
							if err != nil {
								return TypedResult{}, err
							}
						}
					}
				} else {
					observation.dispatch = "concrete"
				}
			}
			selections[selector.Sel] = observation
		}
		for ident, object := range pkg.TypesInfo.Uses {
			if err := ctx.Err(); err != nil {
				return TypedResult{}, err
			}
			if ident == nil || object == nil || ident.Name == "_" {
				continue
			}
			site, err := c.identSite(ident)
			if err != nil {
				return TypedResult{}, err
			}
			id, found := c.objects[originObject(object)]
			resolution := "captured"
			if !found {
				switch {
				case isPackageObject(object):
					resolution = "namespace"
				case object.Pkg() == nil:
					resolution = "builtin"
				case object.Pkg() != pkg.Types && c.localPackages[object.Pkg().Path()]:
					resolution, c.crossPending = "other_package", true
				case object.Pkg() != pkg.Types:
					resolution = "external"
				default:
					resolution, c.unmapped = "unmapped_local", true
				}
			}
			scope, err := c.scopeFor(site)
			if err != nil {
				return TypedResult{}, err
			}
			if err := c.appendOccurrence(site, "use", id, resolution, scope, object, selections[ident]); err != nil {
				return TypedResult{}, err
			}
		}
	}
	if c.unmapped {
		c.output.Reasons = append(c.output.Reasons, "LOCAL_BINDING_UNMAPPED")
	}
	if c.crossPending {
		c.output.Reasons = append(c.output.Reasons, "CROSS_PACKAGE_BINDING_PENDING")
	}
	if c.interfaceSeen {
		c.output.Reasons = append(c.output.Reasons, "INTERFACE_CANDIDATES_SCOPED")
	}
	sort.Slice(c.output.Calls, func(i, j int) bool {
		a, b := c.output.Calls[i], c.output.Calls[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		if a.Range.EndByte != b.Range.EndByte {
			return a.Range.EndByte < b.Range.EndByte
		}
		return a.Kind < b.Kind
	})
	sort.Slice(c.output.Occurrences, func(i, j int) bool {
		a, b := c.output.Occurrences[i], c.output.Occurrences[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		if a.Range.EndByte != b.Range.EndByte {
			return a.Range.EndByte < b.Range.EndByte
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ScopeID != b.ScopeID {
			return a.ScopeID < b.ScopeID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Resolution != b.Resolution {
			return a.Resolution < b.Resolution
		}
		if a.Selection != b.Selection {
			return a.Selection < b.Selection
		}
		if a.ObjectKind != b.ObjectKind {
			return a.ObjectKind < b.ObjectKind
		}
		if a.Dispatch != b.Dispatch {
			return a.Dispatch < b.Dispatch
		}
		if a.Indirect != b.Indirect {
			return !a.Indirect
		}
		if a.SymbolID == nil || b.SymbolID == nil {
			return a.SymbolID == nil && b.SymbolID != nil
		}
		return *a.SymbolID < *b.SymbolID
	})
	return c.output, nil
}

func originObject(object types.Object) types.Object {
	switch value := object.(type) {
	case *types.Var:
		return value.Origin()
	case *types.Func:
		return value.Origin()
	default:
		return object
	}
}

func isPackageObject(object types.Object) bool { _, ok := object.(*types.PkgName); return ok }

func (c *typedCollector) matchingSymbols(site typedSite, object types.Object) []model.SymbolFact {
	var matches []model.SymbolFact
	for _, symbol := range c.candidates[site] {
		valid := false
		switch object.(type) {
		case *types.Var:
			valid = symbol.Kind == "variable" || symbol.Kind == "parameter" || symbol.Kind == "receiver" || symbol.Kind == "result" || symbol.Kind == "field"
		case *types.Func:
			valid = symbol.Kind == "function" || symbol.Kind == "method"
		case *types.TypeName:
			valid = symbol.Kind == "type" || symbol.Kind == "interface"
		case *types.Const:
			valid = symbol.Kind == "constant"
		}
		if valid {
			matches = append(matches, symbol)
		}
	}
	return matches
}

func (c *typedCollector) recordObject(object types.Object, symbolID string) error {
	object = originObject(object)
	if object == nil || symbolID == "" {
		return ErrInputMismatch
	}
	if prior, exists := c.objects[object]; exists && prior != symbolID {
		return ErrInputMismatch
	}
	c.objects[object] = symbolID
	return nil
}

func (c *typedCollector) positionRange(start, end token.Pos) (string, int64, int64, error) {
	file := c.pkg.Fset.File(start)
	if file == nil || c.pkg.Fset.File(end) != file || start < file.Pos(0) || end < start || end > file.Pos(file.Size()) {
		return "", 0, 0, ErrInputMismatch
	}
	filename, exists := c.overlay.files[file.Name()]
	if !exists || !c.selected[filename] {
		return "", 0, 0, ErrInputMismatch
	}
	captured, exists := c.input.Loader.Preflight.Snapshot.File(filename)
	if !exists || file.Size() != captured.Len() {
		return "", 0, 0, ErrInputMismatch
	}
	first, last := int64(file.Offset(start)), int64(file.Offset(end))
	if captured.CheckRange(int(first), int(last)) != nil {
		return "", 0, 0, ErrInputMismatch
	}
	return filename, first, last, nil
}

func (c *typedCollector) identSite(ident *ast.Ident) (typedSite, error) {
	filename, start, end, err := c.positionRange(ident.Pos(), ident.End())
	if err != nil {
		return typedSite{}, err
	}
	file, _ := c.input.Loader.Preflight.Snapshot.File(filename)
	text, err := file.Range(int(start), int(end))
	if err != nil || string(text) != ident.Name {
		return typedSite{}, ErrInputMismatch
	}
	return typedSite{path: filename, start: start, end: end, name: ident.Name}, nil
}

// Per-depth interval bounds skip unrelated sibling scopes. The inspection cap
// still applies when many ranges overlap or the scopes are deeply nested.
func (c *typedCollector) scopeFor(site typedSite) (string, error) {
	if err := c.ctx.Err(); err != nil {
		return "", err
	}
	file := c.fileScopes[site.path]
	best := file.packageID
	for _, level := range file.levels {
		end := sort.Search(len(level), func(i int) bool {
			return level[i].scope.StartByte > site.start
		})
		found := ""
		for i := end - 1; i >= 0 && level[i].prefixMaxEnd >= site.end; i-- {
			c.scopeChecks++
			if c.scopeChecks > 2000000 {
				return "", ErrSyntaxLimit
			}
			if c.scopeChecks%1024 == 0 {
				if err := c.ctx.Err(); err != nil {
					return "", err
				}
			}
			scope := level[i].scope
			if scope.EndByte < site.end {
				continue
			}
			if found != "" && found != scope.ID {
				return "", ErrInputMismatch
			}
			found = scope.ID
		}
		if found != "" {
			best = found
		}
	}
	if best == "" {
		return "", ErrInputMismatch
	}
	return best, nil
}

func (c *typedCollector) appendOccurrence(site typedSite, kind, id, resolution, scope string, object types.Object, selection typedSelection) error {
	if len(c.output.Occurrences)+len(c.output.Calls) >= c.input.MaxOccurrences {
		return ErrSyntaxLimit
	}
	item := TypedOccurrence{Path: site.path, Range: model.ByteRange{StartByte: site.start, EndByte: site.end}, Name: site.name, Kind: kind, ScopeID: scope, Resolution: resolution,
		Selection: selection.kind, ObjectKind: typedObjectKind(object), Dispatch: selection.dispatch, Indirect: selection.indirect, Candidates: append([]string{}, selection.candidates...)}
	if id != "" {
		value := id
		item.SymbolID = &value
	}
	if err := c.reserve(item); err != nil {
		return err
	}
	c.output.Occurrences = append(c.output.Occurrences, item)
	return nil
}

// Candidates are only methods proven compatible in this load's explicitly
// selected captured packages. This is never an exhaustive runtime call set.
func (c *typedCollector) interfaceCandidates(iface *types.Interface, method types.Object) ([]string, error) {
	if iface == nil || method == nil {
		return nil, ErrInputMismatch
	}
	ids := map[string]bool{}
	for _, named := range c.candidateTypes {
		for _, candidate := range []types.Type{named, types.NewPointer(named)} {
			c.candidateChecks++
			if c.candidateChecks > 50000 {
				return nil, ErrSyntaxLimit
			}
			if c.candidateChecks%64 == 0 {
				if err := c.ctx.Err(); err != nil {
					return nil, err
				}
			}
			if !types.Implements(candidate, iface) {
				continue
			}
			selected := types.NewMethodSet(candidate).Lookup(method.Pkg(), method.Name())
			if selected == nil {
				return nil, ErrInputMismatch
			}
			if id, exists := c.objects[originObject(selected.Obj())]; exists {
				ids[id] = true
			}
		}
	}
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}

func typedObjectKind(object types.Object) string {
	switch value := object.(type) {
	case *types.Var:
		if value.IsField() {
			return "field"
		}
		return "variable"
	case *types.Func:
		return "function"
	case *types.TypeName:
		return "type"
	case *types.Const:
		return "constant"
	case *types.Builtin, *types.Nil:
		return "builtin"
	case *types.PkgName:
		return "namespace"
	case *types.Label:
		return "label"
	default:
		return "other"
	}
}

func (c *typedCollector) reserve(value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw)+1 > c.input.MaxOutputBytes-c.usedBytes {
		return ErrSyntaxLimit
	}
	c.usedBytes += len(raw) + 1
	return nil
}
