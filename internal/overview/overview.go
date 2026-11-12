// Package overview builds a bounded, source-backed projection of a pinned Go
// capture. It does not establish live freshness or issue public cursor tokens.
package overview

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

var (
	ErrInput     = errors.New("invalid overview input")
	ErrLimit     = errors.New("overview budget exceeded")
	ErrAuthority = errors.New("overview authority could not be verified")
)

// Scope binds every projected fact and expansion to one pinned generation.
type Scope struct {
	Identity       model.GraphIdentity `json:"identity"`
	SnapshotDigest string              `json:"snapshot_digest"`
	PolicyDigest   string              `json:"policy_digest"`
	PolicyEpoch    int64               `json:"policy_epoch"`
}

type PackageInput struct {
	Fact  model.PackageFact `json:"fact"`
	Files []string          `json:"files"`
}

type Input struct {
	Snapshot       *snapshot.Snapshot
	Profile        *model.GoProfile
	GenerationID   string
	PolicyEpoch    int64
	Packages       []PackageInput
	Symbols        []model.SymbolFact
	Relations      []model.RelationFact
	Scanner        *policy.Scanner
	AuthorizeScope func(context.Context, Scope) error
	AuthorizePath  func(context.Context, Scope, string) error
	// VerifyFacts must establish that the pinned generation and the complete
	// supplied fact set identified by digest are authorized and still valid.
	VerifyFacts      func(context.Context, Scope, string) error
	MaxManifestBytes int
	MaxSourceBytes   int64
	MaxFacts         int
	MaxOutputBytes   int
}

type Module struct {
	Ref        model.ModuleRef `json:"ref"`
	ParentID   string          `json:"parent_id"`
	Root       string          `json:"root"`
	ImportPath string          `json:"import_path"`
	Plane      string          `json:"plane"`
	Summary    string          `json:"summary"`
	Source     model.SourceRef `json:"source"`
	Packages   []Package       `json:"packages"`
}

type Package struct {
	Fact  model.PackageFact `json:"fact"`
	Files []string          `json:"files"`
}

type Summary struct {
	Scope            Scope    `json:"scope"`
	Language         string   `json:"language"`
	CapturedFiles    int      `json:"captured_files"`
	UnavailableFiles int      `json:"unavailable_files"`
	RelationCount    int      `json:"relation_count"`
	Modules          []Module `json:"modules"`
	Groups           []Group  `json:"groups"`
	Gaps             []string `json:"gaps"`
}

type Report struct {
	scope          Scope
	summary        Summary
	groups         map[string][]Site
	paths          []string
	factsDigest    string
	authorizeScope func(context.Context, Scope) error
	authorizePath  func(context.Context, Scope, string) error
	verifyFacts    func(context.Context, Scope, string) error
	maxOutputBytes int
}

// GoModuleID identifies a location within one worktree, not a generation or
// a claim that the location actually contains a module declaration.
func GoModuleID(worktreeID, manifestPath string) (string, error) {
	if model.ValidateRelativePath(manifestPath, false) != nil || path.Base(manifestPath) != "go.mod" {
		return "", ErrInput
	}
	ref := model.ModuleRef{Kind: "module", GraphIdentity: model.GraphIdentity{WorktreeID: worktreeID, ProfileID: strings.Repeat("0", 64), GenerationID: "00000000-0000-4000-8000-000000000000"}, ModuleID: "check"}
	raw, err := json.Marshal(ref)
	if err != nil {
		return "", ErrInput
	}
	if _, err := model.DecodeGraphRefJSON(raw, 16384); err != nil {
		return "", ErrInput
	}
	a, err := model.CanonicalString(worktreeID)
	if err != nil {
		return "", ErrInput
	}
	b, err := model.CanonicalString(manifestPath)
	if err != nil {
		return "", ErrInput
	}
	_, id, err := model.EncodeCanonical("go-module-location-v1", model.CanonicalArray([]model.CanonicalValue{a, b}), 32768)
	if err != nil {
		return "", ErrInput
	}
	return id, nil
}

func (r *Report) authorize(ctx context.Context, paths []string) error {
	if ctx == nil || r == nil {
		return ErrInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.authorizeScope(ctx, r.scope); err != nil {
		return authorityError(ctx)
	}
	if err := r.verifyFacts(ctx, r.scope, r.factsDigest); err != nil {
		return authorityError(ctx)
	}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.authorizePath(ctx, r.scope, p); err != nil {
			return authorityError(ctx)
		}
	}
	if err := r.verifyFacts(ctx, r.scope, r.factsDigest); err != nil {
		return authorityError(ctx)
	}
	if err := r.authorizeScope(ctx, r.scope); err != nil {
		return authorityError(ctx)
	}
	return ctx.Err()
}

func authorityError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrAuthority
}

// Read returns a defensive copy after rechecking the scope and all source
// paths contributing to the summary. Authority remains time-bound.
func (r *Report) Read(ctx context.Context) (Summary, error) {
	if r == nil {
		return Summary{}, ErrInput
	}
	if err := r.authorize(ctx, r.paths); err != nil {
		return Summary{}, err
	}
	raw, err := json.Marshal(r.summary)
	if err != nil || len(raw) > r.maxOutputBytes {
		return Summary{}, ErrLimit
	}
	var result Summary
	if json.Unmarshal(raw, &result) != nil {
		return Summary{}, ErrInput
	}
	if err := r.authorize(ctx, r.paths); err != nil {
		return Summary{}, err
	}
	return result, nil
}

func within(root, filename string) bool {
	return root == "." || filename == root || strings.HasPrefix(filename, root+"/")
}

func buildModules(ctx context.Context, input Input, scope Scope, open func(string) (snapshot.File, error)) ([]Module, []string, map[string]string, error) {
	manifest := input.Snapshot.Manifest()
	declared := make(map[string]bool, len(input.Profile.ModuleRoots))
	for _, root := range input.Profile.ModuleRoots {
		declared[root] = true
	}
	boundaries := map[string]string{}
	gaps := map[string]bool{}
	for _, item := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		if path.Base(item.Path) == "go.mod" {
			root := path.Dir(item.Path)
			boundaries[root] = item.Path
			if !declared[root] {
				gaps["MODULE_OUTSIDE_PROFILE"] = true
			}
		}
		if item.Status != "captured" {
			gaps["CAPTURE_INCOMPLETE"] = true
		}
	}
	modules := []Module{}
	for _, root := range input.Profile.ModuleRoots {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		filename := path.Join(root, "go.mod")
		file, ok := input.Snapshot.File(filename)
		if !ok {
			gaps["MODULE_MANIFEST_UNAVAILABLE"] = true
			continue
		}
		file, err := open(filename)
		if err != nil {
			return nil, nil, nil, err
		}
		if file.Len() > 2*1024*1024 {
			return nil, nil, nil, ErrLimit
		}
		parsed, err := modfile.Parse(filename, file.Bytes(), nil)
		if err != nil || parsed.Module == nil || parsed.Module.Syntax == nil || module.CheckPath(parsed.Module.Mod.Path) != nil || utf8.RuneCountInString(parsed.Module.Mod.Path) > 512 {
			gaps["INVALID_MODULE_MANIFEST"] = true
			continue
		}
		if err := input.Scanner.CheckContent([]byte(parsed.Module.Mod.Path)); err != nil {
			return nil, nil, nil, err
		}
		start, end := parsed.Module.Syntax.Span()
		if file.CheckRange(start.Byte, end.Byte) != nil {
			return nil, nil, nil, ErrInput
		}
		id, err := GoModuleID(scope.Identity.WorktreeID, filename)
		if err != nil {
			return nil, nil, nil, err
		}
		modules = append(modules, Module{Ref: model.ModuleRef{Kind: "module", GraphIdentity: scope.Identity, ModuleID: id}, Root: root, ImportPath: parsed.Module.Mod.Path, Plane: "observed_static",
			Source: model.SourceRef{WorktreeID: scope.Identity.WorktreeID, ProfileID: scope.Identity.ProfileID, GenerationID: scope.Identity.GenerationID, Path: filename, ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: int64(start.Byte), EndByte: int64(end.Byte)}}, Packages: []Package{}})
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Root < modules[j].Root })
	for i := range modules {
		best := -1
		for j := range modules {
			if i != j && within(modules[j].Root, modules[i].Root) && (best < 0 || len(modules[j].Root) > len(modules[best].Root)) {
				best = j
			}
		}
		if best >= 0 {
			modules[i].ParentID = modules[best].Ref.ModuleID
		}
	}
	var reasons []string
	for reason := range gaps {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return modules, reasons, boundaries, nil
}
