// Package selection recommends tests from an approved source capture and
// local evidence. It does not dispatch a test runner.
package selection

import (
	"context"
	"errors"
	"path"
	"slices"
	"sort"
	"strings"

	testevidence "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/tests"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const (
	MaxChanges    = 512
	MaxReferences = 16384
	MaxCandidates = 256
	MaxReports    = 128
)

var ErrInput = errors.New("invalid test selection input")

type TestKey struct {
	ID   string `json:"test_id"`
	Path string `json:"test_path"`
}

// StaticReference must be supplied by the authorized, pinned graph reader.
type StaticReference struct {
	Test       TestKey
	TestHash   string
	SourcePath string
	SourceHash string
}

type Input struct {
	ChangedPaths      []string
	Capture           *snapshot.Snapshot
	Revision          string
	StaticReferences  []StaticReference
	InventoryComplete bool
	Evidence          []testevidence.Report
	SharedPrefixes    []string
	RequiredSuites    []string
	FullSuite         string
	Limit             int
}

type Reason struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type Candidate struct {
	Test    TestKey  `json:"test"`
	Reasons []Reason `json:"reasons"`
}

type ReportContext struct {
	ArtifactID string   `json:"artifact_id"`
	Kind       string   `json:"kind"`
	Alignment  string   `json:"alignment"`
	HitPaths   []string `json:"hit_paths"`
}

type Plan struct {
	Candidates     []Candidate     `json:"candidates"`
	RequiredSuites []string        `json:"required_suites"`
	ReportContext  []ReportContext `json:"report_context"`
	Gaps           []Reason        `json:"gaps"`
	Fallback       string          `json:"fallback"`
	FullSuite      string          `json:"full_suite,omitempty"`
	Truncated      bool            `json:"truncated"`
}

func Recommend(ctx context.Context, input Input) (Plan, error) {
	if ctx == nil || len(input.ChangedPaths) > MaxChanges || len(input.StaticReferences) > MaxReferences ||
		len(input.Evidence) > MaxReports || len(input.SharedPrefixes) > 64 || len(input.RequiredSuites) > 32 ||
		input.Limit < 1 || input.Limit > MaxCandidates {
		return Plan{}, ErrInput
	}
	out := Plan{Candidates: []Candidate{}, ReportContext: []ReportContext{}, Gaps: []Reason{}, Fallback: "none"}
	required := append([]string{}, input.RequiredSuites...)
	for _, suite := range required {
		if !validID(suite) {
			return Plan{}, ErrInput
		}
	}
	if input.FullSuite != "" && !validID(input.FullSuite) {
		return Plan{}, ErrInput
	}
	sort.Strings(required)
	out.RequiredSuites = slices.Compact(required)
	for _, prefix := range input.SharedPrefixes {
		if policy.CheckPath(prefix) != nil {
			return Plan{}, ErrInput
		}
	}
	changes := append([]string{}, input.ChangedPaths...)
	sort.Strings(changes)
	changes = slices.Compact(changes)
	changed := make(map[string]bool, len(changes))
	for _, file := range changes {
		if policy.CheckPath(file) != nil {
			return Plan{}, ErrInput
		}
		changed[file] = true
		if code := broadRisk(file, input.SharedPrefixes); code != "" {
			out.Gaps = append(out.Gaps, Reason{Code: code, Path: file})
			out.Fallback = "full_regression"
		}
	}
	candidates := make(map[TestKey]map[Reason]bool)
	linked := make(map[string]bool, len(changes))
	for _, ref := range input.StaticReferences {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		if !validID(ref.Test.ID) || policy.CheckPath(ref.Test.Path) != nil || policy.CheckPath(ref.SourcePath) != nil ||
			!model.ValidDigest(ref.TestHash) || !model.ValidDigest(ref.SourceHash) {
			return Plan{}, ErrInput
		}
		if !changed[ref.SourcePath] && !changed[ref.Test.Path] {
			continue
		}
		if !matchesCapture(input.Capture, ref.Test.Path, ref.TestHash) || !matchesCapture(input.Capture, ref.SourcePath, ref.SourceHash) {
			continue
		}
		reasons := candidates[ref.Test]
		if reasons == nil {
			reasons = make(map[Reason]bool, 2)
			candidates[ref.Test] = reasons
		}
		if changed[ref.SourcePath] {
			linked[ref.SourcePath] = true
			reasons[Reason{Code: "static_test_reference", Path: ref.SourcePath}] = true
		}
		if changed[ref.Test.Path] {
			linked[ref.Test.Path] = true
			reasons[Reason{Code: "test_source_changed", Path: ref.Test.Path}] = true
		}
	}
	covered := make(map[string]bool, len(changes))
	artifacts := make(map[string]bool, len(input.Evidence))
	for _, report := range input.Evidence {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		id := report.InputHash()
		if !model.ValidDigest(id) {
			return Plan{}, ErrInput
		}
		if artifacts[id] {
			continue
		}
		artifacts[id] = true
		use := ReportContext{ArtifactID: id, Kind: report.Metadata().Kind, Alignment: report.Alignment(input.Revision, input.Capture),
			HitPaths: []string{}}
		if use.Alignment == "matching_capture" {
			for _, hit := range report.Coverage() {
				if changed[hit.Path] && hit.HitCount > 0 {
					covered[hit.Path] = true
					use.HitPaths = append(use.HitPaths, hit.Path)
				}
			}
		}
		sort.Strings(use.HitPaths)
		use.HitPaths = slices.Compact(use.HitPaths)
		out.ReportContext = append(out.ReportContext, use)
	}
	sort.Slice(out.ReportContext, func(i, j int) bool { return out.ReportContext[i].ArtifactID < out.ReportContext[j].ArtifactID })

	keys := make([]TestKey, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Path == keys[j].Path {
			return keys[i].ID < keys[j].ID
		}
		return keys[i].Path < keys[j].Path
	})
	out.Truncated = len(keys) > input.Limit
	if out.Truncated {
		keys = keys[:input.Limit]
		out.Gaps = append(out.Gaps, Reason{Code: "candidate_limit"})
	}
	for _, key := range keys {
		candidate := Candidate{Test: key, Reasons: make([]Reason, 0, len(candidates[key]))}
		for reason := range candidates[key] {
			candidate.Reasons = append(candidate.Reasons, reason)
		}
		sort.Slice(candidate.Reasons, func(i, j int) bool { return lessReason(candidate.Reasons[i], candidate.Reasons[j]) })
		out.Candidates = append(out.Candidates, candidate)
	}
	for _, file := range changes {
		if !linked[file] {
			out.Gaps = append(out.Gaps, Reason{Code: "no_current_static_test_reference", Path: file})
		}
		if !covered[file] {
			out.Gaps = append(out.Gaps, Reason{Code: "no_current_coverage_hit", Path: file})
		}
	}
	if len(changes) > 0 && !input.InventoryComplete {
		out.Gaps = append(out.Gaps, Reason{Code: "test_inventory_partial"})
	}
	if out.Fallback != "full_regression" && len(out.Gaps) > 0 {
		out.Fallback = "broaden"
	}
	if out.Fallback != "none" && input.FullSuite != "" {
		out.Fallback, out.FullSuite = "full_regression", input.FullSuite
	} else if out.Fallback == "full_regression" {
		out.Gaps = append(out.Gaps, Reason{Code: "full_suite_unknown"})
	}
	sort.Slice(out.Gaps, func(i, j int) bool { return lessReason(out.Gaps[i], out.Gaps[j]) })
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	return out, nil
}

func lessReason(a, b Reason) bool {
	if a.Path == b.Path {
		return a.Code < b.Code
	}
	return a.Path < b.Path
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 192 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == '-' || c == '.' || c == '/') {
			return false
		}
	}
	return true
}

func matchesCapture(capture *snapshot.Snapshot, file, hash string) bool {
	if capture == nil {
		return false
	}
	captured, exists := capture.File(file)
	return exists && captured.ContentHash() == hash
}

func broadRisk(file string, shared []string) string {
	if file == "contracts" || strings.HasPrefix(file, "contracts/") ||
		strings.HasSuffix(file, ".schema.json") || strings.Contains("/"+file+"/", "/migrations/") {
		return "schema_or_migration"
	}
	switch path.Base(file) {
	case "go.mod", "go.sum", "go.work", "package.json", "package-lock.json", "pnpm-lock.yaml",
		"yarn.lock", "pyproject.toml", "uv.lock", "tsconfig.json", "Makefile", "Dockerfile":
		return "build_or_global_configuration"
	}
	if strings.HasPrefix(file, ".github/workflows/") {
		return "build_or_global_configuration"
	}
	for _, prefix := range shared {
		if file == prefix || strings.HasPrefix(file, prefix+"/") {
			return "shared_dependency"
		}
	}
	return ""
}
