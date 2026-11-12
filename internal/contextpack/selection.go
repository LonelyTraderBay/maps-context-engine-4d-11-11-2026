// Package contextpack selects evidence from an immutable, explicitly pinned
// capture. Selection is not a public pack, a cursor, or proof of freshness.
package contextpack

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrInput     = errors.New("invalid context selection input")
	ErrLimit     = errors.New("context selection resource limit")
	ErrAuthority = errors.New("context selection authority could not be verified")
	ErrTarget    = errors.New("context selection target unavailable")
)

// Input supplies one complete fact catalog for one member of a verified vector.
// VerifyFacts must establish that the exact digest belongs to the published
// generation; neither a digest nor this package provides authorization.
type Input struct {
	Scope            model.ScopeVector
	Target           model.SymbolRef
	ServiceID        *string
	Snapshot         *snapshot.Snapshot
	Packages         []model.PackageFact
	Symbols          []model.SymbolFact
	Relations        []model.RelationFact
	Coverage         model.Coverage
	Scanner          *policy.Scanner
	AuthorizeScope   func(context.Context, model.ScopeVector, model.ScopeMember) error
	AuthorizePath    func(context.Context, model.ScopeMember, string) error
	VerifyFacts      func(context.Context, model.ScopeMember, string) error
	MaxManifestBytes int
	MaxFactBytes     int
	MaxSourceBytes   int64
	MaxFacts         int
	MaxNodes         int
	MaxEdges         int
	MaxDepth         int
	MaxOutputBytes   int
}

// Reference retains the original site and evidence; candidate dispatch is
// never represented as an observed or exact concrete call.
type Reference struct {
	Relation       model.RelationFact `json:"relation"`
	Source         model.SourceRef    `json:"source"`
	Classification string             `json:"classification"` // static, candidate, interface, unresolved
}

// SourceRange is a merged source descriptor. Relation sites remain separate
// in References, so merging does not discard their semantic evidence.
type SourceRange struct {
	Source   model.SourceRef `json:"source"`
	Priority int             `json:"priority"` // target, references, tests, other declarations
	Reasons  []string        `json:"reasons"`
}

// ReferenceGroup summarizes only the selected references, by their provider
// package's module key and relation/evidence kind. It is not a completeness
// claim and an empty module denotes an unresolved or external boundary.
type ReferenceGroup struct {
	SourceModuleKey string `json:"source_module_key"`
	TargetModuleKey string `json:"target_module_key"`
	RelationKind    string `json:"relation_kind"`
	EvidenceKind    string `json:"evidence_kind"`
	Classification  string `json:"classification"`
	SelectedCount   int    `json:"selected_count"`
}

type Report struct {
	Scope               model.ScopeVector  `json:"scope"`
	Target              model.SymbolRef    `json:"target"`
	FactsDigest         string             `json:"facts_digest"`
	Coverage            model.Coverage     `json:"coverage"`
	Nodes               []model.SymbolFact `json:"nodes"`
	Sources             []SourceRange      `json:"sources"`
	References          []Reference        `json:"references"`
	Groups              []ReferenceGroup   `json:"groups"`
	Gaps                []model.Gap        `json:"gaps"`
	CycleWitness        []string           `json:"cycle_witness"`
	EnumerationComplete bool               `json:"enumeration_complete"`
	// No editing readiness or live-workspace freshness is asserted here.
}

type Selection struct {
	report    Report
	member    model.ScopeMember
	snapshot  *snapshot.Snapshot
	scanner   *policy.Scanner
	scopeAuth func(context.Context, model.ScopeVector, model.ScopeMember) error
	pathAuth  func(context.Context, model.ScopeMember, string) error
	verify    func(context.Context, model.ScopeMember, string) error
	paths     []string
	maxOutput int
}

func authorityError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrAuthority
}

func authorityCopy(scope model.ScopeVector, member model.ScopeMember) (model.ScopeVector, model.ScopeMember, error) {
	raw, err := json.Marshal(struct {
		Scope  model.ScopeVector
		Member model.ScopeMember
	}{scope, member})
	if err != nil {
		return model.ScopeVector{}, model.ScopeMember{}, ErrInput
	}
	var out struct {
		Scope  model.ScopeVector
		Member model.ScopeMember
	}
	if json.Unmarshal(raw, &out) != nil {
		return model.ScopeVector{}, model.ScopeMember{}, ErrInput
	}
	return out.Scope, out.Member, nil
}

func (s *Selection) checkScope(ctx context.Context) error {
	scope, member, err := authorityCopy(s.report.Scope, s.member)
	if err != nil {
		return err
	}
	if err := s.scopeAuth(ctx, scope, member); err != nil {
		return authorityError(ctx)
	}
	return ctx.Err()
}

func (s *Selection) checkPath(ctx context.Context, path string) error {
	_, member, err := authorityCopy(s.report.Scope, s.member)
	if err != nil {
		return err
	}
	if err := s.pathAuth(ctx, member, path); err != nil {
		return authorityError(ctx)
	}
	return ctx.Err()
}

func (s *Selection) checkFacts(ctx context.Context) error {
	_, member, err := authorityCopy(s.report.Scope, s.member)
	if err != nil {
		return err
	}
	if err := s.verify(ctx, member, s.report.FactsDigest); err != nil {
		return authorityError(ctx)
	}
	return ctx.Err()
}

func (s *Selection) authorize(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkScope(ctx); err != nil {
		return err
	}
	if err := s.checkFacts(ctx); err != nil {
		return err
	}
	for _, path := range s.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.checkPath(ctx, path); err != nil {
			return err
		}
	}
	if err := s.checkFacts(ctx); err != nil {
		return err
	}
	if err := s.checkScope(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

// Read returns a defensive copy after checking every contributing path and
// the generation-bound catalog. Limit gaps mean enumeration is incomplete.
func (s *Selection) Read(ctx context.Context) (Report, error) {
	if err := s.authorize(ctx); err != nil {
		return Report{}, err
	}
	raw, err := json.Marshal(s.report)
	if err != nil || len(raw) > s.maxOutput {
		return Report{}, ErrLimit
	}
	var result Report
	if json.Unmarshal(raw, &result) != nil {
		return Report{}, ErrInput
	}
	if err := s.authorize(ctx); err != nil {
		return Report{}, err
	}
	return result, nil
}

type SourceContent struct {
	Source  model.SourceRef `json:"source"`
	Content string          `json:"content"`
}

// ReadSources returns only the selected captured ranges. It does not read the
// worktree and cannot establish that bytes are still current on disk.
func (s *Selection) ReadSources(ctx context.Context) ([]SourceContent, error) {
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	result := make([]SourceContent, 0, len(s.report.Sources))
	used := 2
	for _, item := range s.report.Sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, ok := s.snapshot.File(item.Source.Path)
		if !ok || item.Source.ContentHash != file.ContentHash() {
			return nil, ErrInput
		}
		if err := s.scanner.CheckContent(file.Bytes()); err != nil {
			return nil, err
		}
		body, err := file.Range(int(item.Source.Range.StartByte), int(item.Source.Range.EndByte))
		if err != nil {
			return nil, ErrInput
		}
		entry := SourceContent{Source: item.Source, Content: string(body)}
		raw, err := json.Marshal(entry)
		if err != nil || len(raw)+1 > s.maxOutput-used {
			return nil, ErrLimit
		}
		used += len(raw) + 1
		result = append(result, entry)
	}
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func addGap(report *Report, code, reason string) {
	for _, gap := range report.Gaps {
		if gap.Code == code {
			return
		}
	}
	report.Gaps = append(report.Gaps, model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{}})
}

func orderReport(report *Report) {
	sort.Slice(report.References, func(i, j int) bool {
		a, b := report.References[i].Relation, report.References[j].Relation
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.SitePath != b.SitePath {
			return a.SitePath < b.SitePath
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		return a.RelationID < b.RelationID
	})
	sort.Slice(report.Gaps, func(i, j int) bool { return report.Gaps[i].Code < report.Gaps[j].Code })
}
