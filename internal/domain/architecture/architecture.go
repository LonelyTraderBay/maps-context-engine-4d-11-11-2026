// Package architecture checks approved intended dependencies against retained
// static evidence. It does not change repository source or architecture.
package architecture

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	sourceBytes = 64 << 10
	scopeBytes  = 256 << 10
	maxEdges    = 4096
)

var (
	ErrInput    = errors.New("invalid intended architecture declaration")
	ErrApproval = errors.New("current approved architecture evidence is unavailable")
	ErrScope    = errors.New("architecture comparison scope changed")
	ErrLimit    = errors.New("architecture analysis exceeds its budget")
)

//go:embed architecture.v1.schema.json
var declarationSchema []byte

var schema = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	var doc jsonschema.Schema
	if err := json.Unmarshal(declarationSchema, &doc); err != nil {
		return nil, err
	}
	return doc.Resolve(nil)
})

type Component struct {
	ID         string  `json:"id"`
	ServiceID  *string `json:"service_id"`
	WorktreeID string  `json:"worktree_id"`
	ProfileID  *string `json:"profile_id"`
	ModuleID   string  `json:"module_id"`
	Intended   string  `json:"intended"`
}

type Rule struct {
	ID         string   `json:"id"`
	From       string   `json:"from"`
	Allow      []string `json:"allow"`
	Transitive bool     `json:"transitive"`
	Relation   string   `json:"relation"`
	Severity   string   `json:"severity"`
	Rationale  string   `json:"rationale"`
}

type Exemption struct {
	ID        string `json:"id"`
	RuleID    string `json:"rule_id"`
	Target    string `json:"target"`
	ExpiresAt string `json:"expires_at"`
	Rationale string `json:"rationale"`
}

type definition struct {
	Format     string      `json:"format"`
	Components []Component `json:"components"`
	Rules      []Rule      `json:"rules"`
	Exemptions []Exemption `json:"exemptions"`
}

type Module struct {
	ServiceID *string
	Ref       model.ModuleRef
}

type Dependency struct {
	From, To Module
	Relation string
	Evidence string
	Site     model.SourceRef
}

type Coverage struct {
	Member model.ScopeMember
	Status string
}

type Observation struct {
	Scope        model.ScopeVector
	Coverage     []Coverage
	Modules      []Module
	Dependencies []Dependency
}

type Selection struct {
	Scope     model.ScopeVector
	ServiceID *string
	RecordID  string
}

type ComponentStatus struct {
	Component Component
	Observed  string
	Module    *Module
}

type Finding struct {
	RuleID      string
	Target      string
	Severity    string
	Rationale   string
	Status      string
	Witness     []Dependency
	Exemption   *Exemption
	Declaration model.SourceRef
}

type Report struct {
	Scope      model.ScopeVector
	Approval   model.RecordSummary
	Source     model.SourceRef
	Components []ComponentStatus
	Findings   []Finding
	Gaps       []model.Gap
	Status     string
	CheckedAt  string
}

type Reader struct {
	Observe      func(context.Context, model.ScopeVector) (model.ScopeVector, error)
	ReadApproval func(context.Context, model.ScopeVector, string) (model.RecordSummary, error)
	ReadSource   func(context.Context, model.ScopeMember, model.SourceRef, int) ([]byte, error)
	CheckSource  func(context.Context, model.ScopeMember, model.SourceRef) error
	ReadGraph    func(context.Context, model.ScopeVector, int) (Observation, error)
	Clock        func() time.Time
}

func (r Reader) Check(ctx context.Context, input Selection) (Report, error) {
	if ctx == nil || r.Observe == nil || r.ReadApproval == nil || r.ReadSource == nil || r.CheckSource == nil ||
		r.ReadGraph == nil || !model.ValidUUID(input.RecordID) || model.VerifyScopeVectorDigest(input.Scope, scopeBytes) != nil ||
		(input.Scope.Kind == "federated" && input.ServiceID == nil) {
		return Report{}, ErrInput
	}
	scope, err := model.NormalizeScopeVector(input.Scope, scopeBytes)
	if err != nil {
		return Report{}, ErrInput
	}
	if err := r.recheck(ctx, scope); err != nil {
		return Report{}, err
	}
	record, err := r.approval(ctx, scope, input.RecordID)
	if err != nil {
		return Report{}, err
	}
	ref := record.SourceRefs[0]
	member, err := model.MatchPinnedSource(scope, ref, input.ServiceID, scopeBytes)
	if err != nil {
		return Report{}, ErrApproval
	}
	raw, err := r.ReadSource(ctx, member, ref, sourceBytes)
	if err != nil {
		return Report{}, err
	}
	if len(raw) > sourceBytes || int64(len(raw)) != ref.Range.EndByte || ref.ValidateCapturedBytes(raw) != nil {
		return Report{}, ErrInput
	}
	def, err := decode(raw)
	if err != nil {
		return Report{}, err
	}
	observation, err := r.ReadGraph(ctx, scope, maxEdges)
	if err != nil {
		return Report{}, err
	}
	if model.ComparePinnedScope(scope, observation.Scope, scopeBytes) != nil {
		return Report{}, ErrScope
	}
	now := time.Now().UTC()
	if r.Clock != nil {
		now = r.Clock().UTC()
	}
	out := Report{Scope: scope, Approval: record, Source: ref, Components: []ComponentStatus{},
		Findings: []Finding{}, Gaps: []model.Gap{}, CheckedAt: now.Format(time.RFC3339Nano), Status: "supported_scope_complete"}
	if err := r.evaluate(ctx, &out, def, observation, now); err != nil {
		return Report{}, err
	}
	after, err := r.approval(ctx, scope, input.RecordID)
	if err != nil {
		return Report{}, err
	}
	if !reflect.DeepEqual(record, after) {
		return Report{}, ErrApproval
	}
	if err := r.recheck(ctx, scope); err != nil {
		return Report{}, err
	}
	return out, nil
}

func (r Reader) approval(ctx context.Context, scope model.ScopeVector, id string) (model.RecordSummary, error) {
	record, err := r.ReadApproval(ctx, scope, id)
	if err != nil {
		return model.RecordSummary{}, err
	}
	if record.RecordID != id || record.Version < 1 || record.RecordKind != "intended_architecture" ||
		record.DeclaredStatus != "approved" || record.EffectiveStatus != "approved" ||
		record.ProvenanceStatus != "retained_snapshot" || len(record.SourceRefs) != 1 {
		return model.RecordSummary{}, ErrApproval
	}
	ref := record.SourceRefs[0]
	if !strings.HasSuffix(ref.Path, ".architecture.json") || ref.Range.StartByte != 0 ||
		ref.Range.EndByte < 1 || ref.Range.EndByte > sourceBytes {
		return model.RecordSummary{}, ErrApproval
	}
	return record, nil
}

func (r Reader) recheck(ctx context.Context, scope model.ScopeVector) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	seen, err := r.Observe(ctx, scope)
	if err != nil {
		return err
	}
	if model.ComparePinnedScope(scope, seen, scopeBytes) != nil {
		return ErrScope
	}
	return ctx.Err()
}

func decode(raw []byte) (definition, error) {
	if _, err := model.DecodeIdentityJSON(raw, sourceBytes); err != nil {
		return definition{}, ErrInput
	}
	resolved, err := schema()
	if err != nil {
		return definition{}, ErrInput
	}
	var value any
	var def definition
	if json.Unmarshal(raw, &value) != nil || resolved.Validate(value) != nil ||
		json.Unmarshal(raw, &def) != nil || def.Format != "mce4d-architecture-v1" {
		return definition{}, ErrInput
	}
	components, rules := map[string]bool{}, map[string]bool{}
	for _, component := range def.Components {
		if !model.ValidUUID(component.WorktreeID) || components[component.ID] {
			return definition{}, ErrInput
		}
		components[component.ID] = true
	}
	for _, rule := range def.Rules {
		if !components[rule.From] || rules[rule.ID] {
			return definition{}, ErrInput
		}
		for _, permitted := range rule.Allow {
			if !components[permitted] {
				return definition{}, ErrInput
			}
		}
		rules[rule.ID] = true
	}
	exemptions := map[string]bool{}
	keys := map[[2]string]bool{}
	for _, item := range def.Exemptions {
		key := [2]string{item.RuleID, item.Target}
		_, err := time.Parse(time.RFC3339Nano, item.ExpiresAt)
		if err != nil || !strings.HasSuffix(item.ExpiresAt, "Z") || !rules[item.RuleID] || !components[item.Target] ||
			exemptions[item.ID] || keys[key] {
			return definition{}, ErrInput
		}
		keys[key], exemptions[item.ID] = true, true
	}
	slices.SortFunc(def.Components, func(a, b Component) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(def.Rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	return def, nil
}

func (out *Report) gap(code, reason string) {
	for _, item := range out.Gaps {
		if item.Code == code {
			return
		}
	}
	out.Status = "incomplete"
	out.Gaps = append(out.Gaps, model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{out.Source}})
}
