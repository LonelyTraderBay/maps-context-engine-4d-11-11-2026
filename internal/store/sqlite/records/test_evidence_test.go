package records

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

	testevidence "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/tests"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestC072TestEvidenceCommitsAndReopensUnderAuthority(t *testing.T) {
	ctx, db, store, _, identity := initialized(t)
	base := t.TempDir()
	root, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	meta := testevidence.Metadata{SchemaVersion: 1, Kind: testevidence.TestReport, Environment: "fixture",
		Command: []string{"go", "test", "./..."}, CompletedAt: "2026-11-21T18:30:00Z",
		Capture: testevidence.Capture{WorktreeID: identity.WorktreeID, ProfileID: strings.Repeat("a", 64),
			Revision: strings.Repeat("b", 40), SnapshotDigest: strings.Repeat("c", 64)}}
	test := testevidence.TestResult{TestID: "TestNoExtraAssertion", Path: "store_test.go", ContentHash: strings.Repeat("d", 64),
		Outcome: "passed", AssertionIDs: []string{}}
	hit := testevidence.CoverageHit{Path: "store.go", ContentHash: strings.Repeat("e", 64),
		Range: model.ByteRange{StartByte: 7, EndByte: 17}, HitCount: 1}
	req := TestEvidenceImportRequest{Root: root, Writer: &fixtureWriter{}, JobID: fixtureJob, Authorize: allowed,
		Approval: testevidence.Approval{WorktreeID: identity.WorktreeID, Environment: meta.Environment, Command: meta.Command}}
	check := func(ctx context.Context, got *paths.Root, name, digest string) error {
		if got != root || name != req.RelativePath || digest != req.Approval.InputHash {
			return fixtureDenied
		}
		return ctx.Err()
	}
	req.Approval.Recheck = check
	write := func(name string, tests []testevidence.TestResult, coverage []testevidence.CoverageHit) {
		t.Helper()
		raw, err := json.Marshal(struct {
			Metadata testevidence.Metadata      `json:"metadata"`
			Tests    []testevidence.TestResult  `json:"tests"`
			Coverage []testevidence.CoverageHit `json:"coverage"`
		}{meta, append([]testevidence.TestResult{}, tests...), append([]testevidence.CoverageHit{}, coverage...)})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		req.RelativePath, req.Approval.InputHash = name, hex.EncodeToString(sum[:])
	}
	write("test.json", []testevidence.TestResult{test}, nil)
	if _, err := store.ImportTestEvidence(ctx, req); !errors.Is(err, ErrEvidenceMigrationRequired) {
		t.Fatalf("unapproved schema upgrade: %v", err)
	}
	req.AllowUpgrade = true
	checks := 0
	req.Approval.Recheck = func(ctx context.Context, root *paths.Root, name, digest string) error {
		checks++
		if checks == 4 {
			return fixtureDenied
		}
		return check(ctx, root, name, digest)
	}
	if _, err := store.ImportTestEvidence(ctx, req); !errors.Is(err, testevidence.ErrApproval) || checks != 4 {
		t.Fatalf("revoked test evidence committed: %d %v", checks, err)
	}
	var version int
	if err := db.QueryRow(`SELECT schema_version FROM schema_meta`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("revocation left a partial upgrade: %d %v", version, err)
	}
	req.Approval.Recheck = check
	report, err := store.ImportTestEvidence(ctx, req)
	if err != nil || report.ItemCount != 1 || report.RecordEpoch != 1 || report.ArtifactID != req.Approval.InputHash {
		t.Fatalf("approved test report: %+v %v", report, err)
	}
	req.AllowUpgrade = false
	replay, err := store.ImportTestEvidence(ctx, req)
	if err != nil || !replay.Replayed || replay.RecordEpoch != report.RecordEpoch {
		t.Fatalf("test artifact replay: %+v %v", replay, err)
	}
	meta.Kind = testevidence.CoverageReport
	write("coverage.json", nil, []testevidence.CoverageHit{hit})
	coverage, err := store.ImportTestEvidence(ctx, req)
	if err != nil || coverage.ItemCount != 1 || coverage.RecordEpoch != 2 {
		t.Fatalf("approved coverage report: %+v %v", coverage, err)
	}
	reopened, err := Open(ctx, db, identity.WorktreeID, store.lease)
	if err != nil {
		t.Fatal(err)
	}
	result, state, err := reopened.ReadTestEvidence(ctx, report.ArtifactID, allowed)
	if err != nil || state.Identity != identity || state.RecordEpoch != 2 ||
		len(result.Tests()) != 1 || result.Tests()[0].Outcome != "passed" || len(result.Tests()[0].AssertionIDs) != 0 {
		t.Fatalf("read approved test without assertions: %+v %v", state, err)
	}
	covered, state, err := reopened.ReadTestEvidence(ctx, coverage.ArtifactID, allowed)
	if err != nil || state.RecordEpoch != 2 || len(covered.Tests()) != 0 || len(covered.Coverage()) != 1 ||
		covered.Coverage()[0] != hit || len(result.Coverage()) != 0 {
		t.Fatalf("read approved coverage without test claims: %+v %v", state, err)
	}
	if _, _, err := reopened.ReadTrace(ctx, coverage.ArtifactID, allowed); !errors.Is(err, ErrTraceUnavailable) {
		t.Fatalf("coverage decoded as runtime trace: %v", err)
	}
	reads := 0
	result, state, err = reopened.ReadTestEvidence(ctx, report.ArtifactID, func(context.Context, Identity) error {
		reads++
		if reads == 2 {
			return fixtureDenied
		}
		return nil
	})
	if !errors.Is(err, fixtureDenied) || result.InputHash() != "" || state != (State{}) {
		t.Fatalf("revoked report emitted data: %+v %v", state, err)
	}
}
