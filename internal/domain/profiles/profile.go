// Package profiles projects approved, read-only business review context.
package profiles

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/federation"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/domain/state"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/history"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const scopeBytes = 256 << 10

var (
	ErrInput       = errors.New("invalid business profile request")
	ErrUnavailable = errors.New("approved business profile evidence is unavailable")
	ErrScope       = errors.New("business profile scope changed")
)

type Request struct {
	Domain     string
	Scope      model.ScopeVector
	ImpactRoot *model.RefSelector
}

type MachineBinding struct {
	Role      string
	RecordID  string
	ServiceID *string
}

type ObligationBinding struct {
	Question     string
	ObligationID string
}

type FinancialField struct {
	ServiceID *string
	Field     contracts.Field
}

// Plan is supplied by the trusted owner from the approved captured profile.
// A requester selects a domain and scope through Request.
type Plan struct {
	Domain      string
	ScopeDigest string
	Declaration model.SourceRef
	ServiceID   *string
	Machines    []MachineBinding
	Obligations []ObligationBinding
	Financial   []FinancialField
}

type Machine struct {
	Role          string
	RecordID      string
	RecordVersion int64
	MachineID     string
	States        []string
	Source        model.SourceRef
}

type Obligation struct {
	Question     string
	Role         string
	Status       string
	TransitionID *string
	Reason       *string
	SourceRefs   []model.SourceRef
}

type Consumer struct {
	Ref    model.RefSelector
	Source model.SourceRef
	Bridge model.SourceRef
}

type Result struct {
	Domain      string
	Scope       model.ScopeVector
	Machines    []Machine
	Obligations []Obligation
	Financial   []FinancialField
	Consumers   []Consumer
	Gaps        []model.Gap
	Partial     bool
}

type Reader struct {
	Observe     func(context.Context, model.ScopeVector) (model.ScopeVector, error)
	ReadPlan    func(context.Context, model.ScopeVector, string) (Plan, error)
	ReadMachine func(context.Context, model.ScopeVector, MachineBinding) (history.ApprovedStateModel, error)
	CheckSource func(context.Context, model.ScopeMember, model.SourceRef) error
	ReadFlow    func(context.Context, federation.FlowRequest) (federation.FlowResult, error)
}

type question struct{ id, role, kind string }

type domainProfile struct {
	roles     []string
	questions []question
}

var domains = map[string]domainProfile{
	"trading": {
		roles: []string{"order", "execution", "ledger"},
		questions: []question{
			{"reserve_atomicity", "order", "atomicity"},
			{"retry_idempotency", "order", "idempotency"},
			{"partial_fill_accounting", "execution", "atomicity"},
			{"cancel_release", "order", "compensation"},
			{"ledger_reconciliation", "ledger", "reconciliation"},
			{"settlement_finality", "ledger", "audit"},
		},
	},
	"commerce": {
		roles: []string{"order", "payment", "stock"},
		questions: []question{
			{"order_payment_projection", "order", "audit"},
			{"payment_webhook_duplicate", "payment", "idempotency"},
			{"payment_webhook_order", "payment", "reconciliation"},
			{"partial_refund", "payment", "compensation"},
			{"stock_reservation", "stock", "atomicity"},
		},
	},
}

func (r Reader) Resolve(ctx context.Context, req Request) (Result, error) {
	definition, valid := domains[req.Domain]
	if ctx == nil || !valid || r.Observe == nil || r.ReadPlan == nil || r.ReadMachine == nil || r.CheckSource == nil ||
		model.VerifyScopeVectorDigest(req.Scope, scopeBytes) != nil || (req.ImpactRoot != nil && req.Scope.Kind != "federated") {
		return Result{}, ErrInput
	}
	scope, err := model.NormalizeScopeVector(req.Scope, scopeBytes)
	if err != nil {
		return Result{}, ErrInput
	}
	if err := r.recheck(ctx, scope); err != nil {
		return Result{}, err
	}
	out := Result{Domain: req.Domain, Scope: scope, Machines: []Machine{}, Obligations: []Obligation{},
		Financial: []FinancialField{}, Consumers: []Consumer{}, Gaps: []model.Gap{}}
	if len(scope.UnavailableMembers) != 0 {
		out.gap("PROFILE_MEMBER_UNAVAILABLE")
	}
	if len(scope.Members) == 0 {
		if err := out.addObligations(definition, Plan{}, nil); err != nil {
			return Result{}, err
		}
		if err := r.recheck(ctx, scope); err != nil {
			return Result{}, err
		}
		return out, nil
	}
	plan, err := r.ReadPlan(ctx, scope, req.Domain)
	if errors.Is(err, ErrUnavailable) {
		out.gap("PROFILE_PLAN_UNAVAILABLE")
		if err := out.addObligations(definition, Plan{}, nil); err != nil {
			return Result{}, err
		}
		if err := r.recheck(ctx, scope); err != nil {
			return Result{}, err
		}
		return out, nil
	}
	if err != nil {
		return Result{}, err
	}
	if plan.Domain != req.Domain || plan.ScopeDigest != scope.VectorDigest ||
		len(plan.Machines) > len(definition.roles) || len(plan.Obligations) > len(definition.questions) || len(plan.Financial) > 128 {
		return Result{}, ErrInput
	}
	if err := r.source(ctx, scope, plan.ServiceID, plan.Declaration); err != nil {
		return Result{}, err
	}
	machines, err := r.machines(ctx, &out, definition, plan.Machines)
	if err != nil {
		return Result{}, err
	}
	if err := out.addObligations(definition, plan, machines); err != nil {
		return Result{}, err
	}
	if err := r.financial(ctx, &out, plan.Financial); err != nil {
		return Result{}, err
	}
	if req.ImpactRoot != nil {
		if err := r.impact(ctx, &out, *req.ImpactRoot); err != nil {
			return Result{}, err
		}
	}
	if err := r.recheck(ctx, scope); err != nil {
		return Result{}, err
	}
	return out, nil
}

func (r Reader) recheck(ctx context.Context, scope model.ScopeVector) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	observed, err := r.Observe(ctx, scope)
	if err != nil {
		return err
	}
	if err := model.ComparePinnedScope(scope, observed, scopeBytes); err != nil {
		return ErrScope
	}
	return ctx.Err()
}

func (r Reader) source(ctx context.Context, scope model.ScopeVector, service *string, ref model.SourceRef) error {
	member, err := model.MatchPinnedSource(scope, ref, service, scopeBytes)
	if err != nil {
		return ErrUnavailable
	}
	return r.CheckSource(ctx, member, ref)
}

func (r Reader) machines(ctx context.Context, out *Result, profile domainProfile, input []MachineBinding) (map[string]history.ApprovedStateModel, error) {
	selected := slices.Clone(input)
	slices.SortFunc(selected, func(a, b MachineBinding) int { return strings.Compare(a.Role, b.Role) })
	models := map[string]history.ApprovedStateModel{}
	identities := map[string]bool{}
	for i, binding := range selected {
		if !slices.Contains(profile.roles, binding.Role) || !model.ValidUUID(binding.RecordID) ||
			(i > 0 && selected[i-1].Role == binding.Role) || (out.Scope.Kind == "federated" && binding.ServiceID == nil) {
			return nil, ErrInput
		}
		item, err := r.ReadMachine(ctx, out.Scope, binding)
		if errors.Is(err, ErrUnavailable) || errors.Is(err, history.ErrStateModelApproval) {
			out.gap("PROFILE_MACHINE_UNAVAILABLE")
			continue
		}
		if err != nil {
			return nil, err
		}
		if item.RecordID != binding.RecordID || item.RecordVersion < 1 || item.ScopeDigest != out.Scope.VectorDigest ||
			item.Definition.Format != state.Format || item.Definition.MachineID == "" || len(item.Definition.States) > 256 {
			return nil, ErrInput
		}
		if err := r.source(ctx, out.Scope, binding.ServiceID, item.Source); err != nil {
			return nil, err
		}
		key := item.Source.WorktreeID + "/" + item.Source.ProfileID + "/" + item.Definition.MachineID
		if identities[key] {
			return nil, ErrInput
		}
		identities[key] = true
		states := make([]string, 0, len(item.Definition.States))
		for _, step := range item.Definition.States {
			states = append(states, step.ID)
		}
		out.Machines = append(out.Machines, Machine{Role: binding.Role, RecordID: item.RecordID, RecordVersion: item.RecordVersion,
			MachineID: item.Definition.MachineID, States: states, Source: item.Source})
		models[binding.Role] = item
	}
	if len(models) != len(profile.roles) {
		out.gap("PROFILE_MACHINE_UNAVAILABLE")
	}
	return models, nil
}

func (out *Result) addObligations(profile domainProfile, plan Plan, machines map[string]history.ApprovedStateModel) error {
	known := map[string]bool{}
	for _, expected := range profile.questions {
		known[expected.id] = true
	}
	bindings := map[string]string{}
	for _, entry := range plan.Obligations {
		if !known[entry.Question] || entry.ObligationID == "" || len(entry.ObligationID) > 128 || bindings[entry.Question] != "" {
			return ErrInput
		}
		bindings[entry.Question] = entry.ObligationID
	}
	for _, expected := range profile.questions {
		item := Obligation{Question: expected.id, Role: expected.role, Status: "unknown", SourceRefs: []model.SourceRef{}}
		if approved, ok := machines[expected.role]; ok {
			id := bindings[expected.id]
			for _, declared := range approved.Definition.Obligations {
				if declared.ID == id && declared.Kind == expected.kind && declared.Transition != "" {
					item.Status, item.Reason = declared.Status, declared.Reason
					item.TransitionID, item.SourceRefs = &declared.Transition, []model.SourceRef{plan.Declaration, approved.Source}
					break
				}
			}
		}
		if item.Status == "unknown" {
			out.gap("PROFILE_OBLIGATION_UNVERIFIED")
		}
		out.Obligations = append(out.Obligations, item)
	}
	return nil
}

func (r Reader) financial(ctx context.Context, out *Result, fields []FinancialField) error {
	for _, entry := range fields {
		field := entry.Field
		raw, err := json.Marshal(contracts.Batch{SchemaVersion: 1, Entities: []contracts.Entity{},
			Fields: []contracts.Field{field}, Links: []contracts.Link{}})
		if err != nil {
			return ErrInput
		}
		batch, err := contracts.DecodeBatch(raw)
		if err != nil || len(batch.Fields) != 1 ||
			(field.Unit == nil && field.Currency == nil && field.Precision == nil) {
			return ErrInput
		}
		if err := r.source(ctx, out.Scope, entry.ServiceID, field.Declaration); err != nil {
			return err
		}
		out.Financial = append(out.Financial, entry)
	}
	return nil
}

func (r Reader) impact(ctx context.Context, out *Result, root model.RefSelector) error {
	if r.ReadFlow == nil {
		out.gap("PROFILE_IMPACT_UNAVAILABLE")
		return nil
	}
	found, err := r.ReadFlow(ctx, federation.FlowRequest{Pinned: out.Scope, Root: root, Direction: "forward", MaxDepth: 8, Limit: 100})
	if errors.Is(err, ErrUnavailable) {
		out.gap("PROFILE_IMPACT_UNAVAILABLE")
		return nil
	}
	if err != nil {
		return err
	}
	if model.ComparePinnedScope(out.Scope, found.Scope, scopeBytes) != nil || len(found.Links) > 100 {
		return ErrScope
	}
	if found.Root == nil || found.Partial {
		out.gap("PROFILE_IMPACT_UNAVAILABLE")
	}
	if found.Root == nil {
		if len(found.Links) != 0 {
			return ErrInput
		}
		return nil
	}
	if !reflect.DeepEqual(root, *found.Root) {
		return ErrScope
	}
	for _, link := range found.Links {
		if err := r.source(ctx, out.Scope, link.From.Selector.ServiceID, link.Declaration); err != nil {
			return err
		}
		if err := r.source(ctx, out.Scope, link.To.Selector.ServiceID, link.ToSource); err != nil {
			return err
		}
		out.Consumers = append(out.Consumers, Consumer{Ref: link.To.Selector, Source: link.ToSource, Bridge: link.Declaration})
	}
	return nil
}

func (out *Result) gap(code string) {
	for _, prior := range out.Gaps {
		if prior.Code == code {
			return
		}
	}
	out.Partial = true
	out.Gaps = append(out.Gaps, model.Gap{Code: code, Reason: "Some requested review evidence is unavailable.", SourceRefs: []model.SourceRef{}})
}
