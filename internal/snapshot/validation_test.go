package snapshot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func sourceValidationFixture(t *testing.T) (*Snapshot, model.ScopeVector, model.SourceRef) {
	t.Helper()
	file, err := NewFile("src.go", []byte("a😀b\r\n"), 100)
	if err != nil {
		t.Fatal(err)
	}
	hash, size := file.ContentHash(), int64(file.Len())
	id := "11111111-1111-4111-8111-111111111111"
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: id, ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{{Path: "src.go", Status: "captured", ContentHash: &hash, ByteLength: &size}}, DependencyInputDigests: []string{}}
	captured, err := Freeze(manifest, []File{file}, 8192, 100)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: id, ProfileID: manifest.ProfileID, GenerationID: "22222222-2222-4222-8222-222222222222", SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{WorktreeID: id, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: "src.go", ContentHash: hash, Range: model.ByteRange{StartByte: 1, EndByte: 5}}
	return captured, scope, ref
}

func TestValidateCapturedSourceBindsScopeGenerationHashAndUTF8(t *testing.T) {
	captured, scope, ref := sourceValidationFixture(t)
	if err := ValidateCapturedSource(scope, ref, nil, captured, 8192); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*model.ScopeVector, *model.SourceRef){
		"wrong_hash": func(_ *model.ScopeVector, r *model.SourceRef) { r.ContentHash = strings.Repeat("d", 64) },
		"wrong_generation": func(_ *model.ScopeVector, r *model.SourceRef) {
			r.GenerationID = "66666666-6666-4666-8666-666666666666"
		},
		"missing_file":   func(_ *model.ScopeVector, r *model.SourceRef) { r.Path = "other.go" },
		"split_utf8":     func(_ *model.ScopeVector, r *model.SourceRef) { r.Range.StartByte = 2 },
		"outside_file":   func(_ *model.ScopeVector, r *model.SourceRef) { r.Range.EndByte = 100 },
		"tampered_scope": func(s *model.ScopeVector, _ *model.SourceRef) { s.VectorDigest = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			current, candidate := scope, ref
			mutate(&current, &candidate)
			if err := ValidateCapturedSource(current, candidate, nil, captured, 8192); !errors.Is(err, ErrSourcePrecondition) {
				t.Fatalf("invalid source precondition accepted: %v", err)
			}
		})
	}
	other := scope
	other.Members = append([]model.ScopeMember(nil), scope.Members...)
	other.Members[0].SnapshotDigest = strings.Repeat("d", 64)
	var err error
	other.VectorDigest, err = model.ComputeScopeVectorDigest(other, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCapturedSource(other, ref, nil, captured, 8192); !errors.Is(err, ErrSourcePrecondition) {
		t.Fatalf("mismatched generation snapshot accepted: %v", err)
	}
}

func TestValidateCapturedSourcesRejectsAnyInvalidMemberOrCancellation(t *testing.T) {
	captured, scope, ref := sourceValidationFixture(t)
	other := ref
	other.Range = model.ByteRange{StartByte: 0, EndByte: 1}
	items := []ExpectedSource{{Ref: ref}, {Ref: other}}
	if err := ValidateCapturedSources(context.Background(), scope, items, captured, 8192); err != nil {
		t.Fatal(err)
	}
	items[1].Ref.ContentHash = strings.Repeat("d", 64)
	if err := ValidateCapturedSources(context.Background(), scope, items, captured, 8192); !errors.Is(err, ErrSourcePrecondition) {
		t.Fatalf("invalid item accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateCapturedSources(ctx, scope, items, captured, 8192); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation accepted: %v", err)
	}
}
