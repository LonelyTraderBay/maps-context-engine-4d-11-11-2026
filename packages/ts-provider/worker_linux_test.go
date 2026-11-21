//go:build linux

package tsprovider

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

const (
	compilerPin = "10e108c9cf7d5f2879053dff18515fb405abf2ccef63eaaf017d9c571687a1d3"
	nodePin     = "a7ec1b37e6ff9d187e4198c5b6df9a4eb1c74dd31804fb3bac55cc4ceca1ac01"
	manifestPin = "8fb0025c6ad53e08e02d1f50baf2c2bbf4c9a1833a9cd8b3ac3b26d6e1569707"
)

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func fixture(t *testing.T, mode string, files map[string]string) (providers.NodeConfig, ports.ProviderJob, *snapshot.Snapshot) {
	return workerFixture(t, mode, files, nil)
}

func workerFixture(t *testing.T, mode string, files map[string]string, artifact []byte) (providers.NodeConfig, ports.ProviderJob, *snapshot.Snapshot) {
	t.Helper()
	base := os.Getenv("MCE4D_T044_COMPILER")
	node := os.Getenv("MCE4D_T055_NODE")
	if base == "" || node == "" {
		t.Skip("requires the locally pinned T043 compiler and Node")
	}
	entrypoint, err := filepath.Abs("worker.cjs")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := os.ReadFile(entrypoint)
	if err != nil {
		t.Fatal(err)
	}
	if artifact != nil {
		worker = artifact
		entrypoint = ""
	}
	providerDigest := hashBytes(worker)
	scratch := t.TempDir()
	root := filepath.Join(scratch, "job")
	profile := &model.TSProfile{SchemaVersion: 1, Language: "typescript_javascript", TsconfigPaths: []string{"app/tsconfig.json"},
		CompilerDigest: compilerPin, ProviderDigest: providerDigest, NodeDigest: nodePin, AnalysisMode: mode}
	profileID, err := model.ProfileDigest(profile, 32768)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{
		SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: profileID,
		PolicyDigest: strings.Repeat("a", 64), ProviderDigest: providerDigest, DependencyInputDigests: []string{},
	}
	var source []snapshot.File
	for name, content := range files {
		data := []byte(content)
		file, err := snapshot.NewFile(name, data, 65536)
		if err != nil {
			t.Fatal(err)
		}
		source = append(source, file)
		size, hash := int64(file.Len()), file.ContentHash()
		manifest.Files = append(manifest.Files, model.ManifestFile{
			Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size,
		})
		name = filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	captured, err := snapshot.Freeze(manifest, source, 65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(captured.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	config := providers.NodeConfig{NodePath: node, Entrypoint: entrypoint, ScratchParent: scratch,
		NodeDigest: nodePin, CompilerDigest: compilerPin, ProviderID: "mce4d-ts", ProviderDigest: providerDigest,
		Compiler: providers.CompilerInstallation{Root: filepath.Join(base, "compiler"), ArchivePath: filepath.Join(base, "typescript-5.9.3.tgz"),
			ManifestPath: filepath.Join(base, "compiler-files.json"), ManifestDigest: manifestPin},
	}
	job := ports.ProviderJob{Start: model.ProviderStart{
		ProviderProtocol: 1, Type: "analyze_start", JobID: "22222222-2222-4222-8222-222222222222",
		GenerationID: "33333333-3333-4333-8333-333333333333", ProviderID: config.ProviderID,
		ProviderDigest: config.ProviderDigest, Profile: profile, ProfileID: profileID, SnapshotRoot: root,
		ManifestPath: manifestPath, ManifestDigest: hashBytes(body), MaxFrameBytes: 65536,
	}, Capture: providers.SnapshotInput(captured), Recheck: func(ctx context.Context) error { return ctx.Err() }}
	return config, job, captured
}

func analyze(t *testing.T, config providers.NodeConfig, job ports.ProviderJob) ([]model.ProviderBatch, model.ProviderDone) {
	t.Helper()
	job.Start.DeadlineAt = time.Now().Add(15 * time.Second).UTC().Format(time.RFC3339Nano)
	var batches []model.ProviderBatch
	done, err := providers.NewNodeProvider(config).Analyze(context.Background(), job, func(_ context.Context, batch model.ProviderBatch) error {
		batches = append(batches, batch)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return batches, done
}

func TestTypedBindings(t *testing.T) {
	files := map[string]string{
		"common.json":            `{"compilerOptions":{"strict":true,"target":"ES2022","module":"ESNext","moduleResolution":"Bundler","allowJs":true,"checkJs":true,"jsx":"preserve","lib":["es2022"],"types":[]}}`,
		"app/tsconfig.json":      `{"extends":"../common.json","compilerOptions":{"paths":{"@app/*":["./src/*"],"@lib/api":["../lib/dist/api.d.ts"]}},"include":["src/**/*"],"references":[{"path":"../lib"}]}`,
		"lib/tsconfig.json":      `{"extends":"../common.json","compilerOptions":{"composite":true,"rootDir":"src","outDir":"dist"},"files":["src/api.ts"]}`,
		"lib/src/api.ts":         "export function identity(value: number): number { return value; }",
		"lib/dist/api.d.ts":      "export declare function identity(value: number): number;",
		"app/src/local.ts":       "export function total(): number { return 1; }",
		"app/src/other.ts":       "export function total(): string { return 'other'; }",
		"app/src/barrel.ts":      "export { total as summarize } from './local.js';",
		"app/src/view.tsx":       "export const view = <badge/>;",
		"app/src/jsx-types.d.ts": "declare namespace JSX { interface IntrinsicElements { badge: {} } }",
		"app/src/legacy.jsx":     "export const legacy = <badge/>;",
		"app/package.json":       `{"type":"module","scripts":{"prepare":"node ./canary.cjs"}}`,
		"app/canary.cjs":         "require('node:fs').writeFileSync('../did-run', 'fixture');",
		"app/src/app.ts": strings.Join([]string{
			"import { summarize } from '@app/barrel';", "import { total as other } from './other.js';", "import { identity } from '@lib/api';",
			"export const glyph = '🌱'; export const first = summarize();",
			"export const second = other();", "export const third = identity(first);",
			"export function nested() { { let local = 1; } { let local = 2; } }",
		}, "\r\n"),
	}
	config, job, _ := fixture(t, "typed_when_available", files)
	batches, done := analyze(t, config, job)
	if done.Status != "complete_in_supported_scope" || len(done.Capabilities) != 1 || done.Capabilities[0].Status != "supported_scope_complete" {
		for _, batch := range batches {
			for _, item := range batch.Diagnostics {
				t.Logf("compiler diagnostic %s", item.Code)
			}
		}
		t.Fatalf("typed coverage: %+v", done)
	}
	expected := map[string]string{"summarize": "app/src/local.ts", "other": "app/src/other.ts", "identity": "lib/dist/api.d.ts"}
	symbols := map[string]model.SymbolFact{}
	scopes := map[string]bool{}
	for _, batch := range batches {
		for _, symbol := range batch.Symbols {
			symbols[symbol.SymbolID] = symbol
			if symbol.Name == "local" {
				scopes[symbol.ScopeKey] = true
			}
		}
	}
	for _, batch := range batches {
		for _, rel := range batch.Relations {
			if rel.Kind != "CALLS_DIRECT" || rel.SitePath != "app/src/app.ts" {
				continue
			}
			name := files[rel.SitePath][rel.Range.StartByte:rel.Range.EndByte]
			symbol := symbols[*rel.TargetSymbolID]
			if symbol.Path == nil || *symbol.Path != expected[name] {
				t.Fatalf("typed binding %q ended at %+v", name, symbol)
			}
			delete(expected, name)
		}
	}
	if len(expected) != 0 || len(scopes) != 2 {
		t.Fatalf("incomplete aliases, project references, or scopes: %v %v", expected, scopes)
	}
	again, repeated := analyze(t, config, job)
	if !reflect.DeepEqual(again, batches) || !reflect.DeepEqual(repeated, done) {
		t.Fatal("repeated analysis changed facts or coverage")
	}
	if _, err := os.Stat(filepath.Join(job.Start.SnapshotRoot, "did-run")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("target prepare canary ran")
	}
}

func TestTypedCoverageAndAdmission(t *testing.T) {
	for _, mode := range []string{"typed_when_available", "syntax", "plugin", "wrong-compiler"} {
		t.Run(mode, func(t *testing.T) {
			files := map[string]string{
				"app/tsconfig.json": `{"compilerOptions":{"allowJs":true,"checkJs":true,"lib":["es2022"]},"files":["entry.js"]}`,
				"app/entry.js":      "/** @type {*} */ const x = {}; x.invoke();\n/** @param {string} id */\nfunction load(id) { return import(id); }\n",
			}
			profileMode := mode
			if mode == "plugin" || mode == "wrong-compiler" {
				profileMode = "typed_when_available"
			}
			if mode == "plugin" {
				files["app/tsconfig.json"] = `{"extends":"./base.json","files":["entry.js"]}`
				files["app/base.json"] = `{"compilerOptions":{"plugins":[{"name":"./canary.cjs"}]}}`
				files["app/canary.cjs"] = "throw new Error('fixture plugin must not load');"
			}
			config, job, _ := fixture(t, profileMode, files)
			if mode == "wrong-compiler" {
				config.Compiler.ManifestDigest = strings.Repeat("b", 64)
				job.Start.DeadlineAt = time.Now().Add(15 * time.Second).UTC().Format(time.RFC3339Nano)
				_, err := providers.NewNodeProvider(config).Analyze(context.Background(), job, func(context.Context, model.ProviderBatch) error {
					t.Fatal("unverified compiler staged output")
					return nil
				})
				if !errors.Is(err, providers.ErrAuthority) {
					t.Fatalf("expected compiler admission denial: %v", err)
				}
				return
			}
			batches, done := analyze(t, config, job)
			if done.Status != "partial" {
				t.Fatalf("expected partial coverage: %+v", done)
			}
			gaps := map[string]bool{}
			for _, gap := range done.Gaps {
				gaps[gap.Code] = true
			}
			switch mode {
			case "plugin":
				if len(batches) != 0 || !gaps["TS_CONFIG_PLUGINS_UNSUPPORTED"] {
					t.Fatalf("unsupported plugin: %+v", done)
				}
			case "syntax":
				if len(batches) == 0 || !gaps["TS_SYNTAX_ONLY"] {
					t.Fatalf("syntax fallback: %+v", done)
				}
				for _, b := range batches {
					for _, r := range b.Relations {
						if r.EvidenceKind != "syntax_exact" {
							t.Fatal("syntax profile emitted typed facts")
						}
					}
				}
			default:
				if !gaps["TS_DYNAMIC_IMPORT"] || !gaps["TS_UNRESOLVED_CALL"] {
					t.Fatalf("dynamic coverage: %+v", done)
				}
				for _, b := range batches {
					for _, r := range b.Relations {
						if r.Kind == "CALLS_DIRECT" {
							t.Fatal("JS any became a direct call")
						}
					}
				}
			}
		})
	}
}
