// Command spec_check checks the documented project tracker and contract blocks.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	spec := flag.String("spec", "Maps-Context-Engine-4D_SPEC.md", "path to the project specification")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	data, err := os.ReadFile(*spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	errs := validate(string(data), filepath.Dir(*spec))
	if len(errs) > 0 {
		for _, issue := range errs {
			fmt.Fprintln(os.Stderr, issue)
		}
		os.Exit(1)
	}
	fmt.Println("PASS: selected specification and tracker checks; not product, platform or release verification")
}

type task struct {
	id, gate, status, evidence string
	deps, requirements, cases  []string
}

var (
	sectionPattern = regexp.MustCompile(`(?m)^## S([0-9]{2}) —`)
	idPattern      = regexp.MustCompile(`([TRC])([0-9]{3})((\.\.|–|-)([TRC])?([0-9]{3}))?`)
	versionPattern = regexp.MustCompile(`(?m)^spec_version: ([^\r\n]+)$`)
	resumeVersion  = regexp.MustCompile(`(?m)^  spec_version: ([^\r\n]+)$`)
	evidenceID     = regexp.MustCompile(`^E(?:[0-9]{3}|[1-9][0-9]{3,})$`)
)

func validate(text, root string) []string {
	var issues []string
	add := func(format string, args ...any) { issues = append(issues, fmt.Sprintf(format, args...)) }
	if strings.Count(text, "\n```")%2 != 0 {
		add("unbalanced Markdown fences")
	}
	headings := sectionPattern.FindAllStringSubmatch(text, -1)
	if len(headings) != 54 {
		add("section count: got %d, want 54", len(headings))
	}
	for i, heading := range headings {
		if heading[1] != fmt.Sprintf("%02d", i) {
			add("section order or duplicate: %s", heading[1])
		}
	}
	v, rv := versionPattern.FindStringSubmatch(text), resumeVersion.FindStringSubmatch(text)
	if len(v) != 2 || len(rv) != 2 || v[1] != rv[1] || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(v[1]) {
		add("spec and resume version mismatch")
	}
	reqs := tableIDs(section(text, "S01", "S02"), 'R', 130, add)
	cases := tableIDs(section(text, "S17", "S18"), 'C', 192, add)
	tracker := section(text, "S19", "S20")
	tasks := make(map[string]task)
	for _, row := range tableRows(tracker, regexp.MustCompile(`^T[0-9]{3}$`)) {
		if len(row) != 9 {
			add("task row has %d columns", len(row))
			continue
		}
		id := row[0]
		if _, ok := tasks[id]; ok {
			add("duplicate task %s", id)
			continue
		}
		tasks[id] = task{id: id, gate: row[1], deps: ids(row[2]), requirements: ids(row[4]), cases: ids(row[5]), status: row[6], evidence: row[8]}
	}
	if len(tasks) != 108 {
		add("task count: got %d, want 108", len(tasks))
	}
	evidence := map[string][]string{}
	for _, row := range tableRows(section(text, "S21", "S22"), evidenceID) {
		if len(row) != 8 {
			add("evidence row has %d columns", len(row))
			continue
		}
		if _, ok := evidence[row[0]]; ok {
			add("duplicate evidence %s", row[0])
		}
		evidence[row[0]] = row
		if row[5] != "PASS" && row[5] != "FAIL" && row[5] != "NOT_RUN" {
			add("unknown evidence result %s", row[0])
		}
	}
	if len(tableRows(text, evidenceID)) != len(tableRows(section(text, "S21", "S22"), evidenceID)) {
		add("evidence outside S21")
	}
	last := yamlScalar(section(text, "S22", "S23"), "last_evidence_id")
	if last != "null" {
		if _, ok := evidence[last]; !ok {
			add("last evidence missing: %s", last)
		}
	}
	states := map[string]bool{"TODO": true, "IN_PROGRESS": true, "BLOCKED": true, "VERIFY": true, "DONE": true, "DEFERRED": true, "CANCELLED": true}
	for n := 1; n <= 108; n++ {
		id := fmt.Sprintf("T%03d", n)
		t, ok := tasks[id]
		if !ok {
			add("missing task %s", id)
			continue
		}
		if !states[t.status] {
			add("unknown task status %s: %s", id, t.status)
		}
		if len(t.requirements) == 0 || len(t.cases) == 0 {
			add("task %s lacks requirement or case traceability", id)
		}
		for _, dep := range t.deps {
			if _, ok := tasks[dep]; !ok {
				add("task %s has missing dependency %s", id, dep)
			}
		}
		for _, req := range t.requirements {
			if !reqs[req] {
				add("task %s has missing requirement %s", id, req)
			}
		}
		for _, c := range t.cases {
			if !cases[c] {
				add("task %s has missing case %s", id, c)
			}
		}
		if t.evidence != "NONE" {
			for _, e := range strings.Split(t.evidence, ",") {
				if row, exists := evidence[strings.TrimSpace(e)]; !exists || row[1] != id {
					add("task %s has unknown or mismatched evidence %s", id, e)
				}
			}
		}
		if t.status == "DONE" {
			passed := false
			for _, e := range strings.Split(t.evidence, ",") {
				row, exists := evidence[strings.TrimSpace(e)]
				if exists && row[1] == id && row[5] == "PASS" {
					passed = true
				}
			}
			if !passed {
				add("task %s DONE without matching PASS evidence", id)
			}
		}
	}
	for _, row := range tableRows(section(text, "S01", "S02"), regexp.MustCompile(`^R[0-9]{3}$`)) {
		if len(row) != 5 {
			add("requirement %s row incomplete", row[0])
			continue
		}
		refs := ids(row[4])
		if len(refs) == 0 {
			add("requirement %s lacks task traceability", row[0])
		}
		for _, id := range refs {
			if _, exists := tasks[id]; !exists {
				add("requirement %s has missing task %s", row[0], id)
			}
		}
	}
	checkPlan(text, tasks, add)
	checkGates(text, tasks, add)
	checkTaskReadiness(text, tasks, add)
	checkBatch(text, root, tasks, add)
	checkProgress(text, tasks, add)
	checkPolicies(text, root, add)
	checkClaims(text, add)
	checkContracts(text, root, tasks, add)
	checkAudit(text, add)
	sort.Strings(issues)
	return issues
}

func section(text, start, end string) string {
	a, b := strings.Index(text, "## "+start+" "), strings.Index(text, "## "+end+" ")
	if a < 0 || b <= a {
		return ""
	}
	return text[a:b]
}

func tableRows(text string, id *regexp.Regexp) [][]string {
	var rows [][]string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(strings.TrimSpace(line), "|") {
			continue
		}
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) < 3 {
			continue
		}
		row := parts[1 : len(parts)-1]
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		if id.MatchString(row[0]) {
			rows = append(rows, row)
		}
	}
	return rows
}

func tableIDs(text string, prefix byte, count int, add func(string, ...any)) map[string]bool {
	seen := map[string]bool{}
	pattern := regexp.MustCompile(fmt.Sprintf(`^%c[0-9]{3}$`, prefix))
	for _, row := range tableRows(text, pattern) {
		if seen[row[0]] {
			add("duplicate %s %s", map[byte]string{'R': "requirement", 'C': "case"}[prefix], row[0])
		}
		seen[row[0]] = true
	}
	for n := 1; n <= count; n++ {
		id := fmt.Sprintf("%c%03d", prefix, n)
		if !seen[id] {
			add("missing ID %s", id)
		}
	}
	if len(seen) != count {
		add("%c ID count: got %d, want %d", prefix, len(seen), count)
	}
	return seen
}

func ids(s string) []string {
	var result []string
	for _, m := range idPattern.FindAllStringSubmatch(s, -1) {
		first, _ := strconv.Atoi(m[2])
		last := first
		if m[6] != "" {
			if m[5] != "" && m[5] != m[1] {
				continue
			}
			last, _ = strconv.Atoi(m[6])
		}
		if last < first || last-first > 999 {
			continue
		}
		for n := first; n <= last; n++ {
			result = append(result, fmt.Sprintf("%s%03d", m[1], n))
		}
	}
	return result
}

func checkGates(text string, tasks map[string]task, add func(string, ...any)) {
	gateRows := tableRows(section(text, "S20", "S21"), regexp.MustCompile(`^G(U)?[0-9] —`))
	if len(gateRows) != 12 {
		add("gate count mismatch: %d", len(gateRows))
	}
	for _, row := range gateRows {
		if len(row) != 4 {
			add("gate row incomplete")
			continue
		}
		status := row[3]
		if status != "PASS" && status != "FAIL" && status != "NOT_RUN" {
			add("unknown gate status %s", status)
		}
		if status == "PASS" {
			for _, id := range ids(row[1]) {
				if tasks[id].status != "DONE" {
					add("gate %s PASS with unfinished task %s", row[0], id)
				}
			}
		}
	}
}

// checkTaskReadiness enforces the predecessor gate and dependencies for every
// claimed task. R2 and Explorer subsets open after G5 without waiting for G6.
func checkTaskReadiness(text string, tasks map[string]task, add func(string, ...any)) {
	previous := map[string]string{"G0": "", "G1": "G0", "G2": "G1", "G3": "G2", "G4": "G3", "G5": "G4", "R2": "G5", "R3": "G6", "U0": "G5", "U1": "GU0", "U2": "GU1", "U3": "GU2"}
	gates := map[string]string{}
	for _, row := range tableRows(section(text, "S20", "S21"), regexp.MustCompile(`^G(U)?[0-9] —`)) {
		if len(row) == 4 {
			fields := strings.Fields(row[0])
			if len(fields) > 0 {
				gates[fields[0]] = row[3]
			}
		}
	}
	for id, t := range tasks {
		prev, known := previous[t.gate]
		if !known {
			add("task %s has unknown gate %s", id, t.gate)
			continue
		}
		if t.status != "IN_PROGRESS" && t.status != "VERIFY" && t.status != "DONE" {
			continue
		}
		for _, dep := range t.deps {
			if tasks[dep].status != "DONE" {
				add("claimed task %s has unfinished dependency %s", id, dep)
			}
		}
		if prev != "" && gates[prev] != "PASS" {
			add("claimed task %s requires previous gate %s PASS", id, prev)
		}
	}
}

func checkPlan(text string, tasks map[string]task, add func(string, ...any)) {
	plan := section(text, "S47", "S48")
	phase := regexp.MustCompile(`^P[0-9]{2}`)
	position := map[string]int{}
	for _, row := range tableRows(plan, phase) {
		if len(row) < 2 {
			add("phase row missing schedule")
			continue
		}
		for _, id := range ids(row[1]) {
			if _, ok := position[id]; ok {
				add("duplicate task in schedule %s", id)
			} else {
				position[id] = len(position)
			}
		}
	}
	if len(position) != len(tasks) {
		add("schedule task count mismatch")
	}
	for id, t := range tasks {
		pos, ok := position[id]
		if !ok {
			add("task %s missing from schedule", id)
			continue
		}
		for _, dep := range t.deps {
			if dp, ok := position[dep]; !ok || dp >= pos {
				add("dependency order %s before %s is invalid", dep, id)
			}
		}
	}
	cards := map[string]bool{}
	for _, row := range tableRows(plan, regexp.MustCompile(`^[0-9]{3}$`)) {
		if len(row) != 6 {
			add("work card incomplete")
			continue
		}
		if cards[row[1]] || tasks[row[1]].id == "" {
			add("duplicate or unknown work card %s", row[1])
		}
		cards[row[1]] = true
		for _, cell := range row {
			if cell == "" {
				add("work card %s has empty field", row[1])
			}
		}
	}
	if len(cards) != len(tasks) {
		add("work card count mismatch")
	}
}

func yamlScalar(text, key string) string {
	pattern := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(key) + `: ([^\r\n]+)$`)
	m := pattern.FindStringSubmatch(text)
	if len(m) != 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func checkBatch(text, root string, tasks map[string]task, add func(string, ...any)) {
	part := section(text, "S21", "S22")
	start := strings.Index(part, "git_batch_control:")
	if start < 0 {
		add("batch metadata missing")
		return
	}
	part = part[start:]
	end := strings.Index(part, "\n```")
	if end < 0 {
		add("batch metadata fence missing")
		return
	}
	part = part[:end]
	if yamlScalar(part, "batch_size") != "10" {
		add("batch size differs from policy")
	}
	count, err := strconv.Atoi(yamlScalar(part, "active_done_count"))
	if err != nil || count < 0 || count > 10 {
		add("batch invalid done count")
		return
	}
	active := ids(yamlScalar(part, "active_task_ids"))
	if len(active) != count {
		add("batch count does not match task list")
	}
	typ, final, authority := yamlScalar(part, "batch_type"), yamlScalar(part, "assignment_scope_final"), yamlScalar(part, "authority_ref")
	switch typ {
	case "", "null":
	case "full":
		if count != 10 {
			add("batch full type requires ten tasks")
		}
	case "scope_final":
		if count < 1 || count >= 10 || final != "true" || authority == "" || authority == "null" {
			add("batch final tail lacks valid scope or authority metadata")
		}
	case "authorized_recovery":
		if authority == "" || authority == "null" {
			add("batch recovery lacks authority metadata")
		}
	default:
		add("batch type is unknown")
	}
	seen := map[string]bool{}
	for _, id := range active {
		if seen[id] || tasks[id].status != "DONE" {
			add("batch has duplicate or unfinished task %s", id)
		}
		seen[id] = true
	}
	resume := section(text, "S22", "S23")
	if yamlScalar(resume, "git_batch_done_count") != strconv.Itoa(count) {
		add("batch resume count mismatch")
	}
	if count == 10 && yamlScalar(resume, "active_task") != "null" {
		add("batch is full but another task is active")
	}
	position := map[string]int{}
	var schedule []string
	for _, row := range tableRows(section(text, "S47", "S48"), regexp.MustCompile(`^P[0-9]{2}`)) {
		if len(row) > 1 {
			for _, id := range ids(row[1]) {
				position[id] = len(position)
				schedule = append(schedule, id)
			}
		}
	}
	if yamlScalar(part, "active_batch_id") == "B001" && (typ == "" || typ == "null" || typ == "full") {
		for i, id := range active {
			if i >= len(schedule) || id != schedule[i] {
				add("batch B001 must follow S47 prefix")
				break
			}
		}
		claimed := yamlScalar(resume, "active_task")
		if claimed != "null" && claimed != "" && (len(active) >= len(schedule) || claimed != schedule[len(active)]) {
			add("batch B001 active task is not next in S47 order")
		}
	}
	for i := 1; i < len(active); i++ {
		if position[active[i]] <= position[active[i-1]] {
			add("batch task order is invalid")
		}
	}
	if strings.Contains(part, "  commit_binding: containing_commit") {
		for _, key := range []string{"current_commit_oid", "current_tree_oid"} {
			if value := yamlScalar(part, key); value != "" && value != "null" {
				add("batch metadata may not embed own commit or tree hash")
			}
		}
	}
	oid, tag := yamlScalar(part, "last_verified_checkpoint_oid"), yamlScalar(part, "last_verified_checkpoint_tag")
	if oid != "null" || tag != "null" {
		if err := checkCheckpoint(root, oid, tag); err != nil {
			add("batch checkpoint: %v", err)
		}
	}
}

func checkProgress(text string, tasks map[string]task, add func(string, ...any)) {
	resume := section(text, "S22", "S23")
	r1, totals := map[string]int{}, map[string]int{}
	for n := 1; n <= 108; n++ {
		status := tasks[fmt.Sprintf("T%03d", n)].status
		totals[status]++
		if n <= 42 || (n >= 49 && n <= 54) || n >= 105 {
			r1[status]++
		}
	}
	if yamlScalar(resume, "total_r1_tasks") != "52" || yamlScalar(resume, "done_r1_tasks") != strconv.Itoa(r1["DONE"]) {
		add("R1 progress count does not match tracker")
	}
	summary := regexp.MustCompile(`(?m)^- R1: 52 task; DONE=([0-9]+); IN_PROGRESS=([0-9]+); BLOCKED=([0-9]+); VERIFY=([0-9]+); TODO=([0-9]+);`).FindStringSubmatch(section(text, "S19", "S20"))
	if len(summary) != 6 {
		add("R1 progress summary missing")
	} else {
		for i, status := range []string{"DONE", "IN_PROGRESS", "BLOCKED", "VERIFY", "TODO"} {
			if summary[i+1] != strconv.Itoa(r1[status]) {
				add("R1 progress summary mismatch: %s", status)
			}
		}
	}
	all := regexp.MustCompile(`(?m)^- Toàn roadmap: 108 tasks; ([^;]+);`).FindStringSubmatch(section(text, "S19", "S20"))
	if len(all) != 2 {
		add("roadmap progress summary missing")
	} else {
		reported := map[string]int{}
		for _, part := range regexp.MustCompile(`([0-9]+) (TODO|IN_PROGRESS|BLOCKED|VERIFY|DONE|DEFERRED|CANCELLED)`).FindAllStringSubmatch(all[1], -1) {
			value, _ := strconv.Atoi(part[1])
			if _, exists := reported[part[2]]; exists {
				add("duplicate roadmap progress state: %s", part[2])
			}
			reported[part[2]] = value
		}
		for _, status := range []string{"TODO", "IN_PROGRESS", "BLOCKED", "VERIFY", "DONE", "DEFERRED", "CANCELLED"} {
			if reported[status] != totals[status] {
				add("roadmap progress summary mismatch: %s", status)
			}
		}
	}
	active := yamlScalar(resume, "active_task")
	if active != "null" && tasks[active].status != "IN_PROGRESS" {
		add("resume active task is not in progress")
	}
	if next := yamlScalar(resume, "next_task"); next != "null" {
		t, exists := tasks[next]
		if !exists {
			add("resume next task is unknown")
		} else if t.status == "TODO" || t.status == "IN_PROGRESS" {
			for _, dep := range t.deps {
				if tasks[dep].status != "DONE" {
					add("resume next task %s has unfinished dependency %s", next, dep)
				}
			}
		}
	}
}

func checkClaims(text string, add func(string, ...any)) {
	resume := section(text, "S22", "S23")
	mode := yamlScalar(resume, "last_request_mode")
	if mode != "implement" && mode != "integrate" && mode != "review" && mode != "docs_only" {
		add("unknown task mode: %s", mode)
	}
	label := yamlScalar(resume, "release_label")
	if label != "NOT_BUILT" && label != "EXPERIMENTAL_LOCAL" && label != "R1_VALIDATED_ON_FIXTURES" && label != "R1_VALIDATED_FOR_PERSONAL_USE" {
		add("unknown release label: %s", label)
	}
	if label == "R1_VALIDATED_ON_FIXTURES" || label == "R1_VALIDATED_FOR_PERSONAL_USE" {
		gate := tableRows(section(text, "S20", "S21"), regexp.MustCompile(`^G5 —`))
		if len(gate) != 1 || len(gate[0]) != 4 || gate[0][3] != "PASS" {
			add("release label lacks G5 PASS")
		}
		if yamlScalar(resume, "product_build") != "PASS" || yamlScalar(resume, "product_tests") != "PASS" {
			add("release label lacks product build/test evidence")
		}
	}
	front := regexp.MustCompile(`(?m)^policy_runtime_enforcement: ([^\r\n]+)$`).FindStringSubmatch(text)
	if len(front) != 2 || front[1] != yamlScalar(resume, "policy_runtime_enforcement") {
		add("policy enforcement metadata mismatch")
	}
}

func checkPolicies(text, root string, add func(string, ...any)) {
	policies := []struct{ name, alternate, hash string }{
		{"AI_RULES.md", "AI_RULES(8).md", "1662406d9ac9e63646130516ad6811934b2bd7f0a6b7d240403749200777f9e3"},
		{"AI_RULES_STORAGE_SUPPLEMENT.md", "AI_RULES_STORAGE_SUPPLEMENT(1).md", "eea81c9ac63344efde5f1eccb30e43d06146875aabaf8396fec3b46e5e878dac"},
		{"PROJECT_BOOTSTRAP_PROMPT.txt", "PROJECT_BOOTSTRAP_PROMPT(4).txt", "b1d692184f9b4bac87f6b55b25e1c49bab90af5d5eb1706a428a09d85212b686"},
	}
	ledger := section(text, "S50", "S51")
	for _, policy := range policies {
		if !strings.Contains(ledger, policy.hash) {
			add("policy ledger missing hash for %s", policy.name)
		}
		found := false
		for _, name := range []string{policy.name, policy.alternate} {
			data, err := os.ReadFile(filepath.Join(root, "docs", name))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				add("policy %s cannot be read: %v", name, err)
				continue
			}
			found = true
			normal := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
			windows := bytes.ReplaceAll(normal, []byte("\n"), []byte("\r\n"))
			if digest(data) != policy.hash && digest(normal) != policy.hash && digest(windows) != policy.hash {
				add("policy %s hash or content mismatch", name)
			}
		}
		if !found {
			add("policy %s is missing", policy.name)
		}
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func checkAudit(text string, add func(string, ...any)) {
	closure := section(text, "S52", "S53")
	for i := 1; i <= 8; i++ {
		marker := fmt.Sprintf("| A%02d ", i)
		if strings.Count(closure, marker) != 1 {
			add("audit closure marker %s missing or duplicated", marker)
			continue
		}
		line := strings.SplitN(closure[strings.Index(closure, marker):], "\n", 2)[0]
		if !strings.Contains(line, "CLOSED_IN_SPEC") {
			add("audit closure status missing: %s", marker)
		}
	}
}

func markedBlock(text, name, language string) ([]byte, error) {
	begin, end := "<!-- BEGIN "+name+" -->", "<!-- END "+name+" -->"
	if strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		return nil, fmt.Errorf("marker %s missing or duplicated", name)
	}
	a, b := strings.Index(text, begin)+len(begin), strings.Index(text, end)
	if b <= a {
		return nil, fmt.Errorf("marker %s order invalid", name)
	}
	contents := strings.TrimSpace(text[a:b])
	opening := "```" + language + "\n"
	if !strings.HasPrefix(contents, opening) || !strings.HasSuffix(contents, "```") {
		return nil, fmt.Errorf("marker %s has invalid fence", name)
	}
	return []byte(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(contents, opening), "```"))), nil
}

func checkContracts(text, root string, tasks map[string]task, add func(string, ...any)) {
	objects := map[string]any{}
	for _, name := range []string{"MCE4D_HASH_VECTORS_V1", "MCE4D_RUNTIME_DEFAULTS_V1", "MCE4D_CONTRACT_SCHEMA_V1", "MCE4D_OPERATION_REGISTRY_V1"} {
		block, err := markedBlock(text, name, "json")
		if err != nil {
			add("%v", err)
			continue
		}
		value, err := decodeJSON(block)
		if err != nil {
			add("%s JSON: %v", name, err)
			continue
		}
		objects[name] = value
	}
	schema, schemaOK := objects["MCE4D_CONTRACT_SCHEMA_V1"].(map[string]any)
	if schemaOK {
		if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || schema["$id"] != "urn:mce4d:contracts:1" || schema["$ref"] != "#/$defs/Response" {
			add("schema authority version or root reference invalid")
		}
		defs, ok := schema["$defs"].(map[string]any)
		if !ok || len(defs) != 153 {
			add("schema definition count mismatch")
		}
		checkRefs(schema, schema, add)
		if registry, ok := objects["MCE4D_OPERATION_REGISTRY_V1"].(map[string]any); ok {
			checkRegistry(registry, defs, add)
		}
		if defaults, ok := objects["MCE4D_RUNTIME_DEFAULTS_V1"].(map[string]any); ok {
			checkDefaults(defaults, defs, add)
		}
		checkGenerated(root, "contracts/mce4d.v1.schema.json", schema, tasks["T105"].status == "DONE", add)
	}
	if registry, ok := objects["MCE4D_OPERATION_REGISTRY_V1"].(map[string]any); ok {
		checkGenerated(root, "contracts/operations.v1.json", registry, tasks["T105"].status == "DONE", add)
	}
	if vectors, ok := objects["MCE4D_HASH_VECTORS_V1"].(map[string]any); ok {
		valid, ok := vectors["valid"].([]any)
		if !ok || len(valid) != 14 {
			add("hash vector count mismatch")
		}
		for _, item := range valid {
			v, ok := item.(map[string]any)
			if !ok {
				add("invalid hash vector")
				continue
			}
			encoded, _ := v["encoding_hex"].(string)
			want, _ := v["digest"].(string)
			data, err := hex.DecodeString(encoded)
			if err != nil || digest(data) != want || !bytes.HasPrefix(data, []byte("MCE4D\x00C14N1\x00")) {
				add("hash vector digest invalid: %v", v["case"])
			}
		}
		if reject, ok := vectors["reject_identity_values"].([]any); !ok || len(reject) != 6 {
			add("hash rejection vector count mismatch")
		}
	}
	ddl := []string{"MCE4D_DDL_CATALOG_001", "MCE4D_DDL_INDEX_001", "MCE4D_DDL_INDEX_002", "MCE4D_DDL_RECORDS_001", "MCE4D_DDL_INDEX_003", "MCE4D_DDL_INDEX_004", "MCE4D_DDL_RECORDS_002"}
	last := -1
	paths := []string{"catalog/001.sql", "index/001.sql", "index/002.sql", "records/001.sql", "index/003.sql", "index/004.sql", "records/002.sql"}
	for i, name := range ddl {
		position := strings.Index(text, "<!-- BEGIN "+name+" -->")
		if position <= last {
			add("DDL migration order invalid: %s", name)
		}
		last = position
		block, err := markedBlock(text, name, "sql")
		if err != nil || len(block) == 0 {
			add("DDL marker or SQL block invalid: %s", name)
			continue
		}
		path := filepath.Join(root, "internal", "store", "sqlite", "migrations", filepath.FromSlash(paths[i]))
		file, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			if i < 4 && tasks["T106"].status == "DONE" {
				add("required DDL artifact missing: %s", paths[i])
			}
			continue
		}
		if err != nil {
			add("cannot read DDL artifact %s: %v", paths[i], err)
			continue
		}
		if !bytes.Equal(bytes.TrimSpace(bytes.ReplaceAll(file, []byte("\r\n"), []byte("\n"))), block) {
			add("DDL artifact differs from authority: %s", paths[i])
		}
	}
}

func checkGenerated(root, name string, expected any, required bool, add func(string, ...any)) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if os.IsNotExist(err) {
		if required {
			add("required generated contract missing: %s", name)
		}
		return
	}
	if err != nil {
		add("cannot read generated contract %s: %v", name, err)
		return
	}
	actual, err := decodeJSON(data)
	if err != nil {
		add("generated contract %s: %v", name, err)
		return
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		add("generated contract %s differs from authority", name)
	}
}

func checkRefs(value, root any, add func(string, ...any)) {
	switch x := value.(type) {
	case map[string]any:
		for key, item := range x {
			if key == "$ref" {
				ref, ok := item.(string)
				if !ok || !strings.HasPrefix(ref, "#/") || !pointerExists(root, ref) {
					add("invalid schema reference: %v", item)
				}
			}
			checkRefs(item, root, add)
		}
	case []any:
		for _, item := range x {
			checkRefs(item, root, add)
		}
	}
}

func pointerExists(root any, pointer string) bool {
	current := root
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = object[part]
		if !ok {
			return false
		}
	}
	return true
}

func checkRegistry(registry, defs map[string]any, add func(string, ...any)) {
	if len(registry) != 31 {
		add("registry operation count mismatch")
	}
	counts := map[string]int{}
	for name, raw := range registry {
		switch {
		case strings.HasPrefix(name, "mce4d_"):
			counts["mcp"]++
		case strings.HasPrefix(name, "POST /") || strings.HasPrefix(name, "GET /"):
			counts["http"]++
		case strings.HasPrefix(name, "CLI "):
			counts["cli"]++
		default:
			add("registry invalid transport: %s", name)
		}
		pair, ok := raw.([]any)
		if !ok || len(pair) != 2 {
			add("registry invalid request/data mapping: %s", name)
			continue
		}
		for _, item := range pair {
			name, ok := item.(string)
			if !ok || defs[name] == nil {
				add("registry missing schema reference: %v", item)
			}
		}
	}
	for _, name := range []string{"mce4d_workspace_status", "mce4d_locate", "mce4d_context_pack", "mce4d_expand_pack", "mce4d_validate_pack", "mce4d_rename_preview"} {
		if _, ok := registry[name]; !ok {
			add("registry missing MCP tool: %s", name)
		}
	}
	if counts["mcp"] != 6 || counts["http"] != 18 || counts["cli"] != 7 {
		add("registry transport counts mismatch")
	}
}

func checkDefaults(defaults, defs map[string]any, add func(string, ...any)) {
	if version, ok := defaults["schema_version"].(json.Number); !ok || version.String() != "1" || len(defaults) != 3 {
		add("runtime defaults version or fields invalid")
	}
	for name, schemaName := range map[string]string{"core": "CoreRuntimeLimits", "explorer": "ExplorerRuntimeLimits"} {
		values, ok := defaults[name].(map[string]any)
		def, defOK := defs[schemaName].(map[string]any)
		props, propsOK := def["properties"].(map[string]any)
		if !ok || !defOK || !propsOK || len(values) != len(props) {
			add("runtime defaults fields mismatch: %s", name)
			continue
		}
		for key, value := range values {
			constraint, ok := props[key].(map[string]any)
			n, nOK := value.(json.Number)
			if !ok || !nOK {
				add("runtime default invalid: %s.%s", name, key)
				continue
			}
			v, err := n.Int64()
			if err != nil {
				add("runtime default not integer: %s.%s", name, key)
				continue
			}
			for _, bound := range []string{"minimum", "maximum"} {
				if n, ok := constraint[bound].(json.Number); ok {
					limit, err := n.Int64()
					if err != nil || (bound == "minimum" && v < limit) || (bound == "maximum" && v > limit) {
						add("runtime default out of bounds: %s.%s", name, key)
					}
				}
			}
		}
	}
}

// decodeJSON uses tokens rather than json.Unmarshal so duplicate keys cannot be hidden.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := jsonValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value")
		}
		return nil, err
	}
	return value, nil
}

func jsonValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("JSON object key is not a string")
			}
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", name)
			}
			value, err := jsonValue(dec)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("invalid JSON object ending")
		}
		return object, nil
	case '[':
		var array []any
		for dec.More() {
			value, err := jsonValue(dec)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("invalid JSON array ending")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}
