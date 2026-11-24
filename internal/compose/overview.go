package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// buildOverview projects only package membership evidenced by whole-file
// facts in the pinned provider catalog. It does not infer membership from
// directory names or expose a public pack or a live freshness verdict.
func (b *CoreBackend) buildOverview(ctx context.Context, principal app.CorePrincipal, scope model.ScopeVector, graph *index.PublishedGraph, scanner *policy.Scanner, maxSource int64) (*overview.Report, error) {
	if !b.valid() || ctx == nil || graph == nil || graph.Snapshot == nil || scanner == nil || maxSource < 1 || maxSource > packSourceLimit {
		return nil, app.ErrCoreInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, app.ErrCoreInput
	}
	observed, err := b.Reader.Recheck(ctx, principal, scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if observed.Binding.Profile == nil {
		return nil, app.ErrCoreUnsupported
	}
	member := observed.Vector.Members[0]
	if err := graph.VerifyCatalog(ctx, member); err != nil {
		return nil, coreStoreError(err)
	}
	packages, symbols, relations, digest, err := projectOverviewFacts(ctx, graph)
	if err != nil {
		return nil, err
	}
	expected := overview.Scope{Identity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SnapshotDigest: member.SnapshotDigest, PolicyDigest: member.PolicyDigest, PolicyEpoch: member.PolicyEpoch}
	checkScope := func(ctx context.Context, value overview.Scope) error {
		if value != expected {
			return ErrScopeChanged
		}
		_, err := b.Reader.Recheck(ctx, principal, observed.Vector)
		return err
	}
	verifyFacts := func(ctx context.Context, value overview.Scope, actual string) error {
		if actual != digest || value != expected {
			return ErrScopeChanged
		}
		// Report.authorize brackets fact and path callbacks with full current
		// scope checks. This callback owns the independent catalog proof.
		return graph.VerifyCatalog(ctx, member)
	}
	return overview.Build(ctx, overview.Input{Snapshot: graph.Snapshot, Profile: observed.Binding.Profile, GenerationID: member.GenerationID, PolicyEpoch: member.PolicyEpoch, Packages: packages, Symbols: symbols, Relations: relations, Scanner: scanner, AuthorizeScope: checkScope, AuthorizePath: func(ctx context.Context, value overview.Scope, name string) error {
		if value != expected {
			return ErrScopeChanged
		}
		if err := b.Authority.samePrincipal(ctx, principal); err != nil {
			return err
		}
		return b.Authority.path(ctx, principal, observed.Binding.Policy, name)
	}, VerifyFacts: verifyFacts, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxSourceBytes: maxSource, MaxFacts: 100000, MaxOutputBytes: packFactLimit})
}

// projectOverviewFacts computes the same bounded catalog projection for both
// an initial overview and verification of an already selected restored plan.
// It does not build a replacement plan or resolve an active generation.
func projectOverviewFacts(ctx context.Context, graph *index.PublishedGraph) ([]overview.PackageInput, []model.SymbolFact, []model.RelationFact, string, error) {
	if ctx == nil || graph == nil || graph.Snapshot == nil {
		return nil, nil, nil, "", app.ErrCoreInput
	}
	if len(graph.Packages)+len(graph.Symbols)+len(graph.Relations) > 100000 {
		return nil, nil, nil, "", app.ErrCoreLimit
	}
	packages, err := evidencedOverviewPackages(ctx, graph)
	if err != nil {
		return nil, nil, nil, "", err
	}
	symbols := append([]model.SymbolFact{}, graph.Symbols...)
	relations := append([]model.RelationFact{}, graph.Relations...)
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].SymbolID < symbols[j].SymbolID })
	sort.Slice(relations, func(i, j int) bool { return relations[i].RelationID < relations[j].RelationID })
	raw, err := json.Marshal(struct {
		Packages  []overview.PackageInput `json:"packages"`
		Symbols   []model.SymbolFact      `json:"symbols"`
		Relations []model.RelationFact    `json:"relations"`
	}{packages, symbols, relations})
	if err != nil || len(raw) > packFactLimit {
		return nil, nil, nil, "", app.ErrCoreLimit
	}
	sum := sha256.Sum256(append([]byte("mce4d-overview-facts-v1\x00"), raw...))
	return packages, symbols, relations, hex.EncodeToString(sum[:]), nil
}

// This extractor operates on already-verified catalog facts. The enclosing
// adapter performs scope and digest checks before and after using its output.
func evidencedOverviewPackages(ctx context.Context, graph *index.PublishedGraph) ([]overview.PackageInput, error) {
	filesByPackage := make(map[string]map[string]bool, len(graph.Packages))
	coveredFiles := make(map[string]bool)
	for _, pkg := range graph.Packages {
		if _, exists := filesByPackage[pkg.PackageKey]; exists {
			return nil, app.ErrCoreBackend
		}
		filesByPackage[pkg.PackageKey] = map[string]bool{}
	}
	for _, symbol := range graph.Symbols {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if symbol.Kind != "file" || symbol.Path == nil || symbol.PackageKey == nil || symbol.Range == nil {
			continue
		}
		files, exists := filesByPackage[*symbol.PackageKey]
		file, present := graph.Snapshot.File(*symbol.Path)
		if !exists || !present || symbol.Range.StartByte != 0 || symbol.Range.EndByte != int64(file.Len()) {
			return nil, app.ErrCoreBackend
		}
		files[*symbol.Path] = true
		coveredFiles[*symbol.Path] = true
	}
	// Until an authenticated continuation for missing package membership
	// exists, refuse a partial file list rather than calling it complete.
	for _, file := range graph.Snapshot.Manifest().Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.Status == "captured" && strings.HasSuffix(file.Path, ".go") && !coveredFiles[file.Path] {
			return nil, contextpack.ErrContinuationUnavailable
		}
	}
	packages := make([]overview.PackageInput, 0, len(graph.Packages))
	for _, pkg := range graph.Packages {
		files := make([]string, 0, len(filesByPackage[pkg.PackageKey]))
		for name := range filesByPackage[pkg.PackageKey] {
			files = append(files, name)
		}
		if len(files) == 0 {
			return nil, contextpack.ErrContinuationUnavailable
		}
		sort.Strings(files)
		packages = append(packages, overview.PackageInput{Fact: pkg, Files: files})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Fact.PackageKey < packages[j].Fact.PackageKey })
	return packages, nil
}

// buildPlan accepts explicit declarations from an independently authorized
// source. It does not treat document text or filenames as authority or create
// observed implementation facts from an intended component.
func (b *CoreBackend) buildPlan(ctx context.Context, principal app.CorePrincipal, scope model.ScopeVector, graph *index.PublishedGraph, scanner *policy.Scanner, maxSource int64, request model.PackRequest, declarations []overview.DocumentDeclaration, authorizeDocument func(context.Context, overview.Scope, overview.DocumentDeclaration) error) (*overview.Plan, error) {
	if !b.valid() || ctx == nil || authorizeDocument == nil || request.TaskKind != "plan" && request.TaskKind != "explore" || request.View == nil || *request.View != "overview" || len(declarations) > 512 {
		return nil, app.ErrCoreInput
	}
	requested, err := b.resolveRequestScope(ctx, principal, request.Scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if model.ComparePinnedScope(requested.Vector, scope, 32768) != nil {
		return nil, ErrScopeChanged
	}
	report, err := b.buildOverview(ctx, principal, scope, graph, scanner, maxSource)
	if err != nil {
		return nil, err
	}
	intent := ""
	if request.Intent != nil {
		intent = *request.Intent
	}
	member := requested.Vector.Members[0]
	expected := overview.Scope{Identity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SnapshotDigest: member.SnapshotDigest, PolicyDigest: member.PolicyDigest, PolicyEpoch: member.PolicyEpoch}
	return overview.BuildPlan(ctx, overview.PlanInput{Overview: report, Snapshot: graph.Snapshot, Policy: requested.Binding.Policy, Selector: request.Selector, Declarations: declarations, Scanner: scanner, TaskKind: request.TaskKind, Intent: intent, Constraints: request.Constraints, MaxBytes: packFactLimit, AuthorizeDocument: func(ctx context.Context, observed overview.Scope, declaration overview.DocumentDeclaration) error {
		if observed != expected {
			return ErrScopeChanged
		}
		// BuildPlan and Plan.Read check the complete overview after document
		// callbacks; no document content leaves that owner before this check.
		if err := b.Authority.samePrincipal(ctx, principal); err != nil {
			return err
		}
		if err := b.Authority.path(ctx, principal, requested.Binding.Policy, declaration.Source.Path); err != nil {
			return err
		}
		return authorizeDocument(ctx, observed, declaration)
	}})
}
