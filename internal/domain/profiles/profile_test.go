package profiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/federation"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/domain/state"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/history"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type businessFixture struct {
	scope   model.ScopeVector
	group   model.GroupSummary
	data    map[model.SourceRef][]byte
	owners  map[string]history.ApprovedStateModel
	plan    Plan
	read    Reader
	checked int
}

func uid(i int) string { return fmt.Sprintf("99000000-0000-4000-8000-%012d", i) }

func pointer[T any](v T) *T { return &v }

func hash(source []byte) string {
	digest := sha256.Sum256(source)
	return hex.EncodeToString(digest[:])
}

func graphID(m model.ScopeMember) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: m.GenerationID}
}

func (f *businessFixture) capture(member model.ScopeMember, path string, raw []byte) model.SourceRef {
	ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
		Path: path, ContentHash: hash(raw), Range: model.ByteRange{EndByte: int64(len(raw))}}
	f.data[ref] = raw
	return ref
}

func makeBusinessFixture(t *testing.T, domain string) *businessFixture {
	t.Helper()
	raw, err := os.ReadFile("../state/testdata/order.machine.json")
	if err != nil {
		t.Fatal(err)
	}
	b := &businessFixture{data: map[model.SourceRef][]byte{}, owners: map[string]history.ApprovedStateModel{}}
	groupID, catalogID := uid(70), uid(71)
	profile := domains[domain]
	b.group = model.GroupSummary{Status: "active", Revision: 1, Config: model.GroupConfig{GroupID: groupID, Name: domain}}
	b.scope = model.ScopeVector{Kind: "federated", GroupID: &groupID, GroupRevision: &b.group.Revision,
		CatalogIncarnation: catalogID, Members: []model.ScopeMember{}, UnavailableMembers: []model.MemberUnavailable{}}
	for i, role := range profile.roles {
		seq := i + 1
		member := model.ScopeMember{WorktreeID: uid(seq), ProfileID: hash([]byte("approved profile " + role)), GenerationID: uid(seq + 10),
			SnapshotDigest: hash([]byte("capture " + role)), PolicyDigest: hash([]byte("policy " + role)), PolicyEpoch: 1, BindingRevision: 1,
			RecordEpoch: 1, IndexIncarnation: uid(seq + 20), RecordsIncarnation: uid(seq + 30), ServiceID: pointer(role)}
		b.scope.Members = append(b.scope.Members, member)
		b.group.Config.Members = append(b.group.Config.Members, model.GroupMemberConfig{ServiceID: role, WorktreeID: member.WorktreeID,
			ProfileID: member.ProfileID, Namespace: model.ServiceNamespace{Protocol: "http", Environment: "fixture", ContractVersion: pointer("v1")}})
		b.plan.Machines = append(b.plan.Machines, MachineBinding{Role: role, RecordID: uid(seq + 40), ServiceID: member.ServiceID})
	}
	b.scope.VectorDigest, err = model.ComputeScopeVectorDigest(b.scope, scopeBytes)
	if err != nil {
		t.Fatal(err)
	}
	for i, binding := range b.plan.Machines {
		modelDef, err := state.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		modelDef.MachineID = domain + "-" + binding.Role
		for _, requirement := range profile.questions {
			if requirement.role != binding.Role || requirement.id == "settlement_finality" {
				continue
			}
			transition := modelDef.Transitions[0]
			transition.ID, transition.Event, transition.To = requirement.id, requirement.id, "pending"
			modelDef.Transitions = append(modelDef.Transitions, transition)
			modelDef.Events = append(modelDef.Events, requirement.id)
			modelDef.Obligations = append(modelDef.Obligations, state.Obligation{ID: requirement.id, Transition: transition.ID,
				Kind: requirement.kind, Status: "evidence"})
			b.plan.Obligations = append(b.plan.Obligations, ObligationBinding{Question: requirement.id, ObligationID: requirement.id})
		}
		encoded, err := json.Marshal(modelDef)
		if err != nil {
			t.Fatal(err)
		}
		modelDef, err = state.Decode(encoded)
		if err != nil {
			t.Fatal(err)
		}
		b.owners[binding.Role] = history.ApprovedStateModel{RecordID: binding.RecordID, RecordVersion: 2,
			ScopeDigest: b.scope.VectorDigest, Reachability: "unknown", Definition: modelDef,
			Source: b.capture(b.scope.Members[i], binding.Role+".machine.json", encoded)}
	}
	b.plan.Domain, b.plan.ScopeDigest, b.plan.ServiceID = domain, b.scope.VectorDigest, b.scope.Members[0].ServiceID
	rawPlan, err := json.Marshal(b.plan)
	if err != nil {
		t.Fatal(err)
	}
	b.plan.Declaration = b.capture(b.scope.Members[0], "review-profile.json", rawPlan)
	b.read = Reader{
		Observe:  func(ctx context.Context, _ model.ScopeVector) (model.ScopeVector, error) { return b.scope, ctx.Err() },
		ReadPlan: func(ctx context.Context, _ model.ScopeVector, _ string) (Plan, error) { return b.plan, ctx.Err() },
		ReadMachine: func(ctx context.Context, _ model.ScopeVector, binding MachineBinding) (history.ApprovedStateModel, error) {
			value, ok := b.owners[binding.Role]
			if !ok {
				return history.ApprovedStateModel{}, ErrUnavailable
			}
			return value, ctx.Err()
		},
		CheckSource: func(ctx context.Context, _ model.ScopeMember, ref model.SourceRef) error {
			b.checked++
			if err := ref.ValidateCapturedBytes(b.data[ref]); err != nil {
				return ErrUnavailable
			}
			return ctx.Err()
		},
	}
	return b
}

func TestC065TradingProfilesRequireDeclaredProofAndDoNotInferMoney(t *testing.T) {
	f := makeBusinessFixture(t, "trading")
	order := f.scope.Members[0]
	byteRef := f.capture(order, "order-amount.json", []byte(`{"unit":"base_units","currency":"JPY"}`))
	f.plan.Financial = []FinancialField{{ServiceID: order.ServiceID, Field: contracts.Field{
		Entity:    model.EntityRef{Kind: "entity", GraphIdentity: graphID(order), EntityID: "order-contract"},
		FieldPath: "amount", Direction: "input", Type: "integer", Unit: pointer("base_units"), Currency: pointer("JPY"), Declaration: byteRef}}}
	declared := f.plan.Financial[0]
	declared.Field.FieldPath, declared.Field.Currency, declared.Field.Precision = "fee", pointer("USD"), pointer(int64(3))
	declared.Field.Declaration = f.capture(order, "fee.json", []byte(`{"unit":"base_units","currency":"USD","precision":3}`))
	f.plan.Financial = append(f.plan.Financial, declared)
	result, err := f.read.Resolve(context.Background(), Request{Domain: "trading", Scope: f.scope})
	if err != nil || len(result.Machines) != 3 || len(result.Financial) != 2 ||
		result.Financial[0].Field.Precision != nil || *result.Financial[0].Field.Currency != "JPY" ||
		result.Financial[1].Field.Precision == nil || *result.Financial[1].Field.Precision != 3 || !result.Partial {
		t.Fatalf("trading view: %+v %v", result, err)
	}
	for _, item := range result.Obligations {
		if item.Question == "settlement_finality" {
			if item.Status != "unknown" || len(item.SourceRefs) != 0 {
				t.Fatalf("settlement invented: %+v", item)
			}
			continue
		}
		if item.Status != "evidence" || item.TransitionID == nil || len(item.SourceRefs) != 2 {
			t.Fatalf("lost trading declaration: %+v", item)
		}
	}
	t.Log("C065: reserve, retry, partial fill, cancel, ledger evidence; no inferred precision or settlement finality")
}

func TestC066CommerceKeepsIndependentMachinesAndVerifiedConsumers(t *testing.T) {
	f := makeBusinessFixture(t, "commerce")
	payment, stock := f.scope.Members[1], f.scope.Members[2]
	endpoint := func(m model.ScopeMember, label string) federation.ContractEndpoint {
		ref := &model.SymbolRef{Kind: "symbol", GraphIdentity: graphID(m), SymbolID: "same-action"}
		return federation.ContractEndpoint{Selector: model.RefSelector{Kind: "ref", Ref: ref, ServiceID: m.ServiceID},
			Contract: contracts.Key{ServiceID: m.ServiceID, Environment: "fixture", Protocol: "http", Namespace: "orders",
				Identifier: label, Version: "v1"}}
	}
	from, to := endpoint(payment, "POST /refund"), endpoint(stock, "POST /release")
	doc := f.capture(payment, "effects.bridge.json", []byte(`{"partial_refund":"release_reserved_quantity"}`))
	link := federation.DeclaredLink{ID: "refund-to-stock", From: from, To: to, Declaration: doc}
	flow := federation.FlowResolver{
		Resolver: federation.Resolver{
			Observe: f.read.Observe, CheckSource: f.read.CheckSource,
			ReadGroup: func(ctx context.Context, _ string) (model.GroupSummary, string, error) {
				return f.group, f.scope.CatalogIncarnation, ctx.Err()
			},
		},
		ReadEndpoint: func(ctx context.Context, m model.ScopeMember, ref model.GraphRef) (federation.FlowEndpointEvidence, error) {
			for _, bound := range []federation.ContractEndpoint{from, to} {
				if *m.ServiceID == *bound.Selector.ServiceID && reflect.DeepEqual(ref, bound.Selector.Ref) {
					return federation.FlowEndpointEvidence{Ref: ref, Contract: bound.Contract, Declaration: f.owners[*m.ServiceID].Source}, ctx.Err()
				}
			}
			return federation.FlowEndpointEvidence{}, federation.ErrFlowEndpointUnavailable
		},
		ReadLinks: func(ctx context.Context, _ model.ScopeVector) ([]federation.DeclaredLink, error) {
			return []federation.DeclaredLink{link}, ctx.Err()
		},
	}
	f.read.ReadFlow = flow.ResolveFlow
	req := Request{Domain: "commerce", Scope: f.scope, ImpactRoot: &from.Selector}
	result, err := f.read.Resolve(context.Background(), req)
	if err != nil || len(result.Machines) != 3 || result.Machines[0].Role != "order" ||
		result.Machines[1].Role != "payment" || result.Machines[2].Role != "stock" ||
		len(result.Consumers) != 1 || *result.Consumers[0].Ref.ServiceID != "stock" || result.Consumers[0].Source != f.owners["stock"].Source {
		t.Fatalf("commerce view: %+v %v", result, err)
	}
	for _, item := range result.Obligations {
		if item.Status != "evidence" || item.TransitionID == nil || *item.TransitionID != item.Question {
			t.Fatalf("incorrect lifecycle evidence: %+v", item)
		}
	}
	unset := f.read
	approved := unset.ReadMachine
	unset.ReadMachine = func(ctx context.Context, scope model.ScopeVector, binding MachineBinding) (history.ApprovedStateModel, error) {
		if binding.Role == "payment" {
			return history.ApprovedStateModel{}, history.ErrStateModelApproval
		}
		return approved(ctx, scope, binding)
	}
	unapproved, err := unset.Resolve(context.Background(), Request{Domain: "commerce", Scope: f.scope})
	if err != nil || !unapproved.Partial || len(unapproved.Machines) != 2 {
		t.Fatalf("unapproved payment machine supplied evidence: %+v %v", unapproved, err)
	}
	for _, item := range unapproved.Obligations {
		if item.Role == "payment" && (item.Status != "unknown" || len(item.SourceRefs) != 0) {
			t.Fatalf("unapproved payment supplied obligation: %+v", item)
		}
	}
	checked := f.checked
	f.scope.VectorDigest = hash([]byte("changed pin"))
	if out, err := f.read.Resolve(context.Background(), req); !errors.Is(err, ErrScope) || out.Domain != "" || f.checked != checked {
		t.Fatalf("stale scope produced data: %+v %v", out, err)
	}
	t.Log("C066: order/payment/stock separate, duplicate, out-of-order and partial refund declared, unapproved retained unknown, exact downstream stock consumer")
}
