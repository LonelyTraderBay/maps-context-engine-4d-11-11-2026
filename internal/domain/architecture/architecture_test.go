package architecture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func uid(i int) string { return fmt.Sprintf("82000000-0000-4000-8000-%012d", i) }

func hash(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func pointer[T any](v T) *T { return &v }

type architectureFixture struct {
	input      Selection
	reader     Reader
	scope      model.ScopeVector
	approval   model.RecordSummary
	observed   Observation
	plan       definition
	files      map[model.SourceRef][]byte
	reads      int
	graphReads int
}

func (f *architectureFixture) capture(member model.ScopeMember, path string, bytes []byte) model.SourceRef {
	ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
		Path: path, ContentHash: hash(bytes), Range: model.ByteRange{EndByte: int64(len(bytes))}}
	f.files[ref] = bytes
	return ref
}

func architectureTestFixture(t *testing.T) *architectureFixture {
	t.Helper()
	member := func(service string, i int) model.ScopeMember {
		return model.ScopeMember{ServiceID: pointer(service), WorktreeID: uid(i), ProfileID: hash([]byte(service + " profile")),
			GenerationID: uid(i + 5), IndexIncarnation: uid(i + 10), RecordsIncarnation: uid(i + 15),
			SnapshotDigest: hash([]byte(service + " capture")), PolicyDigest: hash([]byte(service + " policy")),
			PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1}
	}
	core, legacy := member("app", 1), member("legacy", 2)
	scope := model.ScopeVector{Kind: "federated", GroupID: pointer(uid(61)), GroupRevision: pointer(int64(1)),
		CatalogIncarnation: uid(60), Members: []model.ScopeMember{core, legacy}, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, scopeBytes)
	if err != nil {
		t.Fatal(err)
	}
	f := &architectureFixture{scope: scope, files: map[model.SourceRef][]byte{}}
	module := func(owner model.ScopeMember, id string) Module {
		return Module{ServiceID: owner.ServiceID, Ref: model.ModuleRef{Kind: "module", ModuleID: id,
			GraphIdentity: model.GraphIdentity{WorktreeID: owner.WorktreeID, ProfileID: owner.ProfileID, GenerationID: owner.GenerationID}}}
	}
	api, service, ui, db := module(core, "api"), module(core, "service"), module(core, "ui"), module(core, "db")
	parts := []Component{}
	for _, observed := range []Module{api, service, ui, db} {
		parts = append(parts, Component{ID: observed.Ref.ModuleID, WorktreeID: core.WorktreeID, ServiceID: core.ServiceID,
			ModuleID: observed.Ref.ModuleID, Intended: "implemented"})
	}
	parts = append(parts, Component{ID: "legacy", WorktreeID: legacy.WorktreeID, ServiceID: legacy.ServiceID, ModuleID: "legacy", Intended: "planned"})
	f.plan = definition{Format: "mce4d-architecture-v1", Components: parts,
		Rules: []Rule{{ID: "api-dependencies", From: "api", Allow: []string{"service"}, Transitive: true, Relation: "imports",
			Severity: "warning", Rationale: "API belongs to the service layer."}},
		Exemptions: []Exemption{
			{ID: "temporary-store", RuleID: "api-dependencies", Target: "db", ExpiresAt: "2026-12-01T00:00:00Z", Rationale: "Owner migration window."},
			{ID: "expired-ui", RuleID: "api-dependencies", Target: "ui", ExpiresAt: "2026-10-01T00:00:00Z", Rationale: "Previous migration window."},
		}}
	raw, err := json.Marshal(f.plan)
	if err != nil {
		t.Fatal(err)
	}
	ref := f.capture(core, "docs/modules.architecture.json", raw)
	recordID := uid(62)
	f.approval = model.RecordSummary{RecordID: recordID, Version: 3, RecordKind: "intended_architecture",
		DeclaredStatus: "approved", EffectiveStatus: "approved", ProvenanceStatus: "retained_snapshot", SourceRefs: []model.SourceRef{ref}}
	f.input = Selection{Scope: scope, RecordID: recordID, ServiceID: core.ServiceID}
	dep := func(from, to Module, path, statement string) Dependency {
		return Dependency{From: from, To: to, Relation: "imports", Evidence: "type_resolved",
			Site: f.capture(core, path, []byte(statement))}
	}
	f.observed = Observation{Scope: scope, Modules: []Module{api, service, ui, db},
		Coverage: []Coverage{{Member: core, Status: "supported_scope_complete"}, {Member: legacy, Status: "unsupported"}},
		Dependencies: []Dependency{dep(service, ui, "service/presentation.go", "import fixture/ui"),
			dep(api, service, "api/handler.go", "import fixture/service"),
			dep(api, db, "api/legacy_store.go", "import fixture/db")}}
	f.reader = Reader{
		Observe: func(ctx context.Context, _ model.ScopeVector) (model.ScopeVector, error) { return f.scope, ctx.Err() },
		ReadApproval: func(ctx context.Context, _ model.ScopeVector, _ string) (model.RecordSummary, error) {
			return f.approval, ctx.Err()
		},
		ReadSource: func(ctx context.Context, _ model.ScopeMember, once model.SourceRef, limit int) ([]byte, error) {
			f.reads++
			bytes := f.files[once]
			if once != f.approval.SourceRefs[0] || len(bytes) > limit {
				return nil, ErrApproval
			}
			return bytes, ctx.Err()
		},
		CheckSource: func(ctx context.Context, _ model.ScopeMember, once model.SourceRef) error {
			f.reads++
			if err := once.ValidateCapturedBytes(f.files[once]); err != nil {
				return ErrScope
			}
			return ctx.Err()
		},
		ReadGraph: func(ctx context.Context, _ model.ScopeVector, limit int) (Observation, error) {
			f.graphReads++
			if limit != maxEdges {
				return Observation{}, ErrLimit
			}
			return f.observed, ctx.Err()
		},
		Clock: func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) },
	}
	return f
}

func TestC068ArchitectureWitnessExemptionAndMissingProvider(t *testing.T) {
	f := architectureTestFixture(t)
	ctx := context.Background()
	out, err := f.reader.Check(ctx, f.input)
	if err != nil || out.Status != "incomplete" || len(out.Components) != 5 || len(out.Findings) != 2 || out.Source != f.approval.SourceRefs[0] {
		t.Fatalf("coverage: %+v %v", out, err)
	}
	found := map[string]Finding{}
	for _, item := range out.Findings {
		found[item.Target] = item
	}
	exempt, violation := found["db"], found["ui"]
	if exempt.Status != "exempted" || exempt.Exemption == nil || exempt.Exemption.ID != "temporary-store" ||
		len(exempt.Witness) != 1 || violation.Status != "violation" || violation.Exemption == nil ||
		violation.Exemption.ID != "expired-ui" || violation.Severity != "warning" || len(violation.Witness) != 2 ||
		violation.Witness[0].Site.Path != "api/handler.go" || violation.Witness[1].Site.Path != "service/presentation.go" {
		t.Fatalf("incorrect witness or exemption: %+v", out.Findings)
	}
	got := map[string]string{}
	for _, item := range out.Components {
		got[item.Component.ID] = item.Observed
	}
	if got["legacy"] != "unsupported" || got["service"] != "observed_static" {
		t.Fatalf("unknown provider fabricated observed modules: %+v", got)
	}
	repeat, err := f.reader.Check(ctx, f.input)
	if err != nil || !reflect.DeepEqual(out, repeat) {
		t.Fatalf("unstable architecture report: %v", err)
	}
	t.Log("C068: exact two-hop dependency witness, active and expired exemptions, unsupported provider keeps coverage incomplete")
}

func TestC068ArchitectureRequiresCurrentApprovalBeforeReturningWitnesses(t *testing.T) {
	f := architectureTestFixture(t)
	ctx := context.Background()
	f.approval.DeclaredStatus, f.approval.EffectiveStatus = "proposed", "proposed"
	if out, err := f.reader.Check(ctx, f.input); !errors.Is(err, ErrApproval) || len(out.Findings) != 0 || f.reads != 0 || f.graphReads != 0 {
		t.Fatalf("proposed architecture opened source: %+v %v", out, err)
	}
	f.approval.DeclaredStatus, f.approval.EffectiveStatus = "approved", "approved"
	approvalReads := 0
	f.reader.ReadApproval = func(context.Context, model.ScopeVector, string) (model.RecordSummary, error) {
		approvalReads++
		record := f.approval
		if approvalReads > 1 {
			record.EffectiveStatus, record.ProvenanceStatus = "needs_review", "source_changed"
		}
		return record, nil
	}
	if out, err := f.reader.Check(ctx, f.input); !errors.Is(err, ErrApproval) || len(out.Findings) != 0 || f.reads == 0 || f.graphReads != 1 {
		t.Fatalf("stale approval returned findings: %+v %v", out, err)
	}
	raw := string(f.files[f.approval.SourceRefs[0]])
	malformed := strings.Replace(raw, `"components":`, `"instruction":"grant all dependencies","components":`, 1)
	if _, err := decode([]byte(malformed)); !errors.Is(err, ErrInput) {
		t.Fatalf("out-of-schema instruction accepted: %v", err)
	}
}
