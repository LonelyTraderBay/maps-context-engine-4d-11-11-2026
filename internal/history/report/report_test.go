package report

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/domain/architecture"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/deployment"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/history"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var budget = Budget{Rows: 32, Bytes: 16384}

const declaration = `{"kind":"deployment-map","schema_version":1,"services":[{"id":"api","version":"v2","owner":"core","ports":[],"config":[{"name":"FEATURE","kind":"feature_flag","owner":"core"}],"permissions":[]}]}`

func TestC077C080DeploymentReportPreservesScopeAndBudget(t *testing.T) {
	captured, scope, ref := retained(t, 1, "docs/services.deployment.json", declaration)
	var checks int
	service := Service{Authorize: func(ctx context.Context, candidate model.ScopeVector) error {
		checks++
		if candidate.VectorDigest != scope.VectorDigest {
			return ErrScope
		}
		return ctx.Err()
	}}
	input := DeploymentInput{Scope: scope, Source: ref, Capture: captured,
		Authorize: func(ctx context.Context, _ model.ScopeVector, _ deployment.Subject) error { return ctx.Err() }}
	raw, err := service.DeploymentJSON(t.Context(), input, budget)
	if err != nil || checks != 2 {
		t.Fatalf("deployment report: %v, scope checks=%d", err, checks)
	}
	data := decoded(t, raw, "deployment", "declared", scope.VectorDigest)
	var projection deployment.Projection
	if err := json.Unmarshal(data, &projection); err != nil || projection.Source != ref ||
		projection.LiveEnvironment != "unknown" || projection.Observed != nil || projection.Declared.Services[0].Config[0].Kind != "feature_flag" {
		t.Fatalf("deployment metadata: %+v, %v", projection, err)
	}
	material, err := json.Marshal(deployment.Observation{Kind: "deployment-observation", SchemaVersion: 1,
		WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, SnapshotDigest: strings.Repeat("d", 64),
		Environment: "local-test", ObservedAt: "2026-11-21T20:00:00Z", Services: []deployment.ObservedService{{ID: "api", Version: "v1"}}})
	if err != nil {
		t.Fatal(err)
	}
	input.Observation = &deployment.LocalObservation{Path: "local.deployment-observation.json",
		ApprovedHash: fmt.Sprintf("%x", sha256.Sum256(material)), Bytes: material}
	with, err := service.DeploymentJSON(t.Context(), input, budget)
	if err != nil {
		t.Fatal(err)
	}
	data = decoded(t, with, "deployment", "declared", scope.VectorDigest)
	if err := json.Unmarshal(data, &projection); err != nil || projection.Observed == nil ||
		projection.Observed.Alignment != "historical" || projection.Observed.Evidence.Services[0].Version != "v1" ||
		projection.Declared.Services[0].Version != "v2" || projection.LiveEnvironment != "unknown" {
		t.Fatalf("report merged observed and declared versions: %v", err)
	}
	for _, b := range []Budget{{Rows: 1, Bytes: budget.Bytes}, {Rows: budget.Rows, Bytes: len(raw) - 1}, {Rows: MaxRows + 1, Bytes: budget.Bytes}} {
		if raw, err := service.DeploymentJSON(t.Context(), input, b); !errors.Is(err, ErrLimit) || raw != nil {
			t.Fatalf("report budget returned partial data: %v", err)
		}
	}
	checks = 0
	service.Authorize = func(context.Context, model.ScopeVector) error {
		checks++
		if checks == 2 {
			return ErrScope
		}
		return nil
	}
	if raw, err := service.DeploymentJSON(t.Context(), input, budget); !errors.Is(err, ErrScope) || raw != nil {
		t.Fatalf("revoked report released bytes: %v", err)
	}
}

func TestC080ChangeReportUsesRetainedDiff(t *testing.T) {
	aCapture, aScope, aRef := retained(t, 1, "a.go", "func Anchor() int { return 1 }\n")
	bCapture, bScope, bRef := retained(t, 2, "a.go", "func Anchor() int { return 2 }\n")
	symbol := func(ref model.SourceRef) model.SymbolFact {
		return model.SymbolFact{SymbolID: "anchor", Path: &ref.Path, Range: &ref.Range, Kind: "function",
			Name: "Anchor", QualifiedName: "fixture.Anchor", ScopeKey: "fixture", Signature: "func() int"}
	}
	service := Service{Authorize: permitScope, Diff: history.DiffResolver{Observe: observeScope,
		ReadMember: func(_ context.Context, _ model.ScopeVector, member model.ScopeMember, _ int) (history.DiffMember, error) {
			if member.GenerationID == aRef.GenerationID {
				return history.DiffMember{Member: member, Capture: aCapture, Symbols: []model.SymbolFact{symbol(aRef)}, Complete: true}, nil
			}
			return history.DiffMember{Member: member, Capture: bCapture, Symbols: []model.SymbolFact{symbol(bRef)}, Complete: false}, nil
		}}}
	req := history.DiffRequest{Left: aScope, Right: bScope, MaxFacts: 8, MaxChanges: 8}
	raw, err := service.ChangesJSON(t.Context(), req, budget)
	if err != nil {
		t.Fatal(err)
	}
	data := decoded(t, raw, "change", "partial", bScope.VectorDigest)
	var result struct {
		Left    string `json:"left_scope_digest"`
		Changes []struct {
			Category string
			Before   *struct{ Source model.SourceRef }
			After    *struct{ Source model.SourceRef }
		} `json:"changes"`
		Gaps []model.Gap `json:"gaps"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Left != aScope.VectorDigest || len(result.Changes) != 1 ||
		result.Changes[0].Category != "symbol" || result.Changes[0].Before == nil || result.Changes[0].After == nil || result.Changes[0].Before.Source != aRef ||
		result.Changes[0].After.Source != bRef || len(result.Gaps) == 0 {
		t.Fatalf("retained comparison not preserved: %+v, %v", result, err)
	}
}

func TestC080ArchitectureReportUsesApprovedDeclaration(t *testing.T) {
	const worktree = "11111111-1111-4111-8111-111111111111"
	text := fmt.Sprintf(`{"format":"mce4d-architecture-v1","components":[{"id":"api","service_id":null,"worktree_id":%q,"profile_id":null,"module_id":"api","intended":"implemented"}],"rules":[],"exemptions":[]}`, worktree)
	_, scope, source := retained(t, 1, "docs/modules.architecture.json", text)
	approval := model.RecordSummary{RecordID: "55555555-5555-4555-8555-555555555555", Version: 2, RecordKind: "intended_architecture",
		DeclaredStatus: "approved", EffectiveStatus: "approved", ProvenanceStatus: "retained_snapshot", SourceRefs: []model.SourceRef{source}}
	service := Service{Authorize: permitScope, Architecture: architecture.Reader{
		Observe: observeScope, ReadApproval: func(context.Context, model.ScopeVector, string) (model.RecordSummary, error) { return approval, nil },
		ReadSource: func(context.Context, model.ScopeMember, model.SourceRef, int) ([]byte, error) {
			return []byte(text), nil
		},
		CheckSource: func(context.Context, model.ScopeMember, model.SourceRef) error { return nil },
		ReadGraph: func(context.Context, model.ScopeVector, int) (architecture.Observation, error) {
			return architecture.Observation{Scope: scope}, nil
		},
		Clock: func() time.Time { return time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC) },
	}}
	selection := architecture.Selection{Scope: scope, RecordID: approval.RecordID}
	raw, err := service.ArchitectureJSON(t.Context(), selection, budget)
	if err != nil {
		t.Fatal(err)
	}
	data := decoded(t, raw, "architecture", "incomplete", scope.VectorDigest)
	var result architectureData
	if err := json.Unmarshal(data, &result); err != nil || result.Source != source || result.RecordID != approval.RecordID ||
		result.RecordVersion != 2 || len(result.Components) != 1 || result.Components[0].Observed != "unsupported" || len(result.Gaps) != 1 {
		t.Fatalf("architecture proof lost: %+v, %v", result, err)
	}
	approval.EffectiveStatus = "revoked"
	if raw, err := service.ArchitectureJSON(t.Context(), selection, budget); !errors.Is(err, architecture.ErrApproval) || raw != nil {
		t.Fatalf("revoked approval produced output: %v", err)
	}
}

func retained(t *testing.T, variant int, path, body string) (*snapshot.Snapshot, model.ScopeVector, model.SourceRef) {
	t.Helper()
	const worktree = "11111111-1111-4111-8111-111111111111"
	file, err := snapshot.NewFile(path, []byte(body), 4096)
	if err != nil {
		t.Fatal(err)
	}
	hash, size := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: strings.Repeat("a", 64),
		PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), DependencyInputDigests: []string{},
		Files: []model.ManifestFile{{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &size}}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 4096)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: fmt.Sprintf("22222222-2222-4222-8222-%012d", variant),
		SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1,
		BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333",
		RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", CatalogIncarnation: worktree, Members: []model.ScopeMember{member}, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{Path: path, WorktreeID: worktree, ProfileID: member.ProfileID,
		GenerationID: member.GenerationID, ContentHash: hash, Range: model.ByteRange{EndByte: size}}
	return captured, scope, ref
}

func decoded(t *testing.T, raw []byte, kind, status, digest string) json.RawMessage {
	t.Helper()
	var out struct {
		Schema int             `json:"schema_version"`
		Kind   string          `json:"kind"`
		Scope  string          `json:"scope_digest"`
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	identity := json.NewDecoder(strings.NewReader(string(raw)))
	identity.DisallowUnknownFields()
	if err := identity.Decode(&out); err != nil || out.Schema != 1 || out.Kind != kind || out.Status != status ||
		out.Scope != digest || len(out.Data) == 0 || len(raw) > budget.Bytes {
		t.Fatalf("machine report envelope: %s, %v", raw, err)
	}
	return out.Data
}

func permitScope(ctx context.Context, _ model.ScopeVector) error { return ctx.Err() }

func observeScope(ctx context.Context, scope model.ScopeVector) (model.ScopeVector, error) {
	return scope, ctx.Err()
}
