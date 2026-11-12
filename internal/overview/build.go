package overview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type facts struct {
	Packages  []PackageInput       `json:"packages"`
	Symbols   []model.SymbolFact   `json:"symbols"`
	Relations []model.RelationFact `json:"relations"`
}

// Build accepts facts only through an explicit generation-bound verifier.
// An incomplete manifest or unavailable module is reported as a gap, never
// silently turned into proof of an empty workspace.
func Build(ctx context.Context, input Input) (*Report, error) {
	if ctx == nil || input.Snapshot == nil || input.Profile == nil || input.Scanner == nil || input.AuthorizeScope == nil || input.AuthorizePath == nil || input.VerifyFacts == nil ||
		input.PolicyEpoch < 1 || input.PolicyEpoch > 9007199254740991 || input.MaxManifestBytes < 1024 || input.MaxManifestBytes > 64*1024*1024 || input.MaxSourceBytes < 1 || input.MaxSourceBytes > 512*1024*1024 ||
		input.MaxFacts < 1 || input.MaxFacts > 100000 || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 16*1024*1024 || len(input.Packages)+len(input.Symbols)+len(input.Relations) > input.MaxFacts {
		return nil, ErrInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(input.Profile.BuildTags) > 128 || len(input.Profile.ModuleRoots) > 64 || len(input.Profile.DependencyRoots) > 128 {
		return nil, ErrInput
	}
	profileBytes := len(input.Profile.Language) + len(input.Profile.Goos) + len(input.Profile.Goarch) + len(input.Profile.ToolchainDigest) + len(input.Profile.ProviderDigest) + len(input.Profile.ModuleMode) + len(input.Profile.AnalysisMode)
	if profileBytes > input.MaxManifestBytes {
		return nil, ErrLimit
	}
	for _, values := range [][]string{input.Profile.BuildTags, input.Profile.ModuleRoots, input.Profile.DependencyRoots} {
		for _, value := range values {
			if len(value) > 16384 || len(value) > input.MaxManifestBytes-profileBytes {
				return nil, ErrLimit
			}
			profileBytes += len(value)
		}
	}
	rawProfile, err := json.Marshal(input.Profile)
	if err != nil || len(rawProfile) > input.MaxManifestBytes {
		return nil, ErrInput
	}
	profile, err := model.DecodeEffectiveProfileJSON(rawProfile, input.MaxManifestBytes)
	if err != nil {
		return nil, ErrInput
	}
	input.Profile, _ = profile.(*model.GoProfile)
	if input.Profile == nil {
		return nil, ErrInput
	}
	manifest := input.Snapshot.Manifest()
	profileID, err := model.ProfileDigest(input.Profile, input.MaxManifestBytes)
	if err != nil || profileID != manifest.ProfileID || input.Profile.ProviderDigest != manifest.ProviderDigest || model.VerifyCaptureDigest(manifest, input.MaxManifestBytes) != nil {
		return nil, ErrInput
	}
	scope := Scope{Identity: model.GraphIdentity{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: input.GenerationID}, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: input.PolicyEpoch}
	ref := model.ModuleRef{Kind: "module", GraphIdentity: scope.Identity, ModuleID: "check"}
	rawRef, err := json.Marshal(ref)
	if err != nil {
		return nil, ErrInput
	}
	if _, err := model.DecodeGraphRefJSON(rawRef, 16384); err != nil {
		return nil, ErrInput
	}
	if err := input.AuthorizeScope(ctx, scope); err != nil {
		return nil, authorityError(ctx)
	}
	f, digest, err := copyFacts(ctx, input, scope)
	if err != nil {
		return nil, err
	}
	r := &Report{scope: scope, groups: map[string][]Site{}, factsDigest: digest, authorizeScope: input.AuthorizeScope, authorizePath: input.AuthorizePath, verifyFacts: input.VerifyFacts, maxOutputBytes: input.MaxOutputBytes}
	if err := r.authorize(ctx, nil); err != nil {
		return nil, err
	}
	opened := map[string]snapshot.File{}
	var total int64
	open := func(filename string) (snapshot.File, error) {
		if err := ctx.Err(); err != nil {
			return snapshot.File{}, err
		}
		if policy.CheckPath(filename) != nil {
			return snapshot.File{}, ErrInput
		}
		if err := input.Scanner.CheckContent([]byte(filename)); err != nil {
			return snapshot.File{}, err
		}
		if err := input.AuthorizeScope(ctx, scope); err != nil {
			return snapshot.File{}, authorityError(ctx)
		}
		if err := input.AuthorizePath(ctx, scope, filename); err != nil {
			return snapshot.File{}, authorityError(ctx)
		}
		if f, ok := opened[filename]; ok {
			return f, nil
		}
		file, ok := input.Snapshot.File(filename)
		if !ok {
			return snapshot.File{}, ErrInput
		}
		if int64(file.Len()) > input.MaxSourceBytes-total {
			return snapshot.File{}, ErrLimit
		}
		total += int64(file.Len())
		if err := input.Scanner.CheckContent(file.Bytes()); err != nil {
			return snapshot.File{}, err
		}
		opened[filename] = file
		return file, nil
	}
	modules, gaps, boundaries, err := buildModules(ctx, input, scope, open)
	if err != nil {
		return nil, err
	}
	modulesByID := map[string]int{}
	for i, m := range modules {
		modulesByID[m.Ref.ModuleID] = i
	}
	packages := map[string]PackageInput{}
	filePackages := map[string]map[string]bool{}
	for _, p := range f.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mi, ok := modulesByID[p.Fact.ModuleKey]
		if !ok || p.Fact.InputDigest != scope.SnapshotDigest || packages[p.Fact.PackageKey].Fact.PackageKey != "" {
			return nil, ErrInput
		}
		m := modules[mi]
		if p.Fact.Variant != "production" && p.Fact.Variant != "internal_test" && p.Fact.Variant != "external_test" || p.Fact.Variant != "production" && !input.Profile.TestsEnabled {
			return nil, ErrInput
		}
		for _, filename := range p.Files {
			if !strings.HasSuffix(filename, ".go") {
				return nil, ErrInput
			}
			if p.Fact.Variant == "production" && strings.HasSuffix(filename, "_test.go") || p.Fact.Variant == "external_test" && !strings.HasSuffix(filename, "_test.go") {
				return nil, ErrInput
			}
			owner := ""
			for root := path.Dir(filename); ; root = path.Dir(root) {
				if _, found := boundaries[root]; found {
					owner = root
					break
				}
				if root == "." {
					break
				}
			}
			if owner != m.Root {
				return nil, ErrInput
			}
			dir := path.Dir(filename)
			suffix := strings.TrimPrefix(strings.TrimPrefix(dir, m.Root), "/")
			if m.Root == "." {
				suffix = dir
				if suffix == "." {
					suffix = ""
				}
			}
			want := m.ImportPath
			if suffix != "" {
				want += "/" + suffix
			}
			if p.Fact.Variant == "external_test" {
				want += "_test"
			}
			if p.Fact.ImportPath != want {
				return nil, ErrInput
			}
			if _, err := open(filename); err != nil {
				return nil, err
			}
			if filePackages[filename] == nil {
				filePackages[filename] = map[string]bool{}
			}
			filePackages[filename][p.Fact.PackageKey] = true
		}
		packages[p.Fact.PackageKey] = p
		modules[mi].Packages = append(modules[mi].Packages, Package{Fact: p.Fact, Files: append([]string{}, p.Files...)})
	}
	for i := range modules {
		sort.Slice(modules[i].Packages, func(a, b int) bool {
			return modules[i].Packages[a].Fact.PackageKey < modules[i].Packages[b].Fact.PackageKey
		})
		files := map[string]bool{}
		for _, pkg := range modules[i].Packages {
			for _, filename := range pkg.Files {
				files[filename] = true
			}
		}
		modules[i].Summary = fmt.Sprintf("Go module %s: %d package variants, %d captured files.", modules[i].ImportPath, len(modules[i].Packages), len(files))
	}
	groups, sites, err := buildGroups(ctx, scope, f, packages, filePackages, open, input.MaxOutputBytes)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		groups[i].Expand = Expansion{Scope: scope, FactsDigest: digest, GroupID: groups[i].GroupID}
	}
	r.groups = sites
	for filename := range opened {
		r.paths = append(r.paths, filename)
	}
	sort.Strings(r.paths)
	gaps = append(gaps, "ENTRYPOINTS_AND_TRUSTED_DOCUMENTS_NOT_COMPUTED", "SEMANTIC_COVERAGE_NOT_PROVEN")
	sort.Strings(gaps)
	capturedFiles, unavailableFiles := 0, 0
	for _, item := range manifest.Files {
		if item.Status == "captured" {
			capturedFiles++
		} else {
			unavailableFiles++
		}
	}
	r.summary = Summary{Scope: scope, Language: "go", CapturedFiles: capturedFiles, UnavailableFiles: unavailableFiles, RelationCount: len(f.Relations), Modules: modules, Groups: groups, Gaps: gaps}
	encoded, err := json.Marshal(r.summary)
	if err != nil || len(encoded) > input.MaxOutputBytes {
		return nil, ErrLimit
	}
	if err := r.authorize(ctx, r.paths); err != nil {
		return nil, err
	}
	return r, nil
}

// copyFacts bounds individual records before allocation, canonicalizes array
// order, and owns all nested pointers before a verifier or caller can use them.
func copyFacts(ctx context.Context, input Input, scope Scope) (facts, string, error) {
	result := facts{Packages: []PackageInput{}, Symbols: []model.SymbolFact{}, Relations: []model.RelationFact{}}
	used := 0
	members := 0
	checkedPaths := map[string]bool{}
	checkPath := func(filename string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if checkedPaths[filename] {
			return nil
		}
		if len(filename) > 16384 || policy.CheckPath(filename) != nil {
			return ErrInput
		}
		if err := input.Scanner.CheckContent([]byte(filename)); err != nil {
			return err
		}
		if err := input.AuthorizeScope(ctx, scope); err != nil {
			return authorityError(ctx)
		}
		if err := input.AuthorizePath(ctx, scope, filename); err != nil {
			return authorityError(ctx)
		}
		checkedPaths[filename] = true
		return nil
	}
	copyRecord := func(v any, dst any, validate func([]byte) error) error {
		raw, err := json.Marshal(v)
		if err != nil || len(raw) > input.MaxOutputBytes-used {
			return ErrLimit
		}
		used += len(raw)
		if err := input.Scanner.CheckContent(raw); err != nil {
			return err
		}
		if err := validate(raw); err != nil {
			return ErrInput
		}
		if json.Unmarshal(raw, dst) != nil {
			return ErrInput
		}
		return nil
	}
	for _, p := range input.Packages {
		if p.Files == nil || len(p.Files) == 0 || len(p.Files) > input.MaxFacts-members-len(input.Packages)-len(input.Symbols)-len(input.Relations) || len(p.Fact.PackageKey) > 768 || len(p.Fact.ModuleKey) > 768 || len(p.Fact.ImportPath) > 8192 || len(p.Fact.Variant) > 64 || len(p.Fact.InputDigest) > 64 || len(p.Fact.SemanticStatus) > 64 {
			return facts{}, "", ErrInput
		}
		members += len(p.Files)
		pathBytes := 0
		for _, filename := range p.Files {
			if err := checkPath(filename); err != nil {
				return facts{}, "", err
			}
			if len(filename) > input.MaxOutputBytes-used-pathBytes {
				return facts{}, "", ErrLimit
			}
			pathBytes += len(filename)
		}
		var c PackageInput
		if err := copyRecord(p, &c, func(raw []byte) error {
			b, e := json.Marshal(p.Fact)
			if e != nil {
				return e
			}
			_, e = model.DecodePackageFactJSON(b, input.MaxOutputBytes)
			return e
		}); err != nil {
			return facts{}, "", err
		}
		sort.Strings(c.Files)
		for i := 1; i < len(c.Files); i++ {
			if c.Files[i-1] == c.Files[i] {
				return facts{}, "", ErrInput
			}
		}
		result.Packages = append(result.Packages, c)
	}
	for _, s := range input.Symbols {
		if err := ctx.Err(); err != nil {
			return facts{}, "", err
		}
		if len(s.SymbolID) > 768 || len(s.ScopeKey) > 768 || len(s.Kind) > 64 || len(s.Name) > 2048 || len(s.QualifiedName) > 8192 || len(s.Signature) > 131072 || s.Path != nil && len(*s.Path) > 16384 || s.PackageKey != nil && len(*s.PackageKey) > 768 {
			return facts{}, "", ErrLimit
		}
		if s.Path != nil {
			if err := checkPath(*s.Path); err != nil {
				return facts{}, "", err
			}
		}
		var c model.SymbolFact
		if err := copyRecord(s, &c, func(raw []byte) error { _, e := model.DecodeSymbolFactJSON(raw, input.MaxOutputBytes); return e }); err != nil {
			return facts{}, "", err
		}
		result.Symbols = append(result.Symbols, c)
	}
	for _, relation := range input.Relations {
		if err := ctx.Err(); err != nil {
			return facts{}, "", err
		}
		if len(relation.RelationID) > 768 || len(relation.SourceSymbolID) > 768 || len(relation.SitePath) > 16384 || len(relation.Kind) > 64 || len(relation.EvidenceKind) > 64 || len(relation.Reason) > 4096 || relation.TargetSymbolID != nil && len(*relation.TargetSymbolID) > 768 || relation.UnresolvedLabel != nil && len(*relation.UnresolvedLabel) > 2048 {
			return facts{}, "", ErrLimit
		}
		if err := checkPath(relation.SitePath); err != nil {
			return facts{}, "", err
		}
		var c model.RelationFact
		if err := copyRecord(relation, &c, func(raw []byte) error { _, e := model.DecodeRelationFactJSON(raw, input.MaxOutputBytes); return e }); err != nil {
			return facts{}, "", err
		}
		result.Relations = append(result.Relations, c)
	}
	sort.Slice(result.Packages, func(i, j int) bool { return result.Packages[i].Fact.PackageKey < result.Packages[j].Fact.PackageKey })
	sort.Slice(result.Symbols, func(i, j int) bool { return result.Symbols[i].SymbolID < result.Symbols[j].SymbolID })
	sort.Slice(result.Relations, func(i, j int) bool { return result.Relations[i].RelationID < result.Relations[j].RelationID })
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > input.MaxOutputBytes {
		return facts{}, "", ErrLimit
	}
	sum := sha256.Sum256(append([]byte("mce4d-overview-facts-v1\x00"), raw...))
	return result, hex.EncodeToString(sum[:]), nil
}
