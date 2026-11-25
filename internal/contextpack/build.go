package contextpack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type catalog struct {
	Packages  []model.PackageFact  `json:"packages"`
	Symbols   []model.SymbolFact   `json:"symbols"`
	Relations []model.RelationFact `json:"relations"`
	Coverage  model.Coverage       `json:"coverage"`
}

// Select starts from an exact symbol reference, never a fuzzy name. Search
// ambiguity and position resolution are handled by the search adapter first.
// The fact verifier must bind the supplied catalog to this member; callbacks
// are invoked before and after selection and before every subsequent read.
func Select(ctx context.Context, in Input) (*Selection, error) {
	if ctx == nil || in.Snapshot == nil || in.Scanner == nil || in.AuthorizeScope == nil || in.AuthorizePath == nil || in.VerifyFacts == nil ||
		in.MaxManifestBytes < 1024 || in.MaxManifestBytes > 64*1024*1024 || in.MaxFactBytes < 1024 || in.MaxFactBytes > 16*1024*1024 || in.MaxSourceBytes < 1 || in.MaxSourceBytes > 512*1024*1024 ||
		in.MaxFacts < 1 || in.MaxFacts > 100000 || len(in.Packages)+len(in.Symbols)+len(in.Relations) > in.MaxFacts || in.MaxNodes < 1 || in.MaxNodes > 100000 || in.MaxEdges < 1 || in.MaxEdges > 1000000 || in.MaxDepth < 0 || in.MaxDepth > 8 || in.MaxOutputBytes < 1024 || in.MaxOutputBytes > 16*1024*1024 {
		return nil, ErrInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model.VerifyScopeVectorDigest(in.Scope, in.MaxManifestBytes) != nil {
		return nil, ErrInput
	}
	rawScope, err := json.Marshal(in.Scope)
	if err != nil || len(rawScope) > in.MaxManifestBytes {
		return nil, ErrLimit
	}
	scope, err := model.DecodeScopeVectorJSON(rawScope, in.MaxManifestBytes)
	if err != nil {
		return nil, ErrInput
	}
	refBytes, err := json.Marshal(in.Target)
	if err != nil {
		return nil, ErrInput
	}
	ref, err := model.DecodeGraphRefJSON(refBytes, 16384)
	if err != nil {
		return nil, ErrInput
	}
	member, err := model.MatchPinnedMember(scope, ref, in.ServiceID)
	if err != nil {
		return nil, err
	}
	manifest := in.Snapshot.Manifest()
	if model.VerifyCaptureDigest(manifest, in.MaxManifestBytes) != nil || member.WorktreeID != manifest.WorktreeID || member.ProfileID != manifest.ProfileID || member.SnapshotDigest != manifest.SnapshotDigest || member.PolicyDigest != manifest.PolicyDigest {
		return nil, ErrInput
	}
	preScope, preMember, err := authorityCopy(scope, member)
	if err != nil {
		return nil, err
	}
	if err := in.AuthorizeScope(ctx, preScope, preMember); err != nil {
		return nil, authorityError(ctx)
	}

	// Bound scalar sizes before JSON allocation. The closed model decoders then
	// validate each item and the copies prevent later mutation by the caller.
	if len(in.Coverage.Capabilities) > 128 || len(in.Coverage.Gaps) > 64 || len(in.Coverage.ScopeDescription) > 4096 {
		return nil, ErrLimit
	}
	for _, gap := range in.Coverage.Gaps {
		if len(gap.Code) > 768 || len(gap.Reason) > 4096 || len(gap.SourceRefs) > 16 {
			return nil, ErrLimit
		}
		for _, r := range gap.SourceRefs {
			if len(r.Path) > 16384 {
				return nil, ErrLimit
			}
		}
	}
	for _, cap := range in.Coverage.Capabilities {
		if len(cap.Name) > 768 || len(cap.ProviderID) > 768 || len(cap.ProviderVersion) > 512 || len(cap.Status) > 64 {
			return nil, ErrLimit
		}
	}
	c := catalog{Packages: make([]model.PackageFact, 0, len(in.Packages)), Symbols: make([]model.SymbolFact, 0, len(in.Symbols)), Relations: make([]model.RelationFact, 0, len(in.Relations))}
	used := 0
	copyValue := func(value, output any, validate func([]byte) error) error {
		raw, e := json.Marshal(value)
		if e != nil || len(raw) > in.MaxFactBytes-used {
			return ErrLimit
		}
		used += len(raw)
		if e = in.Scanner.CheckContent(raw); e != nil {
			return e
		}
		if validate(raw) != nil || json.Unmarshal(raw, output) != nil {
			return ErrInput
		}
		return nil
	}
	if err := copyValue(in.Coverage, &c.Coverage, func(b []byte) error { _, e := model.DecodeCoverageJSON(b, in.MaxFactBytes); return e }); err != nil {
		return nil, err
	}
	for _, x := range in.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(x.PackageKey) > 768 || len(x.ImportPath) > 8192 || len(x.ModuleKey) > 768 || len(x.Variant) > 64 || len(x.InputDigest) > 64 || len(x.SemanticStatus) > 64 {
			return nil, ErrLimit
		}
		var item model.PackageFact
		if err := copyValue(x, &item, func(b []byte) error { _, e := model.DecodePackageFactJSON(b, in.MaxFactBytes); return e }); err != nil {
			return nil, err
		}
		if item.InputDigest != manifest.SnapshotDigest {
			return nil, ErrInput
		}
		c.Packages = append(c.Packages, item)
	}
	for _, x := range in.Symbols {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(x.SymbolID) > 768 || len(x.Kind) > 64 || len(x.Name) > 2048 || len(x.QualifiedName) > 8192 || len(x.ScopeKey) > 768 || len(x.Signature) > 131072 || x.Path != nil && len(*x.Path) > 16384 || x.PackageKey != nil && len(*x.PackageKey) > 768 {
			return nil, ErrLimit
		}
		var item model.SymbolFact
		if err := copyValue(x, &item, func(b []byte) error { _, e := model.DecodeSymbolFactJSON(b, in.MaxFactBytes); return e }); err != nil {
			return nil, err
		}
		c.Symbols = append(c.Symbols, item)
	}
	for _, x := range in.Relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(x.RelationID) > 768 || len(x.SourceSymbolID) > 768 || len(x.Kind) > 64 || len(x.EvidenceKind) > 64 || len(x.SitePath) > 16384 || len(x.Reason) > 4096 || x.TargetSymbolID != nil && len(*x.TargetSymbolID) > 768 || x.UnresolvedLabel != nil && len(*x.UnresolvedLabel) > 2048 {
			return nil, ErrLimit
		}
		var item model.RelationFact
		if err := copyValue(x, &item, func(b []byte) error { _, e := model.DecodeRelationFactJSON(b, in.MaxFactBytes); return e }); err != nil {
			return nil, err
		}
		c.Relations = append(c.Relations, item)
	}
	sort.Slice(c.Packages, func(i, j int) bool { return c.Packages[i].PackageKey < c.Packages[j].PackageKey })
	sort.Slice(c.Symbols, func(i, j int) bool { return c.Symbols[i].SymbolID < c.Symbols[j].SymbolID })
	sort.Slice(c.Relations, func(i, j int) bool { return c.Relations[i].RelationID < c.Relations[j].RelationID })
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > in.MaxFactBytes {
		return nil, ErrLimit
	}
	h := sha256.New()
	h.Write([]byte("context-facts-v1\x00"))
	h.Write([]byte(scope.VectorDigest))
	h.Write([]byte{0})
	h.Write(raw)
	digest := hex.EncodeToString(h.Sum(nil))
	s := &Selection{report: Report{Scope: scope, Target: in.Target, FactsDigest: digest, Coverage: c.Coverage, Nodes: []model.SymbolFact{}, Sources: []SourceRange{}, References: []Reference{}, Groups: []ReferenceGroup{}, Gaps: []model.Gap{}, CycleWitness: []string{}, EnumerationComplete: true}, member: member, snapshot: in.Snapshot, scanner: in.Scanner, scopeAuth: in.AuthorizeScope, pathAuth: in.AuthorizePath, verify: in.VerifyFacts, maxOutput: in.MaxOutputBytes, focused: in.Focused}
	if in.Focused {
		addGap(&s.report, "FOCUSED_CONTEXT", "Pagination covers only the selected symbol's bounded static neighborhood.")
	}
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	files := map[string]snapshot.File{}
	var total int64
	open := func(path string) (snapshot.File, error) {
		if err := ctx.Err(); err != nil {
			return snapshot.File{}, err
		}
		if file, ok := files[path]; ok {
			return file, nil
		}
		if policy.CheckPath(path) != nil {
			return snapshot.File{}, ErrInput
		}
		if err := in.Scanner.CheckContent([]byte(path)); err != nil {
			return snapshot.File{}, err
		}
		// The surrounding authorize calls pin and recheck the whole scope.
		// This private pass still checks each path before using its bytes.
		if err := s.checkPath(ctx, path); err != nil {
			return snapshot.File{}, err
		}
		file, ok := in.Snapshot.File(path)
		if !ok {
			return snapshot.File{}, ErrInput
		}
		if int64(file.Len()) > in.MaxSourceBytes-total {
			return snapshot.File{}, ErrLimit
		}
		total += int64(file.Len())
		if err := in.Scanner.CheckContent(file.Bytes()); err != nil {
			return snapshot.File{}, err
		}
		files[path] = file
		return file, nil
	}
	packages := map[string]model.PackageFact{}
	for _, p := range c.Packages {
		if _, ok := packages[p.PackageKey]; ok {
			return nil, ErrInput
		}
		packages[p.PackageKey] = p
	}
	symbols := map[string]model.SymbolFact{}
	for _, symbol := range c.Symbols {
		if _, ok := symbols[symbol.SymbolID]; ok {
			return nil, ErrInput
		}
		if symbol.Path != nil {
			if symbol.PackageKey == nil {
				return nil, ErrInput
			}
			if _, ok := packages[*symbol.PackageKey]; !ok {
				return nil, ErrInput
			}
			f, e := open(*symbol.Path)
			if e != nil {
				return nil, e
			}
			if symbol.Range == nil || symbol.Range.StartByte < 0 || symbol.Range.EndByte > int64(f.Len()) || f.CheckRange(int(symbol.Range.StartByte), int(symbol.Range.EndByte)) != nil {
				return nil, ErrInput
			}
		}
		symbols[symbol.SymbolID] = symbol
	}
	target, ok := symbols[in.Target.SymbolID]
	if !ok || target.Path == nil || target.Range == nil || target.Kind == "file" || target.Kind == "external" {
		return nil, ErrTarget
	}
	for i, rel := range c.Relations {
		if i > 0 && c.Relations[i-1].RelationID == rel.RelationID {
			return nil, ErrInput
		}
		if _, ok := symbols[rel.SourceSymbolID]; !ok {
			return nil, ErrInput
		}
		if rel.TargetSymbolID != nil {
			if _, ok := symbols[*rel.TargetSymbolID]; !ok {
				return nil, ErrInput
			}
		}
		file, e := open(rel.SitePath)
		if e != nil {
			return nil, e
		}
		if rel.Range.StartByte < 0 || rel.Range.EndByte > int64(file.Len()) || file.CheckRange(int(rel.Range.StartByte), int(rel.Range.EndByte)) != nil {
			return nil, ErrInput
		}
	}
	for _, gap := range c.Coverage.Gaps {
		for _, ref := range gap.SourceRefs {
			if err := snapshot.ValidateCapturedSource(scope, ref, in.ServiceID, in.Snapshot, in.MaxManifestBytes); err != nil {
				return nil, ErrInput
			}
			if _, err := open(ref.Path); err != nil {
				return nil, err
			}
		}
	}
	for path := range files {
		s.paths = append(s.paths, path)
	}
	sort.Strings(s.paths)
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	if err := walk(ctx, s, &in, c, symbols, files, target); err != nil {
		return nil, err
	}
	orderReport(&s.report)
	output, err := json.Marshal(s.report)
	if err != nil || len(output) > in.MaxOutputBytes {
		return nil, ErrLimit
	}
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return s, nil
}
