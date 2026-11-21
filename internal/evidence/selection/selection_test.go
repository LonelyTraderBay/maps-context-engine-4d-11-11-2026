package selection

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	testevidence "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/tests"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const fixtureRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func selectionFixture(t *testing.T) Input {
	t.Helper()
	capture := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111",
		ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64),
		DependencyInputDigests: []string{}}
	fileHashes := map[string]string{}
	var files []snapshot.File
	for _, name := range []string{"orders/create.go", "orders/create_test.go", "payments/charge.go",
		"payments/charge_test.go", "shared/ids.go", "contracts/orders.schema.json", "go.mod", "new/worker.go",
		"integration/checkout_test.go"} {
		file, err := snapshot.NewFile(name, []byte("fixture "+name+"\n"), 256)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := file.ContentHash(), int64(file.Len())
		files = append(files, file)
		fileHashes[name] = hash
		capture.Files = append(capture.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size})
	}
	frozen, err := snapshot.Freeze(capture, files, 8192, 8192)
	if err != nil {
		t.Fatal(err)
	}
	coverage := testevidence.Metadata{SchemaVersion: 1, Kind: testevidence.CoverageReport, Environment: "fixture",
		Command: []string{"go", "test", "./..."}, CompletedAt: "2026-11-21T18:30:00Z",
		Capture: testevidence.Capture{WorktreeID: capture.WorktreeID, ProfileID: capture.ProfileID,
			Revision: fixtureRevision, SnapshotDigest: frozen.Manifest().SnapshotDigest}}
	meta, err := json.Marshal(coverage)
	if err != nil {
		t.Fatal(err)
	}
	hit, err := json.Marshal(testevidence.CoverageHit{Path: "orders/create.go", ContentHash: fileHashes["orders/create.go"],
		Range: model.ByteRange{StartByte: 0, EndByte: 7}, HitCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	report, err := testevidence.DecodeStored(strings.Repeat("e", 64), meta, nil, []json.RawMessage{hit})
	if err != nil {
		t.Fatal(err)
	}
	link := func(id, testPath, sourcePath string) StaticReference {
		return StaticReference{Test: TestKey{ID: id, Path: testPath}, TestHash: fileHashes[testPath],
			SourcePath: sourcePath, SourceHash: fileHashes[sourcePath]}
	}
	return Input{Capture: frozen, Revision: fixtureRevision, InventoryComplete: true, Evidence: []testevidence.Report{report},
		StaticReferences: []StaticReference{link("TestCreate", "orders/create_test.go", "orders/create.go"),
			link("TestCreate", "orders/create_test.go", "shared/ids.go"), link("TestCharge", "payments/charge_test.go", "shared/ids.go")},
		SharedPrefixes: []string{"shared"}, RequiredSuites: []string{"migration-regression", "core-regression", "core-regression"},
		FullSuite: "all-tests", Limit: 16}
}

func TestC073IndependentAffectedTestOracle(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/test-selection/c073-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string
		Changed   []string
		Expected  []TestKey
		Suggested []TestKey
		Fallback  string
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) < 4 {
		t.Fatalf("independent oracle: %v", err)
	}
	sumExpected, sumMatched := 0, 0
	for _, scenario := range cases {
		t.Run(scenario.Name, func(t *testing.T) {
			input := selectionFixture(t)
			input.ChangedPaths = scenario.Changed
			out, err := Recommend(t.Context(), input)
			if err != nil || out.Fallback != scenario.Fallback ||
				!slices.Equal(out.RequiredSuites, []string{"core-regression", "migration-regression"}) {
				t.Fatalf("selection: %+v %v", out, err)
			}
			if out.Fallback == "full_regression" && out.FullSuite != "all-tests" {
				t.Fatalf("full regression suite lost: %+v", out)
			}
			actual := make([]TestKey, 0, len(out.Candidates))
			for _, candidate := range out.Candidates {
				actual = append(actual, candidate.Test)
				if len(candidate.Reasons) != 1 || candidate.Reasons[0].Code != "static_test_reference" {
					t.Fatalf("candidate lost evidence rationale: %+v", candidate)
				}
			}
			if !slices.Equal(actual, scenario.Suggested) {
				t.Fatalf("candidate set: %v, expected %v", actual, scenario.Suggested)
			}
			matched := 0
			for _, expected := range scenario.Expected {
				if slices.Contains(actual, expected) {
					matched++
				}
			}
			sumExpected += len(scenario.Expected)
			sumMatched += matched
			t.Logf("C073 case=%s candidate_hits=%d expected=%d fallback=%s", scenario.Name, matched, len(scenario.Expected), out.Fallback)
		})
	}
	if sumExpected != 8 || sumMatched != 3 {
		t.Fatalf("unexpected independent recall numerator/denominator: %d/%d", sumMatched, sumExpected)
	}
	t.Logf("C073 candidate_recall %d/%d; full suites remain recommendations", sumMatched, sumExpected)
}

func TestC073HistoricalCoverageAndTruncationRetainFallback(t *testing.T) {
	input := selectionFixture(t)
	input.ChangedPaths = []string{"orders/create.go"}
	input.Revision = strings.Repeat("f", 40)
	input.FullSuite = ""
	out, err := Recommend(t.Context(), input)
	if err != nil || len(out.Candidates) != 1 || out.Fallback != "broaden" || len(out.ReportContext) != 1 ||
		out.ReportContext[0].Alignment != "historical" || len(out.ReportContext[0].HitPaths) != 0 {
		t.Fatalf("historical coverage hid uncertainty: %+v %v", out, err)
	}
	input.StaticReferences[0].SourceHash = strings.Repeat("0", 64)
	out, err = Recommend(t.Context(), input)
	if err != nil || len(out.Candidates) != 0 || !slices.Contains(out.Gaps, Reason{
		Code: "no_current_static_test_reference", Path: "orders/create.go",
	}) {
		t.Fatalf("unmatched source hash produced a candidate: %+v %v", out, err)
	}
	input = selectionFixture(t)
	input.ChangedPaths, input.Limit = []string{"shared/ids.go", "go.mod"}, 1
	first, err := Recommend(t.Context(), input)
	if err != nil || !first.Truncated || first.Fallback != "full_regression" || len(first.Candidates) != 1 ||
		!slices.Contains(first.Gaps, Reason{Code: "candidate_limit"}) {
		t.Fatalf("truncation removed fallback: %+v %v", first, err)
	}
	slices.Reverse(input.ChangedPaths)
	slices.Reverse(input.StaticReferences)
	second, err := Recommend(t.Context(), input)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("input ordering changed plan: %+v %v", second, err)
	}
}
