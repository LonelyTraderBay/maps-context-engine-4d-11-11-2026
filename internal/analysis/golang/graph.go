package golang

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type GraphInput struct {
	Typed          TypedInput
	MaxRelations   int
	MaxOutputBytes int
}

type GraphResult struct {
	Typed     TypedResult          `json:"typed"`
	Relations []model.RelationFact `json:"relations"`
	Status    string               `json:"status"`
	Reasons   []string             `json:"reasons"`
}

// AnalyzeCapturedGraph returns source-backed static relations for one guarded
// load. It is not a runtime trace, full-workspace proof, or published graph.
func AnalyzeCapturedGraph(ctx context.Context, input GraphInput) (GraphResult, error) {
	if ctx == nil || input.MaxRelations < 1 || input.MaxRelations > 100000 || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 16*1024*1024 {
		return GraphResult{}, ErrLoaderInput
	}
	if err := ctx.Err(); err != nil {
		return GraphResult{}, err
	}
	var err error
	input.Typed.Loader.Preflight, err = copyLoaderPreflight(input.Typed.Loader.Preflight)
	if err != nil {
		return GraphResult{}, err
	}
	input.Typed.OtherPackages = append([]TypedPackageIdentity(nil), input.Typed.OtherPackages...)
	if input.Typed.MaxOutputBytes > input.MaxOutputBytes {
		input.Typed.MaxOutputBytes = input.MaxOutputBytes
	}
	typed, err := AnalyzeTypedCaptured(ctx, input.Typed)
	if err != nil {
		return GraphResult{}, err
	}
	result := GraphResult{Typed: typed, Relations: []model.RelationFact{}, Status: "partial", Reasons: append([]string(nil), typed.Reasons...)}
	if !typed.Loader.BuildSelectionVerified {
		return boundedGraphResult(result, input.MaxOutputBytes)
	}
	collector, err := newGraphCollector(ctx, input, typed)
	if err != nil {
		return GraphResult{}, err
	}
	if err := collector.build(); err != nil {
		return GraphResult{}, err
	}
	if err := graphAuthorityRecheck(ctx, input.Typed); err != nil {
		return GraphResult{}, err
	}
	result.Relations = collector.relations
	result.Reasons = result.Reasons[:0]
	for _, reason := range typed.Reasons {
		if reason != "SEMANTIC_RELATIONS_PENDING" {
			collector.reasons[reason] = true
		}
	}
	collector.reasons["GRAPH_COVERAGE_PARTIAL"] = true
	for reason := range collector.reasons {
		result.Reasons = append(result.Reasons, reason)
	}
	sort.Strings(result.Reasons)
	result.Typed.Reasons = append([]string(nil), result.Reasons...)
	for i, reason := range result.Typed.Loader.Reasons {
		if reason == "SEMANTIC_RELATIONS_PENDING" {
			result.Typed.Loader.Reasons[i] = "GRAPH_COVERAGE_PARTIAL"
		}
	}
	return boundedGraphResult(result, input.MaxOutputBytes)
}

func graphAuthorityRecheck(ctx context.Context, input TypedInput) error {
	preflight := input.Loader.Preflight
	post, err := PreflightLoader(ctx, preflight)
	if err != nil {
		return err
	}
	if post.Status != "eligible_for_guarded_load" || input.Loader.Tool == nil || input.Loader.VerifyScratch == nil {
		return ErrLoaderAuthority
	}
	if err := input.Loader.Tool.CheckForLoad(ctx, input.Loader.GoRoot); err != nil {
		return loaderAuthorityError(ctx)
	}
	ok, err := input.Loader.VerifyScratch(ctx, input.Loader.ScratchRoot, input.Loader.WorkspaceRoot, preflight.Snapshot)
	if err != nil || !ok {
		return loaderAuthorityError(ctx)
	}
	if input.Loader.Stdlib != nil {
		ok, err := input.Loader.Stdlib.Check(ctx, preflight.Profile, preflight.Snapshot, input.Loader.GoRoot)
		if err != nil {
			return err
		}
		if !ok {
			return ErrLoaderAuthority
		}
	}
	manifest := preflight.Snapshot.Manifest()
	for _, item := range manifest.Files {
		if err := preflight.AuthorizePath(ctx, manifest.WorktreeID, item.Path, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
			return loaderAuthorityError(ctx)
		}
	}
	if err := preflight.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
		return loaderAuthorityError(ctx)
	}
	return ctx.Err()
}

func boundedGraphResult(result GraphResult, limit int) (GraphResult, error) {
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > limit {
		return GraphResult{}, ErrSyntaxLimit
	}
	return result, nil
}

type graphScopeRange struct {
	typedRange
	kind string
}

type graphCall struct {
	owner         string
	callee        *typedSite
	literalTarget string
}

type graphCollector struct {
	ctx         context.Context
	input       GraphInput
	typed       TypedResult
	files       map[string]snapshot.File
	syntax      map[string]SyntaxResult
	symbols     map[string]model.SymbolFact
	symbolSites map[typedSite][]model.SymbolFact
	fileSymbols map[string]string
	scopes      map[string]Scope
	scopeRanges map[graphScopeRange]string
	scopeOwners map[string]string
	uses        map[typedSite]TypedOccurrence
	useOwners   map[typedSite]string
	calls       map[typedRange]TypedCallSite
	callOwners  map[typedRange]graphCall
	access      map[typedSite]uint8
	relations   []model.RelationFact
	relationIDs map[string]bool
	reasons     map[string]bool
	usedBytes   int
}

func newGraphCollector(ctx context.Context, input GraphInput, typed TypedResult) (*graphCollector, error) {
	c := &graphCollector{ctx: ctx, input: input, typed: typed, files: map[string]snapshot.File{}, syntax: map[string]SyntaxResult{}, symbols: map[string]model.SymbolFact{},
		symbolSites: map[typedSite][]model.SymbolFact{}, fileSymbols: map[string]string{}, scopes: map[string]Scope{}, scopeRanges: map[graphScopeRange]string{},
		scopeOwners: map[string]string{}, uses: map[typedSite]TypedOccurrence{}, useOwners: map[typedSite]string{}, calls: map[typedRange]TypedCallSite{},
		callOwners: map[typedRange]graphCall{}, access: map[typedSite]uint8{}, relations: []model.RelationFact{}, relationIDs: map[string]bool{}, reasons: map[string]bool{}}
	raw, err := json.Marshal(typed)
	if err != nil || len(raw)+256 > input.MaxOutputBytes {
		return nil, ErrSyntaxLimit
	}
	c.usedBytes = len(raw) + 256
	for _, syntax := range typed.Syntax {
		file, exists := input.Typed.Loader.Preflight.Snapshot.File(syntax.Path)
		if !exists || c.syntax[syntax.Path].Path != "" || syntax.SnapshotDigest != typed.Loader.SnapshotDigest || syntax.ContentHash != file.ContentHash() || !syntax.BuildSelectionVerified {
			return nil, ErrInputMismatch
		}
		c.files[syntax.Path], c.syntax[syntax.Path] = file, syntax
		for _, scope := range syntax.Scopes {
			if prior, exists := c.scopes[scope.ID]; exists && prior != scope {
				return nil, ErrInputMismatch
			}
			c.scopes[scope.ID] = scope
			if scope.Path != "" {
				key := graphScopeRange{typedRange: typedRange{path: scope.Path, start: scope.StartByte, end: scope.EndByte}, kind: scope.Kind}
				if c.scopeRanges[key] != "" && c.scopeRanges[key] != scope.ID {
					return nil, ErrInputMismatch
				}
				c.scopeRanges[key] = scope.ID
			}
		}
		for _, symbol := range syntax.Symbols {
			if symbol.SymbolID == "" || symbol.Path == nil || *symbol.Path != syntax.Path || symbol.Range == nil || file.CheckRange(int(symbol.Range.StartByte), int(symbol.Range.EndByte)) != nil {
				return nil, ErrInputMismatch
			}
			if _, duplicate := c.symbols[symbol.SymbolID]; duplicate {
				return nil, ErrInputMismatch
			}
			c.symbols[symbol.SymbolID] = symbol
			key := typedSite{path: syntax.Path, start: symbol.Range.StartByte, end: symbol.Range.EndByte, name: symbol.Name}
			c.symbolSites[key] = append(c.symbolSites[key], symbol)
			if symbol.Kind == "file" {
				if c.fileSymbols[syntax.Path] != "" || symbol.Range.StartByte != 0 || symbol.Range.EndByte != int64(file.Len()) {
					return nil, ErrInputMismatch
				}
				c.fileSymbols[syntax.Path] = symbol.SymbolID
			}
		}
		if c.fileSymbols[syntax.Path] == "" {
			return nil, ErrInputMismatch
		}
	}
	for _, occurrence := range typed.Occurrences {
		if occurrence.Kind != "use" {
			continue
		}
		file, exists := c.files[occurrence.Path]
		if !exists || file.CheckRange(int(occurrence.Range.StartByte), int(occurrence.Range.EndByte)) != nil {
			return nil, ErrInputMismatch
		}
		key := typedSite{path: occurrence.Path, start: occurrence.Range.StartByte, end: occurrence.Range.EndByte, name: occurrence.Name}
		if _, duplicate := c.uses[key]; duplicate {
			return nil, ErrInputMismatch
		}
		if occurrence.Resolution == "captured" {
			if occurrence.SymbolID == nil || c.symbols[*occurrence.SymbolID].SymbolID == "" {
				return nil, ErrInputMismatch
			}
			if occurrence.ObjectKind == "variable" || occurrence.ObjectKind == "field" {
				if occurrence.ObjectKind == "field" && occurrence.Indirect {
					c.reasons["POINTER_ALIAS_BOUNDARY"] = true
				} else {
					c.access[key] = graphRead
				}
			}
		} else if occurrence.SymbolID != nil {
			return nil, ErrInputMismatch
		}
		c.uses[key] = occurrence
	}
	for _, call := range typed.Calls {
		file, exists := c.files[call.Path]
		if !exists || file.CheckRange(int(call.Range.StartByte), int(call.Range.EndByte)) != nil {
			return nil, ErrInputMismatch
		}
		key := typedRange{path: call.Path, start: call.Range.StartByte, end: call.Range.EndByte}
		if _, duplicate := c.calls[key]; duplicate {
			return nil, ErrInputMismatch
		}
		c.calls[key] = call
	}
	return c, nil
}

func (c *graphCollector) build() error {
	paths := make([]string, 0, len(c.syntax))
	for path := range c.syntax {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := c.walkFile(path); err != nil {
			return err
		}
	}
	if len(c.callOwners) != len(c.calls) {
		return ErrInputMismatch
	}
	if err := c.addContainment(paths); err != nil {
		return err
	}
	if err := c.addImports(paths); err != nil {
		return err
	}
	for site, occurrence := range c.uses {
		owner := c.useOwners[site]
		if owner == "" {
			return ErrInputMismatch
		}
		switch occurrence.Resolution {
		case "captured":
			if err := c.emit(owner, occurrence.SymbolID, "REFERS_TO", "type_resolved", site.path, site.start, site.end, "", nil); err != nil {
				return err
			}
			variant := c.syntax[site.path].Package.Variant
			ownerKind := c.symbols[owner].Kind
			if strings.HasSuffix(site.path, "_test.go") && (variant == "internal_test" || variant == "external_test") && (ownerKind == "function" || ownerKind == "method" || ownerKind == "closure") {
				if err := c.emit(owner, occurrence.SymbolID, "TEST_REF", "type_resolved", site.path, site.start, site.end, "static_reference_not_coverage", nil); err != nil {
					return err
				}
			}
			if c.access[site]&graphRead != 0 {
				if err := c.emit(owner, occurrence.SymbolID, "READS", "type_resolved", site.path, site.start, site.end, "", nil); err != nil {
					return err
				}
			}
			if c.access[site]&graphWrite != 0 {
				if err := c.emit(owner, occurrence.SymbolID, "WRITES", "type_resolved", site.path, site.start, site.end, "", nil); err != nil {
					return err
				}
			}
		case "external", "other_package", "unmapped_local":
			label := site.name
			if err := c.emit(owner, nil, "UNRESOLVED_REF", "unresolved", site.path, site.start, site.end, occurrence.Resolution, &label); err != nil {
				return err
			}
			c.reasons["UNRESOLVED_BINDING"] = true
		case "namespace", "builtin":
		default:
			return ErrInputMismatch
		}
	}
	for site, call := range c.calls {
		if call.Kind == "conversion" {
			continue
		}
		if err := c.addCall(site, call); err != nil {
			return err
		}
	}
	sort.Slice(c.relations, func(i, j int) bool {
		a, b := c.relations[i], c.relations[j]
		if a.SitePath != b.SitePath {
			return a.SitePath < b.SitePath
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
		return a.RelationID < b.RelationID
	})
	return c.ctx.Err()
}

func (c *graphCollector) emit(source string, target *string, kind, evidence, path string, start, end int64, reason string, label *string) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	file, exists := c.files[path]
	if !exists || c.symbols[source].SymbolID == "" || target != nil && c.symbols[*target].SymbolID == "" || file.CheckRange(int(start), int(end)) != nil {
		return ErrInputMismatch
	}
	id, err := graphRelationID(source, target, kind, evidence, path, start, end, reason, label)
	if err != nil {
		return err
	}
	if c.relationIDs[id] {
		return nil
	}
	if len(c.relations) >= c.input.MaxRelations {
		return ErrSyntaxLimit
	}
	item := model.RelationFact{RelationID: id, SourceSymbolID: source, TargetSymbolID: target, Kind: kind, EvidenceKind: evidence, SitePath: path, Range: model.ByteRange{StartByte: start, EndByte: end}, Reason: reason, UnresolvedLabel: label}
	raw, err := json.Marshal(item)
	if err != nil || len(raw)+1 > c.input.MaxOutputBytes-c.usedBytes {
		return ErrSyntaxLimit
	}
	if _, err := model.DecodeRelationFactJSON(raw, 65536); err != nil {
		return ErrInputMismatch
	}
	c.usedBytes += len(raw) + 1
	c.relationIDs[id] = true
	c.relations = append(c.relations, item)
	return nil
}

func graphRelationID(source string, target *string, kind, evidence, path string, start, end int64, reason string, label *string) (string, error) {
	fields := []string{source, kind, evidence, path, reason}
	if target == nil {
		fields = append(fields, "")
	} else {
		fields = append(fields, *target)
	}
	if label == nil {
		fields = append(fields, "")
	} else {
		fields = append(fields, *label)
	}
	values := make([]model.CanonicalValue, 0, len(fields)+2)
	for _, field := range fields {
		value, err := model.CanonicalString(field)
		if err != nil {
			return "", err
		}
		values = append(values, value)
	}
	first, err := model.CanonicalInt(start)
	if err != nil {
		return "", err
	}
	last, err := model.CanonicalInt(end)
	if err != nil {
		return "", err
	}
	values = append(values, first, last)
	_, digest, err := model.EncodeCanonical("go-relation-v1", model.CanonicalArray(values), 65536)
	return digest, err
}
