//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	testevidence "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/tests"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

func TestProtectedTestEvidenceFixture(t *testing.T) {
	ctx, profile := profileCatalogFixture(t, true)
	base := t.TempDir()
	root, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	meta := testevidence.Metadata{SchemaVersion: 1, Kind: testevidence.TestReport, Environment: "fixture",
		Command: []string{"fixture-report-only"}, CompletedAt: "2026-11-21T18:30:00Z",
		Capture: testevidence.Capture{WorktreeID: ownerWorktree, ProfileID: strings.Repeat("a", 64),
			Revision: strings.Repeat("b", 40), SnapshotDigest: strings.Repeat("c", 64)}}
	raw, err := json.Marshal(struct {
		Metadata testevidence.Metadata      `json:"metadata"`
		Tests    []testevidence.TestResult  `json:"tests"`
		Coverage []testevidence.CoverageHit `json:"coverage"`
	}{meta, []testevidence.TestResult{{TestID: "TestOwner", Path: "owner_test.go", ContentHash: strings.Repeat("d", 64),
		Outcome: "passed", AssertionIDs: []string{}}}, []testevidence.CoverageHit{}})
	if err != nil {
		t.Fatal(err)
	}
	const name = "synthetic-test-report.json"
	if err := os.WriteFile(filepath.Join(base, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	id := hex.EncodeToString(hash[:])
	input := ProtectedTestEvidenceImport{SourceRoot: root, RelativePath: name, ApproveUpgrade: true,
		Approval: testevidence.Approval{InputHash: id, WorktreeID: ownerWorktree, Environment: meta.Environment, Command: meta.Command,
			Recheck: func(ctx context.Context, candidate *paths.Root, path, digest string) error {
				if candidate != root || path != name || digest != id {
					return ErrProfileDenied
				}
				return ctx.Err()
			}}}
	allowed := func(ctx context.Context) error { return ctx.Err() }
	result, err := ImportProtectedTestEvidence(ctx, profile, ownerWorktree, time.Second, input, allowed)
	if err != nil || result.ArtifactID != id || result.RecordEpoch != 1 || result.ItemCount != 1 || result.Replayed {
		t.Fatalf("write test evidence through protected owner: %+v %v", result, err)
	}
	input.ApproveUpgrade = false
	replay, err := ImportProtectedTestEvidence(ctx, profile, ownerWorktree, time.Second, input, allowed)
	if err != nil || replay.RecordEpoch != 1 || !replay.Replayed {
		t.Fatalf("test report protected replay: %+v %v", replay, err)
	}
	owner, err := OpenProtectedWorktree(ctx, profile, ownerWorktree, allowed)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Records.Store()
	if err != nil {
		t.Fatal(err)
	}
	report, state, err := store.ReadTestEvidence(ctx, id, func(ctx context.Context, identity records.Identity) error {
		if identity.WorktreeID != ownerWorktree || identity.Incarnation != owner.manifest.RecordsIncarnation {
			return ErrProfileDenied
		}
		return ctx.Err()
	})
	if err != nil || state.RecordEpoch != result.RecordEpoch || len(report.Tests()) != 1 || len(report.Tests()[0].AssertionIDs) != 0 {
		t.Fatalf("protected report read: %+v %v", state, err)
	}
}
