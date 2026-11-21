//go:build linux

package flowview

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/sql"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const backend = `package fixture

import (
 "database/sql"
 "net/http"
)

var db *sql.DB
func init() { panic("fixture source is never executed") }
func Routes(m *http.ServeMux, dynamic string) {
 m.HandleFunc("POST /stock/{id}", handler)
 http.HandleFunc("GET /availability", handler)
 m.HandleFunc(dynamic, handler)
 var shadow localMux
 shadow.HandleFunc("DELETE /misleading", handler)
}

type localMux struct{}
func (localMux) HandleFunc(string, http.HandlerFunc) {}

func handler(http.ResponseWriter, *http.Request) {
 if rejected() {
  return
 }
 go signal()
 callA(callB())
 persist()
}

func rejected() bool { return false }
func signal() {}
func callA(int) {}
func callB() int { return 0 }

func persist() {
 tx, err := db.Begin()
 if err != nil { return }
 tx.Exec("INSERT INTO stock (quantity) VALUES (?)", 3)
 tx.Exec("UPDATE stock SET quantity = ?", 7)
 tx.Rollback()
 q := "DELETE FROM stock"
 db.Exec(q)
}

func deferredTx() {
 tx, err := db.Begin()
 if err != nil { return }
 defer tx.Rollback()
 tx.Commit()
}
func aliasTx() {
 tx, err := db.Begin()
 if err != nil { return }
 alias := tx
 alias.Rollback()
}
func retryBoundary() {
 for i := 0; i < 3; i++ { persist() }
}
func publishOrder() { sendToTransport("stock-changed") }
func consumeOrder() { receiveFromTransport() }
func sendToTransport(string) {}
func receiveFromTransport() {}
`

func TestT095GuardedGoStaticFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	dir := t.TempDir()
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "backend.go": backend,
		"schema.sql":  "CREATE TABLE stock (quantity INT NOT NULL) ENGINE=InnoDB;",
		"events.json": eventFixture,
	}
	in, member, schema := flowFixtureInput(t, dir, files)
	goRoot := runtime.GOROOT()
	goPath := filepath.Join(goRoot, "bin", "go")
	t.Setenv("PATH", filepath.Dir(goPath))
	binRoot, err := paths.OpenRoot(filepath.Dir(goPath))
	if err != nil {
		t.Fatal(err)
	}
	defer binRoot.Close()
	goBytes, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := runtimeadapter.OpenGoExecutable(ctx, runtimeadapter.GoExecutableInput{BinRoot: binRoot,
		ExpectedSHA256: fmt.Sprintf("%x", sha256.Sum256(goBytes)), MaxBytes: 128 << 20,
		Authorize: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	in.VerifyToolchain = golang.LoaderToolchainVerifier(tool, goRoot,
		func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
	workspace := filepath.Join(dir, "input")
	env, _, err := golang.LoaderEnvironment(in.Profile, goRoot, dir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	stdList := exec.CommandContext(ctx, goPath, "list", "-deps", "-f", "{{if .Standard}}{{.ImportPath}}{{end}}", "net/http", "database/sql")
	stdList.Dir, stdList.Env = dir, env
	raw, err := stdList.Output()
	if err != nil {
		t.Fatalf("inventory standard libraries: %v", err)
	}
	root, err := paths.OpenRoot(filepath.Join(goRoot, "src"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	stdScanner, err := policy.NewScanner(2 << 20)
	if err != nil {
		t.Fatal(err)
	}
	stdlib, err := golang.NewTrustedStdlib(ctx, golang.TrustedStdlibInput{SourceRoot: root, GoRoot: goRoot,
		Profile: in.Profile, Snapshot: in.Snapshot, DependencyDigest: in.Snapshot.Manifest().DependencyInputDigests[0],
		AllowedPackages: strings.Fields(string(raw)), Scanner: stdScanner, MaxManifestBytes: 32768,
		MaxFileBytes: 2 << 20, MaxFiles: 8192, MaxSourceBytes: 64 << 20, AuthorizePath: func(context.Context, string) error { return nil },
		VerifyAuthority: func(context.Context, string, string, string, []string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer scratch.Close()
	load := golang.CapturedLoaderInput{Preflight: in, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: workspace,
		Tool: tool, Stdlib: stdlib, MaxOutputBytes: 65536, VerifyScratch: golang.LoaderScratchVerifier(scratch,
			func(context.Context) error { return nil })}
	program, err := Load(ctx, Input{Graph: golang.GraphInput{Typed: golang.TypedInput{Loader: load, PackageKey: "fixture-package", ModuleKey: "fixture-module",
		MaxNodes: 20000, MaxDepth: 256, MaxFacts: 4000, MaxOccurrences: 4000, MaxOutputBytes: 2 << 20},
		MaxRelations: 5000, MaxOutputBytes: 2 << 20}, Member: member, ServeMuxModern: true})
	if err != nil {
		t.Fatal(err)
	}
	checkFlowFixture(t, ctx, program, schema)
	checkTransactionFixture(t, ctx, program)
	checkEventFixture(t, ctx, program)
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil || string(got) != want {
			t.Fatalf("fixture source modified: %s", name)
		}
	}
}

func flowFixtureInput(t *testing.T, dir string, sources map[string]string) (golang.LoaderPreflightInput, model.ScopeMember, model.SourceRef) {
	t.Helper()
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{},
		ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64),
		ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 32768)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: profileID,
		PolicyDigest: strings.Repeat("c", 64), ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{strings.Repeat("d", 64)}}
	var sourceFiles []snapshot.File
	fileMap := inventory.FileMap{Entries: []inventory.FileMapEntry{}}
	var schema model.SourceRef
	const generation = "22222222-2222-4222-8222-222222222222"
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := []byte(sources[name])
		if err := os.WriteFile(filepath.Join(dir, "input", name), data, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := snapshot.NewFile(name, data, 32768)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &length})
		sourceFiles = append(sourceFiles, file)
		fileMap.Entries = append(fileMap.Entries, inventory.FileMapEntry{Path: name, Origin: "untracked", State: "candidate", Stages: []uint8{}})
		if name == "schema.sql" {
			schema = model.SourceRef{Path: name, WorktreeID: manifest.WorktreeID, ProfileID: profileID, GenerationID: generation,
				ContentHash: hash, Range: model.ByteRange{EndByte: length}}
		}
	}
	capture, err := snapshot.Freeze(manifest, sourceFiles, 32768, 65536)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	verify := func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	pre := golang.LoaderPreflightInput{Snapshot: capture, Profile: profile, Inventory: fileMap, PolicyEpoch: 1, Scanner: scanner,
		AuthorizeScope: authorize, AuthorizePath: authorize, VerifyClosure: verify, MaxManifestBytes: 32768, MaxFileBytes: 65536, MaxSourceBytes: 65536,
		VerifyDependencies: func(_ context.Context, _ *model.GoProfile, digests []string) (bool, error) {
			return slices.Equal(digests, manifest.DependencyInputDigests), nil
		}}
	return pre, model.ScopeMember{WorktreeID: manifest.WorktreeID, ProfileID: profileID, GenerationID: generation,
		SnapshotDigest: capture.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1}, schema
}

func checkFlowFixture(t *testing.T, ctx context.Context, program *Program, schema model.SourceRef) {
	t.Helper()
	routes := program.Routes()
	if len(routes) != 2 || routes[0].Pattern.Method != "POST" || routes[0].Pattern.Path != "/stock/{id}" ||
		routes[1].Pattern.Method != "GET" || len(program.Gaps()) != 1 {
		t.Fatalf("route scope or binding: %+v, gaps=%+v", routes, program.Gaps())
	}
	root := routes[0].Handler
	choice, err := program.Project(ctx, root, Options{Steps: 80, Edges: 240, CallDepth: 1})
	if err != nil || len(choice.Routes) != 2 || choice.Data.Steps[0].Limitations[0].Code != "FLOW_HTTP_ROUTE_CHOICE_REQUIRED" {
		t.Fatalf("route chooser: %+v %v", choice, err)
	}
	opt := Options{Steps: 80, Edges: 240, CallDepth: 1, Registration: &routes[0].Site,
		SQL: &SQLInput{Profile: sql.Profile{Dialect: "mysql", Version: "8.4", Engine: "innodb", SQLMode: "default",
			Environment: "fixture", Namespace: "stock", Schema: "inventory"}, Schemas: []model.SourceRef{schema}}}
	got, err := program.Project(ctx, root, opt)
	if err != nil || len(got.Data.Steps) == 0 || got.Data.Steps[0].StepKind != "http_route" {
		t.Fatalf("static flow: %v", err)
	}
	if ref := got.Data.Steps[0].SourceRefs[0]; ref != routes[0].Site ||
		string(backend[ref.Range.StartByte:ref.Range.EndByte]) != `m.HandleFunc("POST /stock/{id}", handler)` {
		t.Fatalf("reverse source site lost: %+v", ref)
	}
	kinds := make(map[string]bool)
	gaps := make(map[string]bool)
	async := ""
	for _, step := range got.Data.Steps {
		kinds[step.StepKind] = true
		for _, gap := range step.Limitations {
			gaps[gap.Code] = true
		}
		if step.StepKind == "async_boundary" {
			async = step.StepID
		}
		if step.PossibleOrObserved != "statically_possible" || strings.HasSuffix(step.OutcomeStatus, "_observed") {
			t.Fatalf("invented runtime evidence: %+v", step)
		}
		if step.StepKind == "direct_call" {
			ref := step.SourceRefs[0]
			if strings.HasPrefix(backend[ref.Range.StartByte:ref.Range.EndByte], "call") && step.OrderSemantics != "call_relationship_only" {
				t.Fatalf("nested call assigned sequence: %+v", step)
			}
		}
	}
	for _, kind := range []string{"branch", "branch_then", "return", "async_boundary", "direct_call", "sql_begin", "sql_insert", "sql_update", "sql_rollback"} {
		if !kinds[kind] {
			t.Errorf("missing %s in %+v", kind, kinds)
		}
	}
	if !gaps["FLOW_SQL_DYNAMIC"] {
		t.Fatalf("dynamic SQL lost: %+v", gaps)
	}
	for _, edge := range got.Data.Edges {
		if edge.FromStepID == async {
			t.Fatalf("async tagged as serial predecessor: %+v", edge)
		}
	}
	var ops []string
	group := ""
	for _, site := range got.SQL {
		ops = append(ops, site.Operation)
		if site.OutcomeStatus != "outcome_unknown" {
			t.Fatalf("static call reported persisted: %+v", site)
		}
		if group == "" {
			group = site.TransactionGroupID
		}
		if group == "" || site.TransactionGroupID != group {
			t.Fatalf("typed transaction receiver lost: %+v", site)
		}
		if site.Operation == "INSERT" || site.Operation == "UPDATE" {
			if len(site.Targets) != 1 || site.Targets[0].Key.Identifier != "table:`inventory`.`stock`" ||
				site.Targets[0].Declaration.ContentHash != schema.ContentHash {
				t.Fatalf("wrong SQL target: %+v", site)
			}
		}
	}
	if !slices.Equal(ops, []string{"BEGIN", "INSERT", "UPDATE", "ROLLBACK"}) {
		t.Fatalf("unexpected SQL projection: %v", ops)
	}
	other, err := program.Project(ctx, root, opt)
	if err != nil || other.Data.ProjectionID != got.Data.ProjectionID {
		t.Fatalf("repeat projection changed ID: %v", err)
	}
	bounded, err := program.Project(ctx, root, Options{Steps: 1, Edges: 1, Registration: &routes[0].Site})
	if err != nil || !bounded.Partial || len(bounded.Data.Steps) != 1 || len(bounded.Data.Edges) != 0 {
		t.Fatalf("bounded projection: %+v %v", bounded, err)
	}
	deny := errors.New("fixture source no longer authorized")
	saved := program.authorizePath
	program.authorizePath = func(_ context.Context, _, path, _ string, _ int64) error {
		if path == schema.Path {
			return deny
		}
		return nil
	}
	if data, err := program.Project(ctx, root, opt); err != deny || len(data.Data.Steps) != 0 {
		t.Fatalf("revoked schema projection: %v", err)
	}
	program.authorizePath = saved
	root.GenerationID = "33333333-3333-4333-8333-333333333333"
	if _, err := program.Project(ctx, root, opt); err != ErrInput {
		t.Fatalf("stale root accepted: %v", err)
	}
}
