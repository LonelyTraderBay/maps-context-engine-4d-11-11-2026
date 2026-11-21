// Package testevidence validates owner-approved local reports without running tests.
package testevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const (
	MaxBytes         = 1 << 20
	MaxItems         = 1024
	SanitizerVersion = "test-evidence-v1"
	TestReport       = "test_report"
	CoverageReport   = "coverage_report"
	maxExactInteger  = 9007199254740991
)

var (
	ErrApproval = errors.New("test evidence approval unavailable")
	ErrReport   = errors.New("test evidence report is invalid")
	ErrDigest   = errors.New("test evidence differs from the approved digest")
	label       = regexp.MustCompile("^[A-Za-z][A-Za-z0-9._-]{0,95}$")
	testID      = regexp.MustCompile("^[A-Za-z][A-Za-z0-9._/-]{0,191}$")
	argument    = regexp.MustCompile("^[-A-Za-z0-9._/=:]{1,128}$")
	revisionID  = regexp.MustCompile("^(?:[0-9a-f]{40}|[0-9a-f]{64})$")
)

type Capture struct {
	WorktreeID     string `json:"worktree_id"`
	ProfileID      string `json:"profile_id"`
	Revision       string `json:"revision"`
	SnapshotDigest string `json:"snapshot_digest"`
}

type Metadata struct {
	SchemaVersion int      `json:"schema_version"`
	Kind          string   `json:"kind"`
	Capture       Capture  `json:"capture"`
	Environment   string   `json:"environment"`
	Command       []string `json:"command"`
	CompletedAt   string   `json:"completed_at"`
}

type TestResult struct {
	TestID       string   `json:"test_id"`
	Path         string   `json:"path"`
	ContentHash  string   `json:"content_hash"`
	Outcome      string   `json:"outcome"`
	AssertionIDs []string `json:"assertion_ids"`
}

type CoverageHit struct {
	Path        string          `json:"path"`
	ContentHash string          `json:"content_hash"`
	Range       model.ByteRange `json:"range"`
	HitCount    int64           `json:"hit_count"`
}

// Report is immutable admitted metadata. Neither test outcomes nor coverage
// imply source binding or prove an assertion.
type Report struct {
	hash     string
	metadata Metadata
	tests    []TestResult
	coverage []CoverageHit
}

type Approval struct {
	InputHash   string
	WorktreeID  string
	Environment string
	Command     []string
	Recheck     func(context.Context, *paths.Root, string, string) error
}

type wire struct {
	Metadata json.RawMessage   `json:"metadata"`
	Tests    []json.RawMessage `json:"tests"`
	Coverage []json.RawMessage `json:"coverage"`
}

func (r Report) InputHash() string { return r.hash }

func (r Report) Metadata() Metadata {
	value := r.metadata
	value.Command = slices.Clone(value.Command)
	return value
}

func (r Report) Tests() []TestResult {
	rows := slices.Clone(r.tests)
	for i := range rows {
		rows[i].AssertionIDs = slices.Clone(rows[i].AssertionIDs)
	}
	return rows
}

func (r Report) Coverage() []CoverageHit {
	return slices.Clone(r.coverage)
}

// Read pins the approved bytes before interpreting their closed metadata shape.
func Read(ctx context.Context, root *paths.Root, file string, approved Approval) (Report, error) {
	if ctx == nil || root == nil || approved.Recheck == nil || !model.ValidDigest(approved.InputHash) ||
		!model.ValidUUID(approved.WorktreeID) || !label.MatchString(approved.Environment) || !validCommand(approved.Command) {
		return Report{}, ErrApproval
	}
	if err := approved.Recheck(ctx, root, file, approved.InputHash); err != nil {
		return Report{}, ErrApproval
	}
	scanner, err := policy.NewScanner(MaxBytes)
	if err != nil {
		return Report{}, err
	}
	raw, err := scanner.ReadSource(root, file)
	if err != nil {
		return Report{}, ErrReport
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != approved.InputHash {
		return Report{}, ErrDigest
	}
	report, err := decode(raw, approved.InputHash)
	if err != nil {
		return Report{}, err
	}
	if m := report.metadata; m.Capture.WorktreeID != approved.WorktreeID ||
		m.Environment != approved.Environment || !slices.Equal(m.Command, approved.Command) {
		return Report{}, ErrApproval
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if err := approved.Recheck(ctx, root, file, approved.InputHash); err != nil {
		return Report{}, ErrApproval
	}
	return report, nil
}

// DecodeStored revalidates bounded normalized rows from an authorized records
// snapshot. The independently authenticated input hash identifies the export.
func DecodeStored(inputHash string, metadata json.RawMessage, tests, coverage []json.RawMessage) (Report, error) {
	if !model.ValidDigest(inputHash) || len(tests)+len(coverage) > MaxItems {
		return Report{}, ErrReport
	}
	size := len(metadata)
	for _, set := range [][]json.RawMessage{tests, coverage} {
		for _, row := range set {
			size += len(row)
			if size > MaxBytes {
				return Report{}, ErrReport
			}
		}
	}
	raw, err := json.Marshal(wire{Metadata: metadata, Tests: append([]json.RawMessage{}, tests...), Coverage: append([]json.RawMessage{}, coverage...)})
	if err != nil {
		return Report{}, ErrReport
	}
	return decode(raw, inputHash)
}

func decode(raw []byte, hash string) (Report, error) {
	if _, err := model.DecodeIdentityJSON(raw, MaxBytes); err != nil {
		return Report{}, ErrReport
	}
	var rows wire
	if !closed(raw, "metadata", "tests", "coverage") || json.Unmarshal(raw, &rows) != nil ||
		rows.Tests == nil || rows.Coverage == nil || len(rows.Tests)+len(rows.Coverage) > MaxItems {
		return Report{}, ErrReport
	}
	out := Report{hash: hash, tests: make([]TestResult, 0, len(rows.Tests)), coverage: make([]CoverageHit, 0, len(rows.Coverage))}
	if !decodeMetadata(rows.Metadata, &out.metadata) {
		return Report{}, ErrReport
	}
	switch out.metadata.Kind {
	case TestReport:
		if len(rows.Tests) == 0 || len(rows.Coverage) != 0 {
			return Report{}, ErrReport
		}
	case CoverageReport:
		if len(rows.Coverage) == 0 || len(rows.Tests) != 0 {
			return Report{}, ErrReport
		}
	default:
		return Report{}, ErrReport
	}
	testIDs := make(map[string]bool, len(rows.Tests))
	for _, raw := range rows.Tests {
		test, ok := decodeTest(raw)
		if !ok || testIDs[test.TestID] {
			return Report{}, ErrReport
		}
		testIDs[test.TestID] = true
		out.tests = append(out.tests, test)
	}
	type coverageKey struct {
		path       string
		start, end int64
	}
	covered := make(map[coverageKey]bool, len(rows.Coverage))
	for _, raw := range rows.Coverage {
		hit, ok := decodeCoverage(raw)
		key := coverageKey{path: hit.Path, start: hit.Range.StartByte, end: hit.Range.EndByte}
		if !ok || covered[key] {
			return Report{}, ErrReport
		}
		covered[key] = true
		out.coverage = append(out.coverage, hit)
	}
	return out, nil
}

func decodeMetadata(raw []byte, m *Metadata) bool {
	if !closed(raw, "schema_version", "kind", "capture", "environment", "command", "completed_at") ||
		json.Unmarshal(raw, m) != nil || m.SchemaVersion != 1 || !label.MatchString(m.Environment) ||
		!validCommand(m.Command) || !strings.HasSuffix(m.CompletedAt, "Z") {
		return false
	}
	var nested struct{ Capture json.RawMessage }
	if json.Unmarshal(raw, &nested) != nil || !closed(nested.Capture, "worktree_id", "profile_id", "revision", "snapshot_digest") {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, m.CompletedAt)
	c := m.Capture
	return err == nil && model.ValidUUID(c.WorktreeID) && model.ValidDigest(c.ProfileID) &&
		revisionID.MatchString(c.Revision) && model.ValidDigest(c.SnapshotDigest)
}

func validCommand(command []string) bool {
	if len(command) < 1 || len(command) > 16 {
		return false
	}
	for _, arg := range command {
		if !argument.MatchString(arg) {
			return false
		}
	}
	return true
}

func decodeTest(raw []byte) (TestResult, bool) {
	var test TestResult
	if !closed(raw, "test_id", "path", "content_hash", "outcome", "assertion_ids") ||
		json.Unmarshal(raw, &test) != nil || !testID.MatchString(test.TestID) || policy.CheckPath(test.Path) != nil ||
		!model.ValidDigest(test.ContentHash) || test.AssertionIDs == nil || len(test.AssertionIDs) > 32 {
		return TestResult{}, false
	}
	switch test.Outcome {
	case "passed", "failed", "skipped", "unknown":
	default:
		return TestResult{}, false
	}
	seen := make(map[string]bool, len(test.AssertionIDs))
	for _, id := range test.AssertionIDs {
		if !label.MatchString(id) || seen[id] {
			return TestResult{}, false
		}
		seen[id] = true
	}
	return test, true
}

func decodeCoverage(raw []byte) (CoverageHit, bool) {
	var hit CoverageHit
	var nested struct{ Range json.RawMessage }
	if !closed(raw, "path", "content_hash", "range", "hit_count") || json.Unmarshal(raw, &hit) != nil ||
		json.Unmarshal(raw, &nested) != nil || !closed(nested.Range, "start_byte", "end_byte") ||
		policy.CheckPath(hit.Path) != nil || !model.ValidDigest(hit.ContentHash) ||
		hit.Range.StartByte < 0 || hit.Range.EndByte <= hit.Range.StartByte || hit.Range.EndByte > maxExactInteger ||
		hit.HitCount < 0 || hit.HitCount > maxExactInteger {
		return CoverageHit{}, false
	}
	return hit, true
}

func closed(raw []byte, names ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if value, ok := fields[name]; !ok || string(value) == "null" {
			return false
		}
	}
	return true
}

// Alignment compares evidence with the approved capture selected by the caller.
// AssertionIDs remain host claims even when the capture and every file match.
func (r Report) Alignment(revision string, capture *snapshot.Snapshot) string {
	if capture == nil || !revisionID.MatchString(revision) || r.hash == "" {
		return "unbound"
	}
	m := capture.Manifest()
	pin := r.metadata.Capture
	if m.WorktreeID != pin.WorktreeID || m.ProfileID != pin.ProfileID {
		return "unbound"
	}
	if revision != pin.Revision || m.SnapshotDigest != pin.SnapshotDigest {
		return "historical"
	}
	for _, test := range r.tests {
		if file, ok := capture.File(test.Path); !ok || file.ContentHash() != test.ContentHash {
			return "unbound"
		}
	}
	for _, item := range r.coverage {
		if file, ok := capture.File(item.Path); !ok || file.ContentHash() != item.ContentHash ||
			item.Range.EndByte > int64(file.Len()) || file.CheckRange(int(item.Range.StartByte), int(item.Range.EndByte)) != nil {
			return "unbound"
		}
	}
	return "matching_capture"
}
