//go:build linux

package pythonprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func pinnedInstallation(t *testing.T) Installation {
	t.Helper()
	interpreter, stdlib := os.Getenv("MCE4D_T057_PYTHON"), os.Getenv("MCE4D_T057_STDLIB")
	if interpreter == "" || stdlib == "" {
		t.Skip("requires the A048 pinned Linux CPython installation")
	}
	return Installation{Interpreter: interpreter, Stdlib: stdlib, ScratchParent: t.TempDir()}
}

func TestPinnedPythonParsesCaptureWithoutExecution(t *testing.T) {
	installation := pinnedInstallation(t)
	files := map[string]string{
		"src/app.py": "from direct import original as alias\r\n" +
			"@decorate\r\ndef café():\r\n    async def nested():\r\n        return '🌱'\r\n    return nested\r\n" +
			"class Task:\r\n    def run(self):\r\n        return café()\r\n" +
			"__import__(computed_name)\r\nTask.run = replacement\r\n",
		"src/direct.py":    "raise RuntimeError('target import executed')\ndef original(): return 1\n",
		"src/broken.py":    "def incomplete(\n",
		"sitecustomize.py": "from pathlib import Path\nPath('did-run').touch()\n",
		"outside.py":       "def outside(): pass\n",
		"src/startup.pth":  "import sitecustomize\n",
		"src/.venv/lib.py": "raise RuntimeError('virtual environment code executed')\n",
		"src/demo.ipynb":   `{"cells":[{"cell_type":"code","source":["raise RuntimeError('cell executed')"]}],"metadata":{}}`,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	worker, profile, err := New(installation, []string{"src"})
	if err != nil {
		t.Fatal(err)
	}
	checkHash, err := providers.PythonStdlibDigest(ctx, installation.Stdlib)
	if err != nil || checkHash != profile.StdlibDigest {
		t.Fatalf("stdlib pin %q: %v", checkHash, err)
	}
	job, frozen := capturedJob(t, ctx, installation, profile, files)
	root := job.Start.SnapshotRoot
	t.Setenv("PYTHONPATH", root)
	t.Setenv("PYTHONHOME", root)
	t.Setenv("PYTHONSTARTUP", filepath.Join(root, "sitecustomize.py"))
	batches, done := runPython(t, ctx, worker, job)
	replica, repeat := runPython(t, ctx, worker, job)
	if !reflect.DeepEqual(batches, replica) || !reflect.DeepEqual(done, repeat) || done.Status != "partial" ||
		done.VerifyInputDigest(frozen.Manifest().SnapshotDigest) != nil {
		t.Fatalf("Python baseline output was not deterministic or not scoped: %+v", done)
	}
	got := make(map[string]model.SymbolFact)
	for _, batch := range batches {
		if len(batch.Relations) != 0 {
			t.Fatal("syntax worker claimed resolved imports or calls")
		}
		for _, fact := range batch.Symbols {
			got[fact.QualifiedName] = fact
		}
	}
	for _, name := range []string{"app.alias", "app.café", "app.café.nested", "app.Task", "app.Task.run", "direct.original"} {
		if got[name].SymbolID == "" {
			t.Fatalf("missing lexical definition %q: %+v", name, got)
		}
	}
	if len(got) != 6 || got["app.Task.run"].Kind != "method" ||
		got["app.café.nested"].ScopeKey == got["app.café"].ScopeKey {
		t.Fatalf("incorrect lexical scopes: %+v", got)
	}
	definition := got["app.café"]
	sentinel := files[*definition.Path][definition.Range.StartByte:definition.Range.EndByte]
	if sentinel != "def café():\r\n    async def nested():\r\n        return '🌱'\r\n    return nested" {
		t.Fatalf("range changed the captured Unicode or CRLF bytes: %q", sentinel)
	}
	requireGaps(t, done, "PYTHON_SYNTAX_ONLY", "PYTHON_DECORATOR", "PYTHON_DYNAMIC_IMPORT", "PYTHON_RUNTIME_MUTATION",
		"PYTHON_PARSE_PARTIAL", "PYTHON_NOTEBOOK_IGNORED", "PYTHON_ENV_IGNORED", "PYTHON_OUTSIDE_MODULE_ROOT")
	requireInertCapture(t, root, files)
	t.Log("C057/C058: real CPython framed worker, UTF-8 and CRLF scope, zero startup, script or notebook executions")
}

func TestPythonTypedFactsAndNotebookMetadata(t *testing.T) {
	installation := pinnedInstallation(t)
	worker, profile, err := NewTyped(installation, []string{"src"})
	if err != nil {
		t.Fatal(err)
	}
	const module = "src/model.py"
	const notebook = "src/model.ipynb"
	const badNotebook = "src/future.ipynb"
	const duplicateNotebook = "src/duplicate.ipynb"
	files := map[string]string{
		module: "café: forward() = 7\r\nflag = True\r\nruntime_value = factory()\r\n" +
			"@decorator\r\ndef decorated(arg: make_type()) -> alias: ...\r\n" +
			"obj.method = replacement\r\n__import__(selection)\r\n",
		notebook:          `{"nbformat":4,"nbformat_minor":5,"metadata":{"secret":"CELL_OUTPUT_SENTINEL","kernelspec":{"name":"python3"},"language_info":{"name":"python","version":"3.11.8"}},"cells":[{"id":"fixture","cell_type":"code","source":["from pathlib import Path","Path('did-run').touch()"],"outputs":[{"text":"CELL_OUTPUT_SENTINEL"}]}]}`,
		badNotebook:       `{"nbformat":5,"nbformat_minor":0,"metadata":{},"cells":[]}`,
		duplicateNotebook: `{"nbformat":4,"nbformat":4,"nbformat_minor":5,"metadata":{"language_info":{"name":"python"}},"cells":[]}`,
		"src/broken.py":   "def hidden(CELL_OUTPUT_SENTINEL\n",
		"src/startup.pth": "import sitecustomize\n",
		"src/setup.py":    "from pathlib import Path\nPath('did-run').touch()\n",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	job, frozen := capturedJob(t, ctx, installation, profile, files)
	syntax, _, err := New(installation, []string{"src"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := syntax.Analyze(ctx, job, func(context.Context, model.ProviderBatch) error {
		t.Fatal("syntax-only owner accepted a typed job")
		return nil
	}); !errors.Is(err, providers.ErrAuthority) {
		t.Fatalf("profile mode admission: %v", err)
	}
	wrongInstallation := installation
	wrongInstallation.Interpreter = "/usr/bin/false"
	unverified, _, err := NewTyped(wrongInstallation, []string{"src"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unverified.Analyze(ctx, job, func(context.Context, model.ProviderBatch) error {
		t.Fatal("unverified interpreter produced enrichment")
		return nil
	}); !errors.Is(err, providers.ErrAuthority) {
		t.Fatalf("pin admission: %v", err)
	}
	batches, done := runPython(t, ctx, worker, job)
	if done.Status != "partial" || done.VerifyInputDigest(frozen.Manifest().SnapshotDigest) != nil {
		t.Fatalf("invalid coverage or terminal digest: %+v", done)
	}
	requireGaps(t, done, "PYTHON_TYPE_COVERAGE", "PYTHON_PARSE_PARTIAL", "PYTHON_ANNOTATION_UNRESOLVED",
		"PYTHON_DECORATOR", "PYTHON_DYNAMIC_IMPORT", "PYTHON_RUNTIME_MUTATION", "PYTHON_NOTEBOOK_IGNORED",
		"PYTHON_NOTEBOOK_METADATA_UNKNOWN", "PYTHON_ENV_IGNORED")
	caps := map[string]string{}
	for _, item := range done.Capabilities {
		caps[item.Name] = item.Status
	}
	if caps["python_primitive_literal_types"] != "partial" || caps["python_nbformat4_declared_metadata"] != "partial" {
		t.Fatalf("coverage claims differ from actual artifact types: %+v", caps)
	}
	typedFacts := map[string]string{}
	diagnostics := map[string]model.DiagnosticFact{}
	for _, batch := range batches {
		if len(batch.Relations) != 0 {
			t.Fatal("unverified Python runtime bindings")
		}
		for _, fact := range batch.Symbols {
			if strings.HasPrefix(fact.Signature, "literal_expression_type:") {
				typedFacts[fact.QualifiedName] = fact.Signature
				if *fact.Path != module || fact.Range == nil {
					t.Fatalf("missing typed source evidence: %+v", fact)
				}
			}
		}
		for _, item := range batch.Diagnostics {
			diagnostics[*item.Path] = item
			if strings.Contains(item.Message, "CELL_OUTPUT_SENTINEL") || strings.Contains(item.Message, "did-run") {
				t.Fatalf("cell, custom metadata, or parse source leaked: %+v", item)
			}
		}
	}
	if !reflect.DeepEqual(typedFacts, map[string]string{"model.café": "literal_expression_type:int",
		"model.flag": "literal_expression_type:bool"}) {
		t.Fatalf("unexpected typed facts: %+v", typedFacts)
	}
	if len(diagnostics) != 4 || diagnostics[notebook].Message != "Declared Python notebook metadata: format 4.5; language version 3.11.8." ||
		diagnostics[badNotebook].Code != "PYTHON_NOTEBOOK_METADATA_UNKNOWN" ||
		diagnostics[duplicateNotebook].Code != "PYTHON_NOTEBOOK_METADATA_UNKNOWN" || diagnostics["src/broken.py"].Code != "PYTHON_PARSE_PARTIAL" {
		t.Fatalf("notebook and syntax diagnostics: %+v", diagnostics)
	}
	verifyPythonPublication(t, ctx, job, frozen, batches, done)
	requireInertCapture(t, job.Start.SnapshotRoot, files)
	t.Log("C057/C058: published SQLite literal type evidence and safe notebook metadata; unknown runtime calls and configurations stayed inert")
}

func capturedJob(t *testing.T, ctx context.Context, installation Installation, profile *model.PythonProfile, files map[string]string) (ports.ProviderJob, *snapshot.Snapshot) {
	t.Helper()
	root := filepath.Join(installation.ScratchParent, "snapshot")
	profileID, err := model.ProfileDigest(profile, 65536)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111",
		PolicyDigest: sha([]byte("fixture Python policy")), ProfileID: profileID, ProviderDigest: profile.ProviderDigest,
		Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	var captured []snapshot.File
	for path, content := range files {
		file, err := snapshot.NewFile(path, []byte(content), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &length})
		captured = append(captured, file)
		dest := filepath.Join(root, filepath.FromSlash(path))
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(dest, file.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	frozen, err := snapshot.Freeze(manifest, captured, 65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(frozen.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "capture.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	deadline, _ := ctx.Deadline()
	return ports.ProviderJob{Start: model.ProviderStart{ProviderProtocol: 1, Type: "analyze_start",
		JobID: "22222222-2222-4222-8222-222222222222", ProviderID: ProviderID, ProviderDigest: profile.ProviderDigest,
		ProfileID: profileID, GenerationID: "33333333-3333-4333-8333-333333333333", SnapshotRoot: root,
		ManifestPath: path, ManifestDigest: sha(raw), Profile: profile, MaxFrameBytes: 65536,
		DeadlineAt: deadline.UTC().Format(time.RFC3339Nano)}, Capture: providers.SnapshotInput(frozen),
		Recheck: func(ctx context.Context) error { return ctx.Err() }}, frozen
}

func runPython(t *testing.T, ctx context.Context, worker *providers.PythonProvider, job ports.ProviderJob) ([]model.ProviderBatch, model.ProviderDone) {
	t.Helper()
	var batches []model.ProviderBatch
	done, err := worker.Analyze(ctx, job, func(_ context.Context, batch model.ProviderBatch) error {
		batches = append(batches, batch)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return batches, done
}

func requireGaps(t *testing.T, done model.ProviderDone, want ...string) {
	t.Helper()
	gaps := map[string]bool{}
	for _, gap := range done.Gaps {
		gaps[gap.Code] = true
	}
	for _, gap := range want {
		if !gaps[gap] {
			t.Fatalf("missing boundary gap %q", gap)
		}
	}
}

func requireInertCapture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || string(got) != content {
			t.Fatalf("captured source changed %q: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "did-run")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup canary: %v", err)
	}
}

func sha(raw []byte) string {
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
