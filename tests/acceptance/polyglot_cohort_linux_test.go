//go:build linux

package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type polyglotTestRef struct {
	Source string `json:"source"`
	Name   string `json:"name"`
}

type polyglotCase struct {
	ID         string            `json:"id"`
	Domain     string            `json:"domain"`
	Profile    string            `json:"profile"`
	Capability string            `json:"capability"`
	Tests      []polyglotTestRef `json:"tests"`
}

type polyglotBaselineMode struct {
	Oracle string   `json:"oracle"`
	Scope  []string `json:"scope"`
}

type polyglotFullMode struct {
	Scope   []string `json:"scope"`
	Meaning string   `json:"meaning"`
}

type polyglotIncrementalMode struct {
	Tests []polyglotTestRef `json:"tests"`
	Scope string            `json:"scope"`
}

type polyglotBenchmark struct {
	Kind                 string `json:"kind"`
	RealRepositoryStatus string `json:"real_repository_status"`
	ModelHostStatus      string `json:"model_host_status"`
	UsageExportStatus    string `json:"usage_export_status"`
	InputTokens          *int64 `json:"input_tokens"`
	CachedInputTokens    *int64 `json:"cached_input_tokens"`
	OutputTokens         *int64 `json:"output_tokens"`
	ContextProxyBytes    *int64 `json:"context_proxy_bytes"`
	Rule                 string `json:"rule"`
}

type polyglotMatrix struct {
	SchemaVersion int            `json:"schema_version"`
	CohortID      string         `json:"cohort_id"`
	CorpusKind    string         `json:"corpus_kind"`
	OraclePolicy  string         `json:"oracle_policy"`
	Cases         []polyglotCase `json:"cases"`
	Modes         struct {
		Baseline    polyglotBaselineMode    `json:"baseline"`
		Full        polyglotFullMode        `json:"full"`
		Incremental polyglotIncrementalMode `json:"incremental"`
	} `json:"modes"`
	Benchmark polyglotBenchmark `json:"benchmark"`
}

func TestC069PolyglotCohortManifest(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the acceptance test source")
	}
	acceptanceDir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(acceptanceDir, "..", ".."))
	raw, err := os.ReadFile(filepath.Join(acceptanceDir, "polyglot_capability_matrix.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var matrix polyglotMatrix
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("manifest has trailing JSON: %v", err)
	}
	if matrix.SchemaVersion != 1 || matrix.CohortID != "mce4d-polyglot-fixture-v1" ||
		matrix.CorpusKind != "synthetic" || !strings.Contains(matrix.OraclePolicy, "authored") ||
		!strings.Contains(matrix.OraclePolicy, "engine output is never used") {
		t.Fatal("cohort identity or independent oracle policy is missing")
	}
	if len(matrix.Cases) != 15 {
		t.Fatalf("expected C055-C069, got %d cases", len(matrix.Cases))
	}
	fullScope := make([]string, 0, len(matrix.Cases))
	for i, item := range matrix.Cases {
		expected := fmt.Sprintf("C%03d", 55+i)
		if item.ID != expected || item.Domain == "" || item.Profile == "" || item.Capability == "" || len(item.Tests) == 0 {
			t.Fatalf("case %d is incomplete or out of order: %+v", i, item)
		}
		fullScope = append(fullScope, item.ID)
		for _, ref := range item.Tests {
			checkPolyglotTestRef(t, root, ref)
		}
	}
	if !slices.Equal(matrix.Modes.Baseline.Scope, fullScope) || !slices.Equal(matrix.Modes.Full.Scope, fullScope) ||
		matrix.Modes.Baseline.Oracle == "" || matrix.Modes.Full.Meaning == "" {
		t.Fatal("baseline/full modes do not cover the same closed case set")
	}
	if len(matrix.Modes.Incremental.Tests) != 2 || matrix.Modes.Incremental.Scope == "" {
		t.Fatalf("incremental limits are missing: %+v", matrix.Modes.Incremental)
	}
	incrementalNames := []string{
		matrix.Modes.Incremental.Tests[0].Name,
		matrix.Modes.Incremental.Tests[1].Name,
	}
	if !slices.Equal(incrementalNames, []string{
		"TestInvalidationRebuildPublicationDoesNotExposeOldEdges",
		"TestT064RetainedSQLiteSemanticComparison",
	}) || !strings.Contains(matrix.Modes.Incremental.Scope, "no incremental provider-cache claim") {
		t.Fatalf("incremental evidence or limitation is inaccurate: %+v", matrix.Modes.Incremental)
	}
	for _, ref := range matrix.Modes.Incremental.Tests {
		checkPolyglotTestRef(t, root, ref)
	}
	benchmark := matrix.Benchmark
	if benchmark.Kind != "synthetic_local_fixture" || benchmark.RealRepositoryStatus != "NOT_RUN" ||
		benchmark.ModelHostStatus != "NOT_RUN" || benchmark.UsageExportStatus != "UNAVAILABLE" ||
		benchmark.InputTokens != nil || benchmark.CachedInputTokens != nil || benchmark.OutputTokens != nil ||
		benchmark.ContextProxyBytes != nil || benchmark.Rule == "" {
		t.Fatalf("benchmark metadata overstates available observations: %+v", benchmark)
	}
	t.Log("C069: source-authored baseline oracle, full C055-C069 matrix, scoped incremental regressions; real repo and token metrics remain NOT_RUN")
}

func checkPolyglotTestRef(t *testing.T, root string, ref polyglotTestRef) {
	t.Helper()
	if filepath.IsAbs(ref.Source) || filepath.Clean(filepath.FromSlash(ref.Source)) != filepath.FromSlash(ref.Source) ||
		filepath.Ext(ref.Source) != ".go" || ref.Name == "" {
		t.Fatalf("invalid test reference: %+v", ref)
	}
	path := filepath.Join(root, filepath.FromSlash(ref.Source))
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse referenced Go test %s: %v", ref.Source, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == ref.Name {
			return
		}
	}
	t.Fatalf("referenced Go test %s::%s does not exist", ref.Source, ref.Name)
}
