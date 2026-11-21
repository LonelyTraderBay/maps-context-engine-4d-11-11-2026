package testevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func fixture(t *testing.T) (*snapshot.Snapshot, Metadata, TestResult, CoverageHit) {
	t.Helper()
	file, err := snapshot.NewFile("sample_test.go", []byte("// 🌱\npackage sample\n"), 256)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111",
		ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64),
		Files: []model.ManifestFile{{Path: file.Path(), Status: "captured", ContentHash: new(file.ContentHash()),
			ByteLength: new(int64(file.Len()))}}, DependencyInputDigests: []string{}}
	staged, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 8192)
	if err != nil {
		t.Fatal(err)
	}
	m := Metadata{SchemaVersion: 1, Kind: TestReport, Capture: Capture{WorktreeID: manifest.WorktreeID,
		ProfileID: manifest.ProfileID, Revision: strings.Repeat("d", 40), SnapshotDigest: staged.Manifest().SnapshotDigest},
		Environment: "fixture", Command: []string{"go", "test", "./..."}, CompletedAt: "2026-11-21T18:00:00Z"}
	return staged, m,
		TestResult{TestID: "TestPass/no_assertions", Path: file.Path(), ContentHash: file.ContentHash(), Outcome: "passed", AssertionIDs: []string{}},
		CoverageHit{Path: file.Path(), ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 3, EndByte: 7}, HitCount: 3}
}

func artifactBytes(t *testing.T, meta Metadata, tests []TestResult, coverage []CoverageHit) []byte {
	t.Helper()
	raw, err := json.Marshal(struct {
		Metadata Metadata      `json:"metadata"`
		Tests    []TestResult  `json:"tests"`
		Coverage []CoverageHit `json:"coverage"`
	}{meta, append([]TestResult{}, tests...), append([]CoverageHit{}, coverage...)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestC072TestAndCoverageEvidenceStayDistinct(t *testing.T) {
	ctx := t.Context()
	capture, meta, one, hit := fixture(t)
	base := t.TempDir()
	root, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	const name = "approved-report.json"
	approval := Approval{WorktreeID: meta.Capture.WorktreeID, Environment: meta.Environment, Command: meta.Command}
	approval.Recheck = func(ctx context.Context, candidate *paths.Root, file, hash string) error {
		if candidate != root || file != name || hash != approval.InputHash {
			return ErrApproval
		}
		return ctx.Err()
	}
	write := func(content []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(base, name), content, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		approval.InputHash = hex.EncodeToString(digest[:])
	}
	claimed := one
	claimed.TestID = "TestInvariant"
	claimed.AssertionIDs = []string{"RollbackInvariant"}
	body := artifactBytes(t, meta, []TestResult{one, claimed}, nil)
	write(body)
	report, err := Read(ctx, root, name, approval)
	if err != nil || report.InputHash() != approval.InputHash || report.Alignment(meta.Capture.Revision, capture) != "matching_capture" ||
		report.Alignment(strings.Repeat("e", 40), capture) != "historical" {
		t.Fatalf("report alignment: %v", err)
	}
	results := report.Tests()
	if len(results) != 2 || results[0].Outcome != "passed" || len(results[0].AssertionIDs) != 0 ||
		len(report.Coverage()) != 0 || results[1].AssertionIDs[0] != "RollbackInvariant" {
		t.Fatal("test outcome, coverage and host invariant claim were conflated")
	}
	results[1].AssertionIDs[0] = "caller-mutation"
	if report.Tests()[1].AssertionIDs[0] != "RollbackInvariant" {
		t.Fatal("caller mutated the admitted report")
	}
	oldHash := approval.InputHash
	write(append(append([]byte{}, body...), '\n'))
	wrong := approval
	wrong.InputHash = oldHash
	wrong.Recheck = func(context.Context, *paths.Root, string, string) error { return nil }
	if _, err := Read(ctx, root, name, wrong); !errors.Is(err, ErrDigest) {
		t.Fatalf("changed bytes admitted: %v", err)
	}

	meta.Kind = CoverageReport
	write(artifactBytes(t, meta, nil, []CoverageHit{hit}))
	report, err = Read(ctx, root, name, approval)
	if err != nil || report.Alignment(meta.Capture.Revision, capture) != "matching_capture" ||
		len(report.Tests()) != 0 || len(report.Coverage()) != 1 || report.Coverage()[0].HitCount != 3 {
		t.Fatalf("coverage inferred a test outcome or assertion: %v", err)
	}
	hit.Range.StartByte++
	write(artifactBytes(t, meta, nil, []CoverageHit{hit}))
	report, err = Read(ctx, root, name, approval)
	if err != nil || report.Alignment(meta.Capture.Revision, capture) != "unbound" {
		t.Fatalf("split UTF-8 range bound to source: %v", err)
	}
	meta.Capture.ProfileID = strings.Repeat("f", 64)
	write(artifactBytes(t, meta, nil, []CoverageHit{hit}))
	report, err = Read(ctx, root, name, approval)
	if err != nil || report.Alignment(meta.Capture.Revision, capture) != "unbound" {
		t.Fatalf("different profile used to bind evidence: %v", err)
	}
}

func TestTestEvidenceClosedShape(t *testing.T) {
	_, meta, one, hit := fixture(t)
	raw := artifactBytes(t, meta, []TestResult{one}, nil)
	invalid := []string{
		strings.Replace(string(raw), `"outcome":"passed"`, `"outcome":"passed","output":"synthetic-sensitive"`, 1),
		strings.Replace(string(raw), `"outcome":"passed"`, `"outcome":"passed","outcome":"failed"`, 1),
		strings.Replace(string(raw), `"assertion_ids":[]`, `"assertion_ids":null`, 1),
		strings.Replace(string(raw), `"sample_test.go"`, `"../sample_test.go"`, 1),
		strings.Replace(string(raw), `"go","test"`, `"go","test;exit"`, 1),
		string(artifactBytes(t, meta, []TestResult{one, one}, nil)),
		string(artifactBytes(t, meta, []TestResult{one}, []CoverageHit{hit})),
		string(raw) + strings.Repeat(" ", MaxBytes),
	}
	for i, item := range invalid {
		if item == string(raw) {
			t.Fatalf("invalid fixture %d unchanged", i)
		}
		if _, err := decode([]byte(item), strings.Repeat("f", 64)); !errors.Is(err, ErrReport) {
			t.Fatalf("invalid fixture %d was admitted: %v", i, err)
		}
	}
}
