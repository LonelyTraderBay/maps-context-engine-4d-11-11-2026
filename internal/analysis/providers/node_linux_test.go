//go:build linux

package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const nodePin = "a7ec1b37e6ff9d187e4198c5b6df9a4eb1c74dd31804fb3bac55cc4ceca1ac01"
const compilerPin = "10e108c9cf7d5f2879053dff18515fb405abf2ccef63eaaf017d9c571687a1d3"
const jobID = "01234567-89ab-cdef-0123-456789abcdef"
const generationID = "10234567-89ab-cdef-0123-456789abcdef"
const worktreeID = "20234567-89ab-cdef-0123-456789abcdef"

// This transport fixture is installation-owned. Captured source is only read
// as bytes. T044 will supply compiler facts using the same process contract.
const worker = `
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createHash } = require('node:crypto');
assert.equal(process.version, 'v24.22.3');
assert.equal(process.permission.has('child'), false);
assert.equal(process.env.NODE_OPTIONS, undefined);
function receive(n) {
  const r = Buffer.alloc(n);
  for (let i = 0; i < n;) {
    const got = fs.readSync(0, r, i, n - i);
    if (got === 0) throw Error('unexpected EOF');
    i += got;
  }
  return r;
}
let header = '';
while (!header.endsWith('\r\n\r\n') && header.length < 128) header += receive(1).toString();
const length = Number(/^Content-Length: (\d+)\r\n\r\n$/.exec(header)[1]);
const req = JSON.parse(receive(length));
const manifestBytes = fs.readFileSync(req.manifest_path);
assert.equal(createHash('sha256').update(manifestBytes).digest('hex'), req.manifest_digest);
const manifest = JSON.parse(manifestBytes);
assert.equal(req.type, 'analyze_start');
assert.equal(req.profile.allow_project_plugins, false);
const source = path.join(req.snapshot_root, 'fixture.ts');
assert.equal(process.permission.has('fs.read', source), true);
assert.equal(process.permission.has('fs.write', source), false);
assert.equal(process.permission.has('fs.read', path.resolve(req.snapshot_root, '../canary.cjs')), false);
assert.equal(createHash('sha256').update(fs.readFileSync(source)).digest('hex'), manifest.files[0].content_hash);
function send(part) {
  const bytes = Buffer.from(JSON.stringify({ provider_protocol: 1, job_id: req.job_id, ...part }));
  process.stdout.write('Content-Length: ' + bytes.length + '\r\n\r\n');
  process.stdout.write(bytes);
}
const symbol = { symbol_id:'fixture-symbol', path:'fixture.ts', package_key:null, kind:'variable', name:'label',
                 qualified_name:'label', scope_key:'fixture', range:{start_byte:6,end_byte:11},signature:'',exported:false };
if (mode === 'wrong-source') symbol.path = 'unavailable.ts';
if (mode === 'wrong-range') symbol.range = {start_byte:16,end_byte:17};
if (mode === 'oversize') {
  process.stdout.write('Content-Length: 65537\r\n\r\n');
} else if (mode === 'stderr') {
  process.stderr.write('x'.repeat((1 << 20) + 1));
  setInterval(() => {}, 1000);
} else {
  send({ type:'analyze_batch', sequence:1, packages:[], symbols:[symbol], relations:[], diagnostics:[] });
  if (mode !== 'missing-done' && mode !== 'stall') {
    const done = {type:'analyze_done',sequence:2,status:'complete_in_supported_scope',
                  input_digest:manifest.snapshot_digest,capabilities:[],gaps:[],error:null};
    if (mode === 'wrong-digest') done.input_digest = 'a'.repeat(64);
    send(done);
    if (mode === 'extra') send(done);
    if (mode === 'nonzero') process.exitCode = 3;
  }
  if (mode === 'stall') setInterval(() => {}, 1000);
}
`

func TestNodeProviderProcess(t *testing.T) {
	node := os.Getenv("MCE4D_T055_NODE")
	if node == "" {
		t.Skip("requires the pinned T043 Node executable")
	}
	t.Setenv("NODE_OPTIONS", "--not-a-real-flag")
	for _, test := range []struct {
		name   string
		err    error
		staged int
	}{
		{"ok", nil, 1},
		{"missing-done", ErrProtocol, 1},
		{"wrong-digest", ErrProtocol, 1},
		{"oversize", ErrProtocol, 0},
		{"extra", ErrProtocol, 1},
		{"wrong-source", ErrAuthority, 0},
		{"wrong-range", ErrAuthority, 0},
		{"nonzero", ErrProcess, 1},
		{"stderr", ErrBudget, 0},
		{"stall", context.DeadlineExceeded, 1},
		{"cancel", context.Canceled, 1},
		{"late-mutation", ErrAuthority, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			mode := test.name
			if mode == "cancel" {
				mode = "stall"
			}
			config, job := providerFixture(t, node, mode)
			got := 0
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			if test.name == "stall" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			}
			defer cancel()
			done, err := NewNodeProvider(config).Analyze(ctx, job, func(_ context.Context, batch model.ProviderBatch) error {
				got++
				if len(batch.Symbols) != 1 || batch.Symbols[0].Name != "label" {
					t.Fatal("wrong batch")
				}
				if test.name == "cancel" {
					cancel()
				}
				if test.name == "late-mutation" {
					writeFixture(t, filepath.Join(job.Start.SnapshotRoot, "fixture.ts"), []byte("const label = 'changed';"), 0600)
				}
				return nil
			})
			if !errors.Is(err, test.err) || got != test.staged {
				t.Fatalf("error %v, staged %d, expected %v and %d", err, got, test.err, test.staged)
			}
			if test.err == nil && done.Status != "complete_in_supported_scope" ||
				test.err != nil && done.Status != "" {
				t.Fatal("unexpected terminal result")
			}
		})
	}
}

func TestNodeProviderAdmission(t *testing.T) {
	node := os.Getenv("MCE4D_T055_NODE")
	if node == "" {
		t.Skip("requires the pinned T043 Node executable")
	}
	for _, mode := range []string{"capture", "manifest", "unexpected", "profile", "node", "worker", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			config, job := providerFixture(t, node, "ok")
			switch mode {
			case "capture":
				writeFixture(t, filepath.Join(job.Start.SnapshotRoot, "fixture.ts"), []byte("const unrelated = 0;"), 0600)
			case "manifest":
				writeFixture(t, job.Start.ManifestPath, []byte("{}"), 0600)
			case "unexpected":
				writeFixture(t, filepath.Join(job.Start.SnapshotRoot, "unexpected.ts"), []byte("const unrelated = 0;"), 0600)
			case "profile":
				config.CompilerDigest = strings.Repeat("b", 64)
			case "node":
				config.NodeDigest = strings.Repeat("b", 64)
				ts := job.Start.Profile.(*model.TSProfile)
				ts.NodeDigest = config.NodeDigest
				job.Start.ProfileID, _ = model.ProfileDigest(ts, 32768)
			case "worker":
				writeFixture(t, config.Entrypoint, []byte("// different fixture"), 0600)
			case "revoke":
				job.Recheck = func(context.Context) error { return ErrAuthority }
			}
			job.Start.DeadlineAt = time.Now().Add(5 * time.Second).UTC().Format(time.RFC3339Nano)
			done, err := NewNodeProvider(config).Analyze(context.Background(), job, func(context.Context, model.ProviderBatch) error {
				t.Fatal("rejected input staged a batch")
				return nil
			})
			if !errors.Is(err, ErrAuthority) || done.Status != "" {
				t.Fatalf("expected authority denial, got %v", err)
			}
		})
	}
}

func providerFixture(t *testing.T, node, mode string) (NodeConfig, ports.ProviderJob) {
	t.Helper()
	base := t.TempDir()
	scratch := filepath.Join(base, "scratch")
	capture := filepath.Join(scratch, "job")
	if err := os.MkdirAll(capture, 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(scratch, "canary.cjs"), []byte("// unrelated filesystem bytes"), 0600)
	entry := filepath.Join(base, "worker.cjs")
	workerCode := []byte("const mode = " + strconv.Quote(mode) + ";\n" + worker)
	writeFixture(t, entry, workerCode, 0600)
	provider := hexDigest(workerCode)
	config := NodeConfig{NodePath: node, Entrypoint: entry, ScratchParent: scratch, NodeDigest: nodePin,
		CompilerDigest: compilerPin, ProviderDigest: provider, ProviderID: "ts-fixture"}
	profile := &model.TSProfile{SchemaVersion: 1, Language: "typescript_javascript", TsconfigPaths: []string{"tsconfig.json"},
		CompilerDigest: compilerPin, NodeDigest: nodePin, ProviderDigest: provider, AnalysisMode: "syntax"}
	profileID, err := model.ProfileDigest(profile, 32768)
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes := []byte("const label = '🌱';\n")
	source, err := snapshot.NewFile("fixture.ts", sourceBytes, 4096)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(capture, "fixture.ts"), sourceBytes, 0600)
	hash := source.ContentHash()
	size := int64(source.Len())
	configBytes := []byte("{\"files\":[\"fixture.ts\"]}\n")
	tsconfig, err := snapshot.NewFile("tsconfig.json", configBytes, 4096)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(capture, "tsconfig.json"), configBytes, 0600)
	configHash, configSize := tsconfig.ContentHash(), int64(tsconfig.Len())
	frozen, err := snapshot.Freeze(model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktreeID, ProfileID: profileID,
		PolicyDigest: strings.Repeat("a", 64), ProviderDigest: provider, DependencyInputDigests: []string{},
		Files: []model.ManifestFile{{Path: source.Path(), Status: "captured", ContentHash: &hash, ByteLength: &size},
			{Path: tsconfig.Path(), Status: "captured", ContentHash: &configHash, ByteLength: &configSize}}},
		[]snapshot.File{source, tsconfig}, 32768, 4096)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(frozen.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(capture, "manifest.json")
	writeFixture(t, manifestPath, manifest, 0600)
	start := model.ProviderStart{ProviderProtocol: 1, Type: "analyze_start", JobID: jobID, ProviderID: config.ProviderID,
		ProviderDigest: provider, ProfileID: profileID, GenerationID: generationID, Profile: profile,
		SnapshotRoot: capture, ManifestPath: manifestPath, ManifestDigest: hexDigest(manifest),
		MaxFrameBytes: 65536, DeadlineAt: time.Now().Add(8 * time.Second).UTC().Format(time.RFC3339Nano)}
	return config, ports.ProviderJob{Start: start, Capture: SnapshotInput(frozen), Recheck: func(ctx context.Context) error { return ctx.Err() }}
}

func writeFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
