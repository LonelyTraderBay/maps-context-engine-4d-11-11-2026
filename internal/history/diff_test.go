package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const diffWorktree = "11000000-0000-4000-8000-000000000001"
const diffLeft = "11000000-0000-4000-8000-000000000002"
const diffRight = "11000000-0000-4000-8000-000000000003"
const diffRecord = "11000000-0000-4000-8000-000000000004"

func fingerprint(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func diffFixture(t *testing.T, generation, profile, provider string) (model.ScopeVector, DiffMember) {
	t.Helper()
	right := generation == diffRight
	move, original, body := "src/move.go", "Previous", `{"required":false}`
	twinPaths := []string{"src/twin-a.go", "src/twin-b.go"}
	if right {
		move, original, body = "src/moved.go", "Renamed", `{"required":true}`
		twinPaths = []string{"src/twin-c.go", "src/twin-d.go"}
	}
	files := map[string]string{
		move:                  "func Move() { _ = 1 }\n",
		"src/name.go":         "func " + original + "() {}\n",
		"src/caller.go":       "func Call() { Move() }\n",
		"contracts/api.json":  body,
		"rules/decision.json": "approved source " + body,
		twinPaths[0]:          "func Twin() {}\n",
		twinPaths[1]:          "func Twin() {}\n",
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: diffWorktree, ProfileID: profile,
		PolicyDigest: fingerprint("fixture policy"), ProviderDigest: provider, Files: []model.ManifestFile{},
		DependencyInputDigests: []string{}}
	var captured []snapshot.File
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, path := range names {
		file, err := snapshot.NewFile(path, []byte(files[path]), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := file.ContentHash(), int64(file.Len())
		captured = append(captured, file)
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &size})
	}
	frozen, err := snapshot.Freeze(manifest, captured, 65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: diffWorktree, ProfileID: profile, GenerationID: generation,
		SnapshotDigest: frozen.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1,
		BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: diffWorktree, RecordsIncarnation: diffRecord}
	scope := model.ScopeVector{Kind: "single", CatalogIncarnation: diffWorktree, Members: []model.ScopeMember{member},
		UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	full := func(path string) model.SourceRef {
		file, _ := frozen.File(path)
		return memberSource(member, path, file.ContentHash(), model.ByteRange{EndByte: int64(file.Len())})
	}
	symbol := func(id, path, name string) model.SymbolFact {
		return model.SymbolFact{SymbolID: id, Kind: "function", Path: &path, Name: name, QualifiedName: "demo." + name,
			ScopeKey: "package-demo", Signature: "func()", Exported: true,
			Range: &model.ByteRange{EndByte: int64(len(files[path]))}}
	}
	idMove, declaration := "move-"+generation, full("contracts/api.json")
	return scope, DiffMember{Member: member, Capture: frozen, Complete: true,
		Symbols: []model.SymbolFact{symbol(idMove, move, "Move"), symbol("renamed-"+generation, "src/name.go", original),
			symbol("caller", "src/caller.go", "Call"), symbol("one", twinPaths[0], "Twin"), symbol("two", twinPaths[1], "Twin")},
		Relations: []model.RelationFact{{RelationID: "calls", SourceSymbolID: "caller", TargetSymbolID: &idMove, Kind: "CALLS",
			EvidenceKind: "type_resolved", SitePath: "src/caller.go", Range: model.ByteRange{StartByte: 14, EndByte: 20}}},
		Domain: contracts.Batch{SchemaVersion: 1, Entities: []contracts.Entity{{Ref: model.EntityRef{Kind: "entity",
			GraphIdentity: memberIdentity(member), EntityID: "contract-" + generation}, Kind: "http_operation",
			Key:   contracts.Key{Environment: "local", Protocol: "http", Namespace: "orders", Identifier: "POST /orders", Version: "v1"},
			Plane: "observed_static", Declaration: declaration}}, Fields: []contracts.Field{}, Links: []contracts.Link{}},
		Decisions: []model.RecordSummary{{RecordID: diffRecord, Version: 1, RecordKind: "constraint", DeclaredStatus: "approved",
			EffectiveStatus: "approved", ProvenanceStatus: "retained_snapshot", SourceRefs: []model.SourceRef{full("rules/decision.json")}}},
	}
}

func diffOwner(views ...DiffMember) DiffResolver {
	return DiffResolver{
		Observe: func(ctx context.Context, exact model.ScopeVector) (model.ScopeVector, error) { return exact, ctx.Err() },
		ReadMember: func(ctx context.Context, _ model.ScopeVector, target model.ScopeMember, _ int) (DiffMember, error) {
			for _, view := range views {
				if reflect.DeepEqual(view.Member, target) {
					return view, ctx.Err()
				}
			}
			return DiffMember{}, ErrDiffSnapshotMissing
		},
	}
}

func TestC064SemanticSnapshotDiffPreservesAmbiguityAndApprovalDrift(t *testing.T) {
	left, a := diffFixture(t, diffLeft, fingerprint("profile"), fingerprint("provider"))
	right, b := diffFixture(t, diffRight, fingerprint("profile"), fingerprint("provider"))
	req := DiffRequest{Left: left, Right: right, MaxFacts: 100, MaxChanges: 100}
	resolver := diffOwner(a, b)
	result, err := resolver.Compare(context.Background(), req)
	if err != nil || !result.Partial || result.ExactMatches == 0 || len(result.Decisions) != 1 ||
		result.Decisions[0].Status != "needs_review" || result.Decisions[0].Before.ContentHash == result.Decisions[0].After.ContentHash ||
		len(result.Consumers) != 2 || len(result.ProviderDeltas) != 0 {
		t.Fatalf("comparison proof: %+v %v", result, err)
	}
	repeat, err := resolver.Compare(context.Background(), req)
	if err != nil || !reflect.DeepEqual(result, repeat) {
		t.Fatalf("non-deterministic comparison: %v", err)
	}
	var moves, ambiguous, renamed, changedContract, relations int
	for _, change := range result.Changes {
		switch {
		case change.Category == "symbol" && change.Before != nil && change.Before.Label == "demo.Move":
			if change.Match != "probable" || change.After == nil || change.After.Source.Path != "src/moved.go" {
				t.Fatalf("moved symbol inferred exact: %+v", change)
			}
			moves++
		case change.Category == "symbol" && change.Change == "ambiguous":
			if change.Match != "unmatched" || (change.Before != nil && change.After != nil) {
				t.Fatalf("ambiguous same-name symbol forced together: %+v", change)
			}
			ambiguous++
		case change.Category == "symbol" && (change.Before != nil && strings.HasSuffix(change.Before.Label, "Previous") ||
			change.After != nil && strings.HasSuffix(change.After.Label, "Renamed")):
			if change.Match != "unmatched" {
				t.Fatalf("rename guessed from location: %+v", change)
			}
			renamed++
		case change.Category == "relation":
			relations++
		case change.Category == "contract" && change.Change == "changed" && change.Match == "exact":
			changedContract++
		}
	}
	if moves != 1 || ambiguous != 4 || renamed != 2 || changedContract != 1 || relations != 2 {
		t.Fatalf("changes did not cover all feature cases: %+v", result.Changes)
	}
	t.Log("C064: exact contract and probable move evidence, four ambiguous symbols, two known callers, one source-bound needs_review")
}

func TestC064ProviderChangeMissingAndExpiredStopSemanticComparison(t *testing.T) {
	left, a := diffFixture(t, diffLeft, fingerprint("profile"), fingerprint("provider"))
	right, b := diffFixture(t, diffRight, fingerprint("profile"), fingerprint("new provider"))
	req := DiffRequest{Left: left, Right: right, MaxFacts: 100, MaxChanges: 100}
	resolver := diffOwner(a, b)
	result, err := resolver.Compare(context.Background(), req)
	if err != nil || len(result.ProviderDeltas) != 1 || len(result.Changes) != 0 || len(result.Consumers) != 0 {
		t.Fatalf("provider change misclassified as code change: %+v %v", result, err)
	}
	missing := diffOwner(a)
	if result, err := missing.Compare(context.Background(), req); !errors.Is(err, ErrDiffSnapshotMissing) || result.Left.Kind != "" {
		t.Fatalf("missing snapshot silent fallback: %+v %v", result, err)
	}
	reads := 0
	resolver.Observe = func(_ context.Context, pin model.ScopeVector) (model.ScopeVector, error) {
		reads++
		if reads == 4 {
			return model.ScopeVector{}, ErrDiffSnapshotMissing
		}
		return pin, nil
	}
	if result, err := resolver.Compare(context.Background(), req); !errors.Is(err, ErrDiffSnapshotMissing) || result.Left.Kind != "" {
		t.Fatalf("late expiry returned evidence: %+v %v", result, err)
	}
	req.MaxFacts = 1
	if _, err := diffOwner(a, b).Compare(context.Background(), req); !errors.Is(err, ErrDiffLimit) {
		t.Fatalf("unbounded comparison: %v", err)
	}
}
