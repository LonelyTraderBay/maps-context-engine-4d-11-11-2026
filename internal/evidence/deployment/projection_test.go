package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const manifestFixture = `{
  "kind": "deployment-map",
  "schema_version": 1,
  "services": [
    {
      "id": "worker", "version":"build-2", "owner":"jobs",
      "ports":[], "config":[], "permissions":[]
    },
    {
      "id": "api", "version":"build-2", "owner":"payments",
      "ports":[{"name":"http","number":8080,"protocol":"tcp"}],
      "config":[
        {"name":"SESSION_SIGNING","kind":"secret_reference","owner":"runtime"},
        {"name":"ASYNC_RECEIPTS","kind":"feature_flag","owner":"payments"}
      ],
      "permissions":[{"principal":"api-role","action":"read","resource":"receipts"}]
    }
  ]
}`

func TestC077DeclaredBindingAndHistoricalObservations(t *testing.T) {
	captured, scope, ref := newCapture(t, manifestFixture)
	var checked []Subject
	guard := func(_ context.Context, current model.ScopeVector, subject Subject) error {
		if current.VectorDigest != scope.VectorDigest || !model.ValidDigest(subject.ContentHash) {
			return ErrAuthority
		}
		checked = append(checked, subject)
		return nil
	}
	ctx := t.Context()
	base, err := Build(ctx, scope, captured, ref, nil, guard)
	if err != nil || len(checked) != 2 || base.Source != ref || base.LiveEnvironment != "unknown" || base.Observed != nil ||
		len(base.Declared.Services) != 2 {
		t.Fatalf("declaration: %+v, %v", base, err)
	}
	api := base.Declared.Services[0]
	if api.ID != "api" || api.Owner != "payments" || api.Version != "build-2" || api.Ports[0].Number != 8080 ||
		api.Config[0].Kind != "feature_flag" || api.Config[1].Kind != "secret_reference" || api.Permissions[0].Action != "read" {
		t.Fatalf("declared ownership: %+v", api)
	}
	old := observationFixture(t, scope.Members[0], strings.Repeat("d", 64))
	checked = nil
	with, err := Build(ctx, scope, captured, ref, old, guard)
	if err != nil || len(checked) != 4 || with.Observed.Alignment != "historical" || with.LiveEnvironment != "unknown" ||
		with.Observed.Evidence.Environment != "fixture-stage" || with.Observed.Evidence.Services[0].Version != "build-1" ||
		with.Declared.Services[0].Version != "build-2" || with.Observed.ArtifactHash != old.ApprovedHash {
		t.Fatalf("historical artifact changed declared or live state: %+v, %v", with, err)
	}
	current := observationFixture(t, scope.Members[0], captured.Manifest().SnapshotDigest)
	with, err = Build(ctx, scope, captured, ref, current, guard)
	if err != nil || with.Observed.Alignment != "matching_capture" || with.LiveEnvironment != "unknown" ||
		with.Declared.Services[0].Version != "build-2" {
		t.Fatalf("an observed version became a live claim: %+v, %v", with, err)
	}
}

func TestC077RejectsExecutionFieldsAndUnknownMetadata(t *testing.T) {
	for _, item := range []struct{ name, old, replacement string }{
		{"script", `"owner":"jobs"`, `"owner":"jobs","script":"synthetic-iac-command"`},
		{"secret value", `"owner":"runtime"`, `"owner":"runtime","value":"synthetic_value"`},
		{"nested alias", `"kind":"feature_flag"`, `"KIND":"feature_flag"`},
		{"duplicate member", `"owner":"jobs"`, `"owner":"jobs","owner":"other"`},
		{"duplicate service", `"id": "worker"`, `"id": "api"`},
		{"invalid port", `"number":8080`, `"number":65536`},
		{"missing fields", `"ports":[], `, ``},
		{"pointer with shell interpolation", `"name":"ASYNC_RECEIPTS"`, `"name":"$(synthetic-command)"`},
	} {
		t.Run(item.name, func(t *testing.T) {
			raw := strings.Replace(manifestFixture, item.old, item.replacement, 1)
			if raw == manifestFixture {
				t.Fatal("fixture did not change")
			}
			captured, scope, ref := newCapture(t, raw)
			if result, err := Build(t.Context(), scope, captured, ref, nil, allowFixture); !errors.Is(err, ErrEvidence) || result.Declared.Services != nil {
				t.Fatalf("excluded metadata escaped parser: %v", err)
			}
		})
	}
}

func TestC077AuthorityCaptureAndArtifactHash(t *testing.T) {
	captured, scope, ref := newCapture(t, manifestFixture)
	member := scope.Members[0]
	ctx := t.Context()
	subset := ref
	subset.Range.EndByte--
	if _, err := Build(ctx, scope, captured, subset, nil, allowFixture); !errors.Is(err, ErrEvidence) {
		t.Fatalf("partial manifest admitted: %v", err)
	}
	ignored := ref
	ignored.Path = ".env"
	if _, err := Build(ctx, scope, captured, ignored, nil, allowFixture); !errors.Is(err, ErrAuthority) {
		t.Fatalf("denied path admitted: %v", err)
	}
	foreign := member
	foreign.WorktreeID = "66666666-6666-4666-8666-666666666666"
	if _, err := Build(ctx, scope, captured, ref, observationFixture(t, foreign, member.SnapshotDigest), allowFixture); !errors.Is(err, ErrEvidence) {
		t.Fatalf("foreign observation admitted: %v", err)
	}
	bad := observationFixture(t, member, member.SnapshotDigest)
	bad.Bytes = append(bad.Bytes, ' ')
	if _, err := Build(ctx, scope, captured, ref, bad, allowFixture); !errors.Is(err, ErrEvidence) {
		t.Fatalf("unpinned artifact admitted: %v", err)
	}
	checks := 0
	revoke := func(_ context.Context, _ model.ScopeVector, subject Subject) error {
		if subject.Kind == "observed" {
			checks++
			if checks == 2 {
				return ErrAuthority
			}
		}
		return nil
	}
	if result, err := Build(ctx, scope, captured, ref, observationFixture(t, member, member.SnapshotDigest), revoke); !errors.Is(err, ErrAuthority) || result.Observed != nil {
		t.Fatalf("revoked artifact admitted: %v", err)
	}
}

func newCapture(t *testing.T, raw string) (*snapshot.Snapshot, model.ScopeVector, model.SourceRef) {
	t.Helper()
	const worktree = "11111111-1111-4111-8111-111111111111"
	const generation = "22222222-2222-4222-8222-222222222222"
	const filename = "docs/services.deployment.json"
	file, err := snapshot.NewFile(filename, []byte(raw), MaxEvidenceBytes)
	if err != nil {
		t.Fatal(err)
	}
	hash, size := file.ContentHash(), int64(file.Len())
	m := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: strings.Repeat("a", 64),
		PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), DependencyInputDigests: []string{},
		Files: []model.ManifestFile{{Path: filename, Status: "captured", ContentHash: &hash, ByteLength: &size}}}
	capture, err := snapshot.Freeze(m, []snapshot.File{file}, 8192, MaxEvidenceBytes)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: m.ProfileID, GenerationID: generation,
		PolicyDigest: m.PolicyDigest, PolicyEpoch: 1, RecordEpoch: 1, BindingRevision: 1, SnapshotDigest: capture.Manifest().SnapshotDigest,
		IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member},
		CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{WorktreeID: worktree, GenerationID: generation, ProfileID: m.ProfileID, Path: filename,
		ContentHash: hash, Range: model.ByteRange{StartByte: 0, EndByte: size}}
	return capture, scope, ref
}

func observationFixture(t *testing.T, member model.ScopeMember, capture string) *LocalObservation {
	t.Helper()
	raw, err := json.Marshal(Observation{Kind: "deployment-observation", SchemaVersion: 1,
		WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, SnapshotDigest: capture,
		Environment: "fixture-stage", ObservedAt: "2026-11-21T20:00:00Z",
		Services: []ObservedService{{ID: "api", Version: "build-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return &LocalObservation{Path: "fixture.deployment-observation.json", ApprovedHash: hex.EncodeToString(sum[:]), Bytes: raw}
}

func allowFixture(ctx context.Context, _ model.ScopeVector, _ Subject) error { return ctx.Err() }
