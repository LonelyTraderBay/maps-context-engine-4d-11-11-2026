package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestCurrentSpecAndMutationRejections(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "Maps-Context-Engine-4D_SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	valid := string(data)
	if errs := validate(valid, root); len(errs) != 0 {
		t.Fatalf("current spec has errors: %v", errs)
	}
	var firstTask string
	for _, line := range strings.Split(valid, "\n") {
		if strings.HasPrefix(line, "| T001 |") {
			firstTask = line
			break
		}
	}
	fields := strings.Split(firstTask, "|")
	if len(fields) != 11 {
		t.Fatal("T001 task row missing")
	}
	fields[7], fields[9] = " DONE ", " NONE "
	badDone := strings.Join(fields, "|")
	resume := section(valid, "S22", "S23")
	version := yamlScalar(resume, "spec_version")
	progress := yamlScalar(resume, "done_r1_tasks")
	next := yamlScalar(resume, "next_task")
	var notReady string
	rows := tableRows(section(valid, "S19", "S20"), regexp.MustCompile(`^T[0-9]{3}$`))
	states := map[string]string{}
	for _, row := range rows {
		states[row[0]] = row[6]
	}
	for _, row := range rows {
		if row[6] != "TODO" && row[6] != "IN_PROGRESS" {
			continue
		}
		for _, dep := range ids(row[2]) {
			if states[dep] != "DONE" {
				notReady = row[0]
				break
			}
		}
		if notReady != "" {
			break
		}
	}
	tests := []struct {
		name, old, replacement, want string
	}{
		{"duplicate requirement", "| R002 | P0 |", "| R001 | P0 |", "duplicate requirement"},
		{"missing task", "| T003 | G0 | T001 |", "| T999 | G0 | T001 |", "task"},
		{"done without evidence", firstTask, badDone, "evidence"},
		{"wrong evidence result", "| E004 | T002 |", "| E004 | T999 |", "evidence"},
		{"evidence outside log", "| T004 | G1 |", "| E999 | misplaced |\n| T004 | G1 |", "evidence outside"},
		{"unknown last evidence", "  last_evidence_id: " + yamlScalar(resume, "last_evidence_id"), "  last_evidence_id: E999", "last evidence"},
		{"backward dependency", "T001 → T002 → T003 → T105", "T002 → T001 → T003 → T105", "order"},
		{"bad schema ref", `"$ref": "#/$defs/Response"`, `"$ref": "#/$defs/NotDefined"`, "reference"},
		{"external schema ref", `"$ref": "#/$defs/Response"`, `"$ref": "https://example.test/schema"`, "reference"},
		{"bad resume version", "  spec_version: " + version, "  spec_version: invalid", "version"},
		{"wrong progress", "  done_r1_tasks: " + progress, "  done_r1_tasks: 999", "progress"},
		{"false release label", "  release_label: " + yamlScalar(resume, "release_label"), "  release_label: UNKNOWN_LABEL", "release label"},
		{"false enforcement", "policy_runtime_enforcement: NOT_IMPLEMENTED", "policy_runtime_enforcement: VERIFIED", "enforcement"},
		{"bad hash vector", "9a3a326d97dd4ca8e7648816695f8e2ae848098b19025f76d5bc717987a42ac0", "0a3a326d97dd4ca8e7648816695f8e2ae848098b19025f76d5bc717987a42ac0", "hash vector"},
		{"missing marker", "<!-- END MCE4D_OPERATION_REGISTRY_V1 -->", "<!-- END REMOVED -->", "marker"},
		{"missing audit closure", "| A01 GC FK |", "| A99 GC FK |", "audit closure"},
	}
	if notReady != "" && next != notReady {
		tests = append(tests, struct{ name, old, replacement, want string }{"next task not ready", "  next_task: " + next, "  next_task: " + notReady, "unfinished dependency"})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(valid, tc.old) {
				t.Fatalf("mutation anchor missing: %q", tc.old)
			}
			mutated := strings.Replace(valid, tc.old, tc.replacement, 1)
			errs := validate(mutated, root)
			if !strings.Contains(strings.ToLower(strings.Join(errs, "\n")), strings.ToLower(tc.want)) {
				t.Fatalf("expected %q rejection, got %v", tc.want, errs)
			}
		})
	}
}

func TestBatchBoundariesAndTaskOrder(t *testing.T) {
	all := []string{"T001", "T002", "T003", "T105", "T106", "T107", "T108", "T005", "T004", "T006", "T007"}
	tasks := map[string]task{}
	for _, id := range all {
		tasks[id] = task{id: id, status: "DONE"}
	}
	makeSpec := func(n int, active string) string {
		return "## S21 — test\n```yaml\ngit_batch_control:\n  batch_size: 10\n  active_done_count: " + strconv.Itoa(n) + "\n  active_task_ids: [" + strings.Join(all[:n], ", ") + "]\n  last_verified_checkpoint_oid: null\n  last_verified_checkpoint_tag: null\n```\n" +
			"## S22 — test\n  git_batch_done_count: " + strconv.Itoa(n) + "\n  active_task: " + active + "\n" +
			"## S23 — test\n## S47 — test\n| P00 Foundation | " + strings.Join(all, " → ") + " | G0 |\n## S48 — test\n"
	}
	for _, n := range []int{9, 10} {
		var errs []string
		checkBatch(makeSpec(n, "null"), "", tasks, func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) })
		if len(errs) != 0 {
			t.Fatalf("batch %d: %v", n, errs)
		}
	}
	for _, tc := range []struct {
		n      int
		active string
	}{{10, "T007"}, {11, "null"}} {
		var errs []string
		checkBatch(makeSpec(tc.n, tc.active), "", tasks, func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) })
		if len(errs) == 0 {
			t.Fatalf("invalid batch %+v was accepted", tc)
		}
	}
	tail := strings.Replace(makeSpec(9, "null"), "  batch_size: 10", "  batch_size: 10\n  batch_type: scope_final\n  assignment_scope_final: false\n  authority_ref: null", 1)
	var tailErrors []string
	checkBatch(tail, "", tasks, func(format string, args ...any) { tailErrors = append(tailErrors, fmt.Sprintf(format, args...)) })
	if !strings.Contains(strings.Join(tailErrors, "\n"), "authority") {
		t.Fatalf("unauthorized final tail was not rejected: %v", tailErrors)
	}
	for _, status := range []string{"TODO", "VERIFY", "BLOCKED", "DEFERRED", "CANCELLED"} {
		tasks["T006"] = task{id: "T006", status: status}
		var errs []string
		checkBatch(makeSpec(10, "null"), "", tasks, func(format string, args ...any) {
			errs = append(errs, fmt.Sprintf(format, args...))
		})
		if !strings.Contains(strings.Join(errs, "\n"), "unfinished task T006") {
			t.Fatalf("%s still counted toward the batch: %v", status, errs)
		}
	}
	tasks["T006"] = task{id: "T006", status: "DONE"}
	duplicate := strings.Replace(makeSpec(10, "null"), "T004, T006]", "T004, T004]", 1)
	var duplicateErrors []string
	checkBatch(duplicate, "", tasks, func(format string, args ...any) {
		duplicateErrors = append(duplicateErrors, fmt.Sprintf(format, args...))
	})
	if !strings.Contains(strings.Join(duplicateErrors, "\n"), "duplicate or unfinished task T004") {
		t.Fatalf("duplicate task counted toward the batch: %v", duplicateErrors)
	}
}

func TestPolicyMissingAndModifiedRejected(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "Maps-Context-Engine-4D_SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	if err := os.Mkdir(filepath.Join(temp, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	names := []string{"AI_RULES.md", "AI_RULES_STORAGE_SUPPLEMENT.md", "PROJECT_BOOTSTRAP_PROMPT.txt"}
	for _, name := range names {
		contents, err := os.ReadFile(filepath.Join(root, "docs", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(temp, "docs", name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(temp, "docs", names[0]), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errs := validate(string(data), temp); !strings.Contains(strings.Join(errs, "\n"), "hash or content mismatch") {
		t.Fatalf("modified policy not rejected: %v", errs)
	}
	if err := os.Remove(filepath.Join(temp, "docs", names[1])); err != nil {
		t.Fatal(err)
	}
	if errs := validate(string(data), temp); !strings.Contains(strings.Join(errs, "\n"), "is missing") {
		t.Fatalf("missing policy not rejected: %v", errs)
	}
}

func TestPolicyAliasesDuplicatesAndLineEndings(t *testing.T) {
	root := filepath.Join("..", "..")
	spec, err := os.ReadFile(filepath.Join(root, "Maps-Context-Engine-4D_SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	docs := filepath.Join(temp, "docs")
	if err := os.Mkdir(docs, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AI_RULES.md", "AI_RULES_STORAGE_SUPPLEMENT.md", "PROJECT_BOOTSTRAP_PROMPT.txt"} {
		data, err := os.ReadFile(filepath.Join(root, "docs", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(docs, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mainPath, alias := filepath.Join(docs, "AI_RULES.md"), filepath.Join(docs, "AI_RULES(8).md")
	original, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(mainPath, alias); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		var problems []string
		checkPolicies(string(spec), temp, func(format string, args ...any) {
			problems = append(problems, fmt.Sprintf(format, args...))
		})
		if want == "" && len(problems) != 0 || want != "" && !strings.Contains(strings.Join(problems, "\n"), want) {
			t.Fatalf("policy diagnostics %v, expected %q", problems, want)
		}
	}
	check("")
	if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
		t.Fatal("checking the alias recreated the preferred path")
	}
	lineEndings := strings.ReplaceAll(string(original), "\r\n", "\n")
	if lineEndings == string(original) {
		lineEndings = strings.ReplaceAll(lineEndings, "\n", "\r\n")
	}
	if err := os.WriteFile(alias, []byte(lineEndings), 0o600); err != nil {
		t.Fatal(err)
	}
	check("")
	if err := os.WriteFile(mainPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	check("")
	if err := os.WriteFile(alias, original, 0o600); err != nil {
		t.Fatal(err)
	}
	check("")
	changed := strings.Replace(lineEndings, "3.1", "3.2", 1)
	if changed == lineEndings {
		t.Fatal("fixture has no version marker")
	}
	if err := os.WriteFile(alias, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	check("AI_RULES(8).md hash or content mismatch")
	got, err := os.ReadFile(alias)
	if err != nil || string(got) != changed {
		t.Fatal("checking conflicting policy candidates changed the source")
	}
}

func TestDocsOnlyMetadataKeepsImplementationTracker(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "Maps-Context-Engine-4D_SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	original := string(raw)
	resume := section(original, "S22", "S23")
	mode := yamlScalar(resume, "last_request_mode")
	explicit := strings.Replace(original, "  last_request_mode: "+mode, "  last_request_mode: docs_only", 1)
	if errs := validate(explicit, root); len(errs) != 0 {
		t.Fatalf("docs-only metadata should coexist with the implementation tracker: %v", errs)
	}
	if section(explicit, "S19", "S20") != section(original, "S19", "S20") ||
		yamlScalar(section(explicit, "S22", "S23"), "active_task") != yamlScalar(resume, "active_task") {
		t.Fatal("docs-only metadata changed implementation state")
	}
	inferred := strings.Replace(explicit, "  last_request_mode: docs_only", "  last_request_mode: bootstrap_detected", 1)
	if errs := validate(inferred, root); !strings.Contains(strings.Join(errs, "\n"), "unknown task mode") {
		t.Fatalf("bootstrap presence accepted as an assignment mode: %v", errs)
	}
}

func TestIDRanges(t *testing.T) {
	got := ids("C001..C004,T001–T003")
	if strings.Join(got, ",") != "C001,C002,C003,C004,T001,T002,T003" {
		t.Fatalf("range expansion: %v", got)
	}
}

func TestDuplicateJSONKeysRejected(t *testing.T) {
	if _, err := decodeJSON([]byte(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("duplicate keys must be rejected")
	}
	if _, err := decodeJSON([]byte(`{"a":{"b":1,"b":2}}`)); err == nil {
		t.Fatal("nested duplicate keys must be rejected")
	}
	if _, err := decodeJSON([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("trailing JSON value must be rejected")
	}
}

func TestGeneratedContractParity(t *testing.T) {
	dir := t.TempDir()
	expected := map[string]any{"type": "object"}
	var issues []string
	add := func(format string, args ...any) { issues = append(issues, fmt.Sprintf(format, args...)) }
	checkGenerated(dir, "contract.json", expected, true, add)
	if len(issues) == 0 {
		t.Fatal("missing required generated contract was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "contract.json"), []byte(`{"type":"string"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	issues = nil
	checkGenerated(dir, "contract.json", expected, true, add)
	if len(issues) == 0 {
		t.Fatal("different generated contract was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "contract.json"), []byte("{\n  \"type\": \"object\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	issues = nil
	checkGenerated(dir, "contract.json", expected, true, add)
	if len(issues) != 0 {
		t.Fatalf("equivalent generated contract rejected: %v", issues)
	}
}

func TestClaimedTaskRequiresPreviousGateAndDependencies(t *testing.T) {
	spec := "## S20 — Gates\n| G0 — Foundation | T001 | description | NOT_RUN |\n| G5 — Release | T042 | description | PASS |\n| GU0 — Explorer | T081 | description | PASS |\n## S21 — Evidence\n"
	tasks := map[string]task{"T005": {id: "T005", gate: "G1", status: "IN_PROGRESS", deps: []string{"T108"}}, "T108": {id: "T108", gate: "G0", status: "VERIFY"}, "T043": {id: "T043", gate: "R2", status: "IN_PROGRESS"}, "T085": {id: "T085", gate: "U1", status: "IN_PROGRESS"}}
	var errs []string
	checkTaskReadiness(spec, tasks, func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) })
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "T005 has unfinished dependency T108") || !strings.Contains(joined, "T005 requires previous gate G0 PASS") {
		t.Fatalf("missing rejections: %v", errs)
	}
	if strings.Contains(joined, "T043") || strings.Contains(joined, "T085") {
		t.Fatalf("valid R2/Explorer subset incorrectly blocked: %v", errs)
	}
	tasks["T108"] = task{id: "T108", gate: "G0", status: "DONE"}
	spec = strings.Replace(spec, "T001 | description | NOT_RUN", "T001 | description | PASS", 1)
	errs = nil
	checkTaskReadiness(spec, tasks, func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) })
	if len(errs) != 0 {
		t.Fatalf("ready tasks rejected: %v", errs)
	}
}

func TestValidatorRejectsClaimBeforePreviousGate(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "Maps-Context-Engine-4D_SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := string(data)
	var taskRow, gateRow string
	for _, line := range strings.Split(invalid, "\n") {
		if strings.HasPrefix(line, "| T005 | G1 |") {
			taskRow = line
		}
		if strings.HasPrefix(line, "| G0 — Foundation/design seal |") {
			gateRow = line
		}
	}
	if taskRow == "" || gateRow == "" {
		t.Fatal("fixture anchors missing")
	}
	taskCells := strings.Split(taskRow, "|")
	if len(taskCells) != 11 {
		t.Fatal("task row malformed")
	}
	taskCells[7] = " IN_PROGRESS "
	invalid = strings.Replace(invalid, taskRow, strings.Join(taskCells, "|"), 1)
	gateCells := strings.Split(gateRow, "|")
	if len(gateCells) != 6 {
		t.Fatal("gate row malformed")
	}
	gateCells[4] = " NOT_RUN "
	invalid = strings.Replace(invalid, gateRow, strings.Join(gateCells, "|"), 1)
	errs := validate(invalid, root)
	if !strings.Contains(strings.Join(errs, "\n"), "claimed task T005 requires previous gate G0 PASS") {
		t.Fatalf("gate violation not detected: %v", errs)
	}
}

func TestFirstBatchCannotSkipCompletedTask(t *testing.T) {
	tasks := map[string]task{"T001": {id: "T001", status: "DONE"}, "T002": {id: "T002", status: "DONE"}, "T003": {id: "T003", status: "DONE"}}
	spec := "## S21 — test\n```yaml\ngit_batch_control:\n  batch_size: 10\n  active_batch_id: B001\n  active_done_count: 2\n  active_task_ids: [T001, T003]\n  last_verified_checkpoint_oid: null\n  last_verified_checkpoint_tag: null\n```\n## S22 — test\n  git_batch_done_count: 2\n  active_task: null\n## S23 — test\n## S47 — test\n| P00 Foundation | T001 → T002 → T003 | G0 |\n## S48 — test\n"
	var errs []string
	checkBatch(spec, "", tasks, func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) })
	if !strings.Contains(strings.Join(errs, "\n"), "batch B001 must follow S47 prefix") {
		t.Fatalf("skipped task accepted: %v", errs)
	}
}

func TestFirstBatchCannotClaimTaskOutOfOrder(t *testing.T) {
	tasks := map[string]task{"T001": {id: "T001", status: "DONE"}, "T002": {id: "T002", status: "TODO"}, "T003": {id: "T003", status: "IN_PROGRESS"}}
	spec := "## S21 — test\n```yaml\ngit_batch_control:\n  batch_size: 10\n  active_batch_id: B001\n  active_done_count: 1\n  active_task_ids: [T001]\n  last_verified_checkpoint_oid: null\n  last_verified_checkpoint_tag: null\n```\n## S22 — test\n  git_batch_done_count: 1\n  active_task: T003\n## S23 — test\n## S47 — test\n| P00 Foundation | T001 → T002 → T003 | G0 |\n## S48 — test\n"
	var errs []string
	checkBatch(spec, "", tasks, func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) })
	if !strings.Contains(strings.Join(errs, "\n"), "active task is not next in S47 order") {
		t.Fatalf("out-of-order claim accepted: %v", errs)
	}
}

func TestG0RejectsEachUnfinishedFoundationTask(t *testing.T) {
	spec := "## S20 — Gates\n| G0 — Foundation/design seal | T001–T003,T105–T108 | description | PASS |\n## S21 — Evidence\n"
	foundation := []string{"T001", "T002", "T003", "T105", "T106", "T107", "T108"}
	tasks := map[string]task{}
	for _, id := range foundation {
		tasks[id] = task{id: id, status: "DONE"}
	}
	for _, id := range foundation {
		prior := tasks[id]
		tasks[id] = task{id: id, status: "VERIFY"}
		var errs []string
		checkGates(spec, tasks, func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) })
		if !strings.Contains(strings.Join(errs, "\n"), "unfinished task "+id) {
			t.Fatalf("unfinished %s did not block G0: %v", id, errs)
		}
		tasks[id] = prior
	}
}
