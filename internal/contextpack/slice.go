package contextpack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// SliceInput consumes only a previously selected pinned catalog. Policy must
// hash to the selected member's policy; RuleAnchors must be exact captured
// ranges from paths explicitly listed in that policy. Text is always data.
type SliceInput struct {
	Selection   *Selection
	Policy      model.PolicyConfig
	TaskKind    string
	Intent      string
	Constraints []string
	RuleAnchors []model.SourceRef
	// When supplied, this trusted callback must prove that every relevant
	// record store in the pinned vector has no approved records at its epoch.
	// A false result is unknown, not proof that the store is empty.
	VerifyNoApprovedRecords func(context.Context, model.ScopeVector) (bool, error)
	MaxBytes                int
}

// TaskEvidence is an internal candidate, not the S43 wire item. T024/T025
// own budget, continuation and public rendering of individual items.
type TaskEvidence struct {
	ID                string            `json:"id"`
	Kind              string            `json:"kind"`
	RequiredForReview bool              `json:"required_for_review"`
	Why               string            `json:"why"`
	Plane             string            `json:"plane"`
	Trust             string            `json:"trust"`
	SourceRefs        []model.SourceRef `json:"source_refs"`
	Content           *string           `json:"content"`
	Reference         *Reference        `json:"reference"`
}

type ValidationPlan struct {
	Status         string            `json:"status"`
	TestReferences []model.SourceRef `json:"test_references"`
	Guidance       string            `json:"guidance"`
}

type SliceReport struct {
	Scope               model.ScopeVector `json:"scope"`
	Target              model.SymbolRef   `json:"target"`
	TaskKind            string            `json:"task_kind"`
	Intent              string            `json:"intent"` // user-supplied data, never authorization
	Items               []TaskEvidence    `json:"items"`
	Gaps                []model.Gap       `json:"gaps"`
	Validation          ValidationPlan    `json:"validation"`
	EnumerationComplete bool              `json:"enumeration_complete"`
}

type TaskSlice struct {
	selection       *Selection
	report          SliceReport
	rulePaths       []string
	verifyNoRecords func(context.Context, model.ScopeVector) (bool, error)
	retentionCheck  func(context.Context) error
	maxBytes        int
	policyMaxOutput int64
}

// PolicyMaxOutput exposes the already validated cap without granting access to
// the captured selection or its authority callbacks.
func (s *TaskSlice) PolicyMaxOutput() int64 {
	if s == nil {
		return 0
	}
	return s.policyMaxOutput
}

func (s *TaskSlice) authorize(ctx context.Context) error {
	if s == nil || s.selection == nil || ctx == nil {
		return ErrInput
	}
	if s.retentionCheck != nil {
		if err := s.retentionCheck(ctx); err != nil {
			return err
		}
	}
	// Rule and record callbacks inspect this immutable selection privately.
	// The complete selection check below brackets every contributing path with
	// live scope/fact checks after those callbacks and before data can return.
	for _, path := range s.rulePaths {
		if err := s.selection.checkPath(ctx, path); err != nil {
			return err
		}
	}
	if s.verifyNoRecords != nil {
		scope, _, err := authorityCopy(s.selection.report.Scope, s.selection.member)
		if err != nil {
			return err
		}
		ok, err := s.verifyNoRecords(ctx, scope)
		if err != nil || !ok {
			return authorityError(ctx)
		}
	}
	for _, path := range s.rulePaths {
		if err := s.selection.checkPath(ctx, path); err != nil {
			return err
		}
	}
	if err := s.selection.authorize(ctx); err != nil {
		return err
	}
	if s.retentionCheck != nil {
		return s.retentionCheck(ctx)
	}
	return nil
}

// Read rechecks the pinned selection, rule paths and epochs before returning a
// defensive copy. It does not validate the live workspace or authorize edits.
func (s *TaskSlice) Read(ctx context.Context) (SliceReport, error) {
	if s == nil || ctx == nil {
		return SliceReport{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return SliceReport{}, err
	}
	// Defensive copying touches only this immutable materialization. Recheck
	// the complete selection/rules/record authority after copying and before
	// returning; do not cache that proof for another operation.
	raw, err := json.Marshal(s.report)
	if err != nil || len(raw) > s.maxBytes {
		return SliceReport{}, ErrLimit
	}
	var report SliceReport
	if json.Unmarshal(raw, &report) != nil {
		return SliceReport{}, ErrInput
	}
	if err := s.authorize(ctx); err != nil {
		return SliceReport{}, err
	}
	return report, nil
}

type rankedEvidence struct {
	item        TaskEvidence
	rank, order int
}

func evidenceID(scope, task, kind string, refs []model.SourceRef, relation, content string, ordinal int) (string, error) {
	raw, err := json.Marshal(struct {
		Scope, Task, Kind string
		Refs              []model.SourceRef
		Relation, Content string
		Ordinal           int
	}{scope, task, kind, refs, relation, content, ordinal})
	if err != nil {
		return "", ErrInput
	}
	h := sha256.New()
	h.Write([]byte("task-evidence-v1\x00"))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validTask(task string) bool {
	switch task {
	case "explore", "plan", "rename", "type_change", "behavior_change", "contract_change", "debug":
		return true
	}
	return false
}
func validConstraint(c string) bool {
	switch c {
	case "preserve_json_keys", "preserve_db_schema", "no_public_api_change", "preserve_message_schema", "no_source_mutation":
		return true
	}
	return false
}

// BuildTaskSlice makes task-specific review candidates. A known static test
// reference is not a test run; without a proven project command the validation
// plan is explicitly unknown. This method never executes target code.
func BuildTaskSlice(ctx context.Context, in SliceInput) (*TaskSlice, error) {
	s := in.Selection
	if ctx == nil || s == nil || !validTask(in.TaskKind) || !utf8.ValidString(in.Intent) || utf8.RuneCountInString(in.Intent) > 2048 || len(in.Constraints) > 16 || len(in.RuleAnchors) > 128 || in.MaxBytes < 1024 || in.MaxBytes > 16*1024*1024 {
		return nil, ErrInput
	}
	if err := s.scanner.CheckContent([]byte(in.Intent)); err != nil {
		return nil, err
	}
	for _, c := range in.Constraints {
		if !validConstraint(c) {
			return nil, ErrInput
		}
	}
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	rawPolicy, err := json.Marshal(in.Policy)
	if err != nil || len(rawPolicy) > s.maxOutput {
		return nil, ErrLimit
	}
	p, err := model.DecodePolicyConfigJSON(rawPolicy, s.maxOutput)
	if err != nil {
		return nil, ErrInput
	}
	digest, err := model.PolicyDigest(p, s.maxOutput)
	if err != nil || digest != s.member.PolicyDigest {
		return nil, ErrAuthority
	}
	quota := &sourceQuota{maxFile: p.MaxFileBytes, maxTotal: p.MaxSourceBytes, seen: map[string]bool{}}
	report, err := s.Read(ctx)
	if err != nil {
		return nil, err
	}
	ranges := append([]SourceRange(nil), report.Sources...)
	missingBody := false
	if in.TaskKind == "behavior_change" || in.TaskKind == "type_change" || in.TaskKind == "contract_change" || in.TaskKind == "debug" {
		additional, unknown, e := immediateDeclarations(ctx, s, report, quota)
		if e != nil {
			return nil, e
		}
		missingBody = unknown
		ranges = mergeRanges(append(ranges, additional...))
	}
	contents, err := candidateContents(ctx, s, ranges, in.MaxBytes, quota)
	if err != nil {
		return nil, err
	}
	out := &TaskSlice{selection: s, maxBytes: in.MaxBytes, policyMaxOutput: p.MaxOutputBytes, report: SliceReport{Scope: report.Scope, Target: report.Target, TaskKind: in.TaskKind, Intent: in.Intent, Items: []TaskEvidence{}, Gaps: append([]model.Gap{}, report.Gaps...), Validation: ValidationPlan{Status: "unknown", TestReferences: []model.SourceRef{}, Guidance: "Review related test sources and identify validation instructions from the project; execution is a separate host decision."}, EnumerationComplete: report.EnumerationComplete}}
	items := []rankedEvidence{}
	add := func(item TaskEvidence, rank, order int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.SourceRefs == nil {
			item.SourceRefs = []model.SourceRef{}
		}
		key, body := "", ""
		if item.Reference != nil {
			key = item.Reference.Relation.RelationID
		}
		if item.Content != nil {
			body = *item.Content
		}
		id, e := evidenceID(report.Scope.VectorDigest, in.TaskKind, item.Kind, item.SourceRefs, key, body, order)
		if e != nil {
			return e
		}
		item.ID = id
		items = append(items, rankedEvidence{item: item, rank: rank, order: order})
		return nil
	}
	for i, content := range contents {
		selected := ranges[i]
		rank := 4
		required := false
		why := "related_source"
		if selected.Priority == 0 {
			rank, required, why = 0, true, "target_source"
		} else if s.focused && in.TaskKind == "explore" && selected.Priority == 1 {
			rank, why = 1, "related_source"
		} else if in.TaskKind != "explore" && in.TaskKind != "plan" {
			rank, required = 2, true
		}
		body := content.Content
		if err := add(TaskEvidence{Kind: "source_range", RequiredForReview: required, Why: why, Plane: "observed_static", Trust: "captured_source_data", SourceRefs: []model.SourceRef{content.Source}, Content: &body}, rank, 0); err != nil {
			return nil, err
		}
	}
	for i, constraint := range in.Constraints {
		value := constraint
		if err := add(TaskEvidence{Kind: "constraint", RequiredForReview: true, Why: "explicit_task_constraint", Plane: "annotation", Trust: "request_data", Content: &value}, 1, i); err != nil {
			return nil, err
		}
	}
	trusted := map[string]int{}
	for i, path := range p.TrustedRulePaths {
		if _, ok := trusted[path]; !ok {
			trusted[path] = i
		}
	}
	seen := map[model.SourceRef]bool{}
	anchoredPaths := map[string]bool{}
	for _, ref := range in.RuleAnchors {
		precedence, ok := trusted[ref.Path]
		if !ok || seen[ref] || policy.CheckPath(ref.Path) != nil {
			return nil, ErrInput
		}
		seen[ref] = true
		if err := s.checkScope(ctx); err != nil {
			return nil, err
		}
		if err := s.checkPath(ctx, ref.Path); err != nil {
			return nil, err
		}
		if snapshot.ValidateCapturedSource(report.Scope, ref, s.member.ServiceID, s.snapshot, s.maxOutput) != nil {
			return nil, ErrInput
		}
		file, ok := s.snapshot.File(ref.Path)
		if !ok {
			return nil, ErrInput
		}
		if err := quota.charge(file); err != nil {
			return nil, err
		}
		if ref.Range.StartByte == 0 && ref.Range.EndByte == int64(file.Len()) {
			anchoredPaths[ref.Path] = true
		}
		if err := s.scanner.CheckContent(file.Bytes()); err != nil {
			return nil, err
		}
		data, e := file.Range(int(ref.Range.StartByte), int(ref.Range.EndByte))
		if e != nil {
			return nil, ErrInput
		}
		body := string(data)
		if err := add(TaskEvidence{Kind: "constraint_excerpt", RequiredForReview: true, Why: "declared_rule_path", Plane: "intended", Trust: "declared_rule_data", SourceRefs: []model.SourceRef{ref}, Content: &body}, 1, precedence); err != nil {
			return nil, err
		}
		out.rulePaths = append(out.rulePaths, ref.Path)
	}
	for _, ref := range report.References {
		r := ref
		kind, why, rank := "reference_catalog", "selected_reference", 2
		required := in.TaskKind != "explore" && in.TaskKind != "plan" && r.Relation.Kind != "CONTAINS" && r.Relation.Kind != "IMPORTS" && r.Relation.Kind != "DOC_LINK"
		if r.Classification != "static" {
			kind, why, required = "boundary", "uncertain_relation", true
		}
		if r.Relation.Kind == "TEST_REF" {
			kind, why, rank = "test_reference", "static_test_reference", 3
			out.report.Validation.TestReferences = append(out.report.Validation.TestReferences, r.Source)
		}
		if err := add(TaskEvidence{Kind: kind, RequiredForReview: required, Why: why, Plane: "observed_static", Trust: "analyzer_fact", SourceRefs: []model.SourceRef{r.Source}, Reference: &r}, rank, 0); err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.order != b.order {
			return a.order < b.order
		}
		if a.item.Kind != b.item.Kind {
			return a.item.Kind < b.item.Kind
		}
		ap, bp := "", ""
		var as, bs int64
		if len(a.item.SourceRefs) > 0 {
			ap = a.item.SourceRefs[0].Path
			as = a.item.SourceRefs[0].Range.StartByte
		}
		if len(b.item.SourceRefs) > 0 {
			bp = b.item.SourceRefs[0].Path
			bs = b.item.SourceRefs[0].Range.StartByte
		}
		if ap != bp {
			return ap < bp
		}
		if as != bs {
			return as < bs
		}
		return a.item.ID < b.item.ID
	})
	for _, item := range items {
		out.report.Items = append(out.report.Items, item.item)
	}
	sort.Strings(out.rulePaths)
	out.rulePaths = dedupStrings(out.rulePaths)
	addSliceGap(&out.report, "VALIDATION_COMMAND_UNKNOWN", "Static references are not test execution; no verified project validation command was supplied.")
	addSliceGap(&out.report, "LIVE_FRESHNESS_NOT_CHECKED", "Captured evidence does not prove that the current workspace remains unchanged.")
	verifiedEmpty := false
	if in.VerifyNoApprovedRecords != nil {
		scope, _, e := authorityCopy(report.Scope, s.member)
		if e != nil {
			return nil, e
		}
		verifiedEmpty, e = in.VerifyNoApprovedRecords(ctx, scope)
		if e != nil {
			return nil, authorityError(ctx)
		}
		if verifiedEmpty {
			out.verifyNoRecords = in.VerifyNoApprovedRecords
		}
	}
	if !verifiedEmpty {
		out.report.EnumerationComplete = false
		addSliceGap(&out.report, "APPROVED_RECORDS_NOT_QUERIED", "Approved records were not proven absent; declared source text is not a persistent approved decision.")
	}
	if missingBody {
		out.report.EnumerationComplete = false
		addSliceGap(&out.report, "REQUIRED_SOURCE_BODY_UNKNOWN", "At least one relevant declaration body could not be resolved from the captured Go source.")
	}
	for path := range trusted {
		if !anchoredPaths[path] {
			out.report.EnumerationComplete = false
			addSliceGap(&out.report, "TRUSTED_RULES_NOT_ENUMERATED", "At least one configured rule path was not supplied as a complete source range; remaining rule content is unknown.")
			break
		}
	}
	if in.TaskKind == "plan" {
		addSliceGap(&out.report, "INTENDED_MAPPING_UNKNOWN", "Declared text and observed code are not automatically mapped to implemented requirements.")
	}
	if in.TaskKind == "rename" {
		gaps, err := serializationBoundaries(ctx, s, report)
		if err != nil {
			return nil, err
		}
		out.report.Gaps = append(out.report.Gaps, gaps...)
		sort.Slice(out.report.Gaps, func(i, j int) bool { return out.report.Gaps[i].Code < out.report.Gaps[j].Code })
	}
	if err := out.authorize(ctx); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out.report)
	if err != nil || len(raw) > out.maxBytes {
		return nil, ErrLimit
	}
	return out, nil
}

func dedupStrings(values []string) []string {
	out := values[:0]
	for _, v := range values {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
func addSliceGap(report *SliceReport, code, reason string) {
	for _, g := range report.Gaps {
		if g.Code == code {
			return
		}
	}
	report.Gaps = append(report.Gaps, model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{}})
	sort.Slice(report.Gaps, func(i, j int) bool { return report.Gaps[i].Code < report.Gaps[j].Code })
}
