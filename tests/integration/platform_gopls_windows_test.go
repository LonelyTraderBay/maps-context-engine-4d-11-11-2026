package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func approvedWindowsFixtureFile(t *testing.T, path, expected string, limit int64) []byte {
	t.Helper()
	if !filepath.IsAbs(path) || len(expected) != 64 {
		t.Fatal("fixture material requires an absolute path and an exact SHA-256")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		t.Fatalf("invalid fixture material size: %v", err)
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != expected {
		t.Fatal("fixture material SHA-256 does not match")
	}
	return data
}

func windowsFixtureURI(path string) string {
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path)}).String()
}

func TestWindowsAppContainerGopls(t *testing.T) {
	runWindowsGoplsFixture(t, true, false)
}

func TestWindowsJobGopls(t *testing.T) {
	runWindowsGoplsFixture(t, false, false)
}

func TestWindowsManagedGoplsSession(t *testing.T) {
	runWindowsGoplsFixture(t, true, true)
}

func runWindowsGoplsFixture(t *testing.T, isolated, managed bool) {
	t.Helper()
	binary := os.Getenv("MCE4D_GOPLS_WIN_FIXTURE")
	if binary == "" {
		t.Skip("requires the pinned Windows gopls and Go fixture materials")
	}
	goroot := os.Getenv("MCE4D_GOPLS_WIN_GOROOT")
	data := approvedWindowsFixtureFile(t, binary, os.Getenv("MCE4D_GOPLS_WIN_SHA256"), 96<<20)
	approvedWindowsFixtureFile(t, filepath.Join(goroot, "bin", "go.exe"), os.Getenv("MCE4D_GO_WIN_SHA256"), 96<<20)
	var role appContainer
	base := t.TempDir()
	readGrants, writeGrants := "", ""
	if isolated {
		role = newAppContainer(t, "Gopls")
		readGrants, writeGrants = readableBy(role), writableBy(role)
		base = fixtureDirectory(t, filepath.Join(role.localFolder(t), "mce4d"), readGrants)
	}
	materials := fixtureDirectory(t, filepath.Join(base, "bin"), readGrants)
	sourceDir := "source"
	if managed {
		sourceDir = "input"
	}
	worktree := fixtureDirectory(t, filepath.Join(base, sourceDir), readGrants)
	scratch := fixtureDirectory(t, filepath.Join(base, "scratch"), writeGrants)
	if isolated {
		markFixtureLowIntegrity(t, scratch)
	}
	server := filepath.Join(materials, "gopls.exe")
	if err := os.WriteFile(server, data, 0700); err != nil {
		t.Fatal(err)
	}
	source := "package fixture\r\nfunc Original() string { return \"Tiếng Việt 😀\" }\r\nfunc Caller() string { return Original() }\r\n"
	if managed {
		source = "package fixture\r\nimport \"strings\"\r\nfunc Original() string { return strings.TrimSpace(\"Tiếng Việt 😀\") }\r\nfunc Caller() string { return Original() }\r\n"
	}
	inputs := map[string]string{"go.mod": "module example.invalid/fixture\n\ngo 1.27.4\n", "fixture.go": source}
	for name, data := range inputs {
		file := filepath.Join(worktree, name)
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if managed {
			protectFixturePath(t, file, readGrants)
		}
	}
	if managed {
		verifyWindowsManagedGopls(t, role, base, materials, worktree, goroot, inputs)
		return
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	env := []string{"GOROOT=" + goroot, "PATH=" + filepath.Join(goroot, "bin") + ";" + filepath.Join(os.Getenv("SystemRoot"), "System32"),
		"PATHEXT=.EXE", "GOCACHE=" + filepath.Join(scratch, "cache"), "GOPATH=" + filepath.Join(scratch, "go"),
		"APPDATA=" + scratch, "GOTMPDIR=" + scratch,
		"GOENV=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOPACKAGESDRIVER=off", "GOTELEMETRY=off", "CGO_ENABLED=0"}
	probe := startWindowsFixtureJob(t, role, filepath.Join(goroot, "bin", "go.exe"), worktree,
		[]string{"env", "-json", "GOOS", "GOARCH", "GOVERSION", "GOROOT"}, env, nil)
	exit, err := probe.wait(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	probeLog, err := io.ReadAll(io.LimitReader(probe.output, 4096))
	var metadata struct{ GOOS, GOARCH, GOVERSION, GOROOT string }
	if err != nil || exit != 0 || json.Unmarshal(probeLog, &metadata) != nil ||
		metadata.GOOS != "windows" || metadata.GOARCH != "amd64" || metadata.GOVERSION != "go1.27.4" || metadata.GOROOT != goroot {
		t.Fatalf("native toolchain could not run in the fixture container: exit=%d output=%q error=%v", exit, probeLog, err)
	}
	logPath := filepath.Join(scratch, "gopls.log")
	defer func() {
		if t.Failed() {
			if file, err := os.Open(logPath); err == nil {
				log, _ := io.ReadAll(io.LimitReader(file, 8192))
				file.Close()
				t.Logf("gopls synthetic fixture log: %s", log)
			}
		}
	}()
	child := startWindowsFixtureJob(t, role, server, worktree, []string{"-logfile", logPath, "serve"}, env, read)
	read.Close()
	messages := bufio.NewReader(io.LimitReader(child.output, 2<<20))
	send := func(id int, method string, params any) {
		t.Helper()
		request := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != 0 {
			request["id"] = id
		}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(write, "Content-Length: %d\r\n\r\n%s", len(raw), raw); err != nil {
			t.Fatal(err)
		}
	}
	request := func(id int, method string, params any) json.RawMessage {
		t.Helper()
		send(id, method, params)
		type response struct {
			ID     int
			Result json.RawMessage
			Error  json.RawMessage
			Notes  []json.RawMessage
		}
		results := make(chan response, 1)
		go func() {
			var notes []json.RawMessage
			for {
				raw, err := readWindowsFixtureLSP(messages)
				if err != nil {
					results <- response{Error: json.RawMessage(strconv.Quote(err.Error())), Notes: notes}
					return
				}
				var reply response
				if json.Unmarshal(raw, &reply) == nil && reply.ID == id {
					reply.Notes = notes
					results <- reply
					return
				}
				if len(notes) < 6 && len(raw) < 2048 && strings.Contains(string(raw), "window/") {
					notes = append(notes, raw)
				}
			}
		}()
		select {
		case reply := <-results:
			if len(reply.Error) != 0 || reply.Result == nil {
				t.Fatalf("gopls %s failed: %s; notifications: %s", method, reply.Error, reply.Notes)
			}
			return reply.Result
		case <-time.After(30 * time.Second):
			t.Fatalf("gopls %s timed out", method)
		}
		return nil
	}
	root := windowsFixtureURI(worktree)
	initialized := request(1, "initialize", map[string]any{"processId": os.Getpid(), "rootUri": root,
		"workspaceFolders":      []map[string]any{{"uri": root, "name": "fixture"}},
		"initializationOptions": map[string]any{"verboseOutput": true, "fileWatcher": "off", "vulncheck": "Off"},
		"capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}},
			"workspace":    map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}},
			"textDocument": map[string]any{"rename": map[string]any{"prepareSupport": true}}}})
	var serverVersion struct {
		ServerInfo struct{ Version string }
	}
	if err := json.Unmarshal(initialized, &serverVersion); err != nil || !strings.Contains(serverVersion.ServerInfo.Version, "v0.24.0") {
		t.Fatalf("unexpected native gopls version: %v", err)
	}
	send(0, "initialized", map[string]any{})
	uri := windowsFixtureURI(filepath.Join(worktree, "fixture.go"))
	send(0, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": source}})
	position := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]int{"line": 1, "character": 5}}
	if prepared := request(2, "textDocument/prepareRename", position); string(prepared) == "null" {
		t.Fatal("gopls did not prepare the fixture rename")
	}
	position["newName"] = "Revised"
	raw := request(3, "textDocument/rename", position)
	file, err := snapshot.NewFile("fixture.go", []byte(source), 1024)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	id := "33333333-3333-4333-8333-333333333333"
	ref := model.SourceRef{WorktreeID: id, ProfileID: strings.Repeat("a", 64), GenerationID: id, Path: "fixture.go", ContentHash: file.ContentHash()}
	edit, err := lsp.ValidateWorkspaceEdit(context.Background(), raw, lsp.WorkspaceEditInput{WorkspaceRoot: worktree,
		Documents: []lsp.CapturedDocument{{File: file, Ref: ref, Version: &version}}, Encoding: snapshot.UTF16, MaxBytes: 65536, MaxEdits: 8})
	if err != nil || len(edit.Edits) != 2 {
		t.Fatalf("captured Windows rename edit validation: %v", err)
	}
	for _, change := range edit.Edits {
		if source[change.SourceRef.Range.StartByte:change.SourceRef.Range.EndByte] != "Original" || change.NewText != "Revised" {
			t.Fatal("rename did not bind to expected captured bytes")
		}
	}
	request(4, "shutdown", nil)
	send(0, "exit", nil)
	if exit, err := child.wait(5 * time.Second); err != nil || exit != 0 {
		t.Fatalf("gopls did not shut down: exit=%d, %v", exit, err)
	}
	for name, expected := range inputs {
		if contents, err := os.ReadFile(filepath.Join(worktree, name)); err != nil || string(contents) != expected {
			t.Fatalf("gopls modified the read-only synthetic source: %v", err)
		}
	}
	t.Logf("pinned gopls v0.24.0: native Job initialize, prepare, two validated rename edits and clean shutdown; AppContainer=%v", isolated)
}

func verifyWindowsManagedGopls(t *testing.T, role appContainer, scratch, bin, workspace, goRoot string, inputs map[string]string) {
	t.Helper()
	proofCtx, proofCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer proofCancel()
	toolchain, err := runtime.InspectGoToolchain(proofCtx, goRoot)
	if err != nil {
		t.Fatalf("full Windows Go toolchain proof: %v", err)
	}
	defer toolchain.Close()
	if !model.ValidDigest(toolchain.Digest()) || !slices.Contains(toolchain.StandardPackages(), "fmt") {
		t.Fatal("verified toolchain lacks standard package coverage")
	}
	tool := verifyWindowsGoLookup(t, proofCtx, goRoot, toolchain)
	for _, name := range []string{"home", "gopath", "modcache", "cache", "tmp"} {
		dir := fixtureDirectory(t, filepath.Join(scratch, name), writableBy(role))
		markFixtureLowIntegrity(t, dir)
	}
	root, err := paths.OpenRoot(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	source, err := paths.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	captured, verifyCapture := verifyWindowsCapturedLoad(t, proofCtx, role, scratch, workspace, goRoot, inputs, toolchain, tool)
	file, _ := captured.File("fixture.go")
	const id = "11111111-1111-4111-8111-111111111111"
	version := int64(1)
	document := lsp.CapturedDocument{File: file, Ref: model.SourceRef{WorktreeID: id, ProfileID: captured.Manifest().ProfileID,
		GenerationID: id, Path: file.Path(), ContentHash: file.ContentHash()}, Version: &version}
	check := func(ctx context.Context) error {
		if err := errors.Join(ctx.Err(), source.Verify(), toolchain.CheckPath(ctx, "bin/go.exe"),
			toolchain.CheckPath(ctx, "pkg/tool/windows_amd64")); err != nil {
			return err
		}
		ok, err := verifyCapture(ctx, scratch, workspace, captured)
		if err != nil || !ok {
			return errors.Join(lsp.ErrLSPAuthority, err)
		}
		return nil
	}
	limiter, err := lsp.NewSessionLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config := lsp.SessionConfig{ServerBinRoot: root, ExpectedSHA256: os.Getenv("MCE4D_GOPLS_WIN_SHA256"), Check: check,
		Documents: []lsp.CapturedDocument{document}, Limiter: limiter, WindowsContainerSID: role.sid.String(),
		Environment: runtime.GoEnvironmentInput{GoRoot: goRoot, ScratchRoot: scratch, WorkspaceRoot: workspace,
			GOOS: "windows", GOARCH: "amd64", BuildTags: []string{}}}
	session, err := lsp.StartSession(ctx, config)
	if err != nil {
		t.Fatalf("managed Windows gopls startup: %v", err)
	}
	defer session.Close(ctx)
	selector := model.PositionSelector{Kind: "source_position", Path: file.Path(), ContentHash: file.ContentHash(),
		ByteOffset: int64(strings.Index(inputs[file.Path()], "Original"))}
	result, err := session.Preview(ctx, selector, "Renamed")
	if err != nil || len(result.Edits) != 2 || len(result.FilePreconditions) != 1 {
		t.Fatalf("managed Windows rename: edits=%d preconditions=%d err=%v", len(result.Edits), len(result.FilePreconditions), err)
	}
	for _, edit := range result.Edits {
		if edit.NewText != "Renamed" || edit.SourceRef.Path != file.Path() ||
			string(file.Bytes()[edit.SourceRef.Range.StartByte:edit.SourceRef.Range.EndByte]) != "Original" {
			t.Fatalf("managed Windows session returned an invalid edit")
		}
	}
	if err := errors.Join(session.Close(ctx), check(ctx)); err != nil {
		t.Fatal(err)
	}
	slowCheck := false
	config.Check = func(ctx context.Context) error {
		if slowCheck {
			<-ctx.Done()
			return ctx.Err()
		}
		return check(ctx)
	}
	session, err = lsp.StartSession(ctx, config)
	if err != nil {
		t.Fatalf("managed Windows gopls restart after rename: %v", err)
	}
	slowCheck = true
	request, requestCancel := context.WithTimeout(ctx, 15*time.Millisecond)
	result, err = session.Preview(request, selector, "Renamed")
	requestCancel()
	if !errors.Is(err, context.DeadlineExceeded) || len(result.Edits) != 0 {
		t.Fatalf("managed Windows rename timeout returned edits or the wrong error: %v", err)
	}
	config.Check = check
	session, err = lsp.StartSession(ctx, config)
	if err != nil {
		t.Fatalf("managed Windows gopls did not release its session slot after timeout: %v", err)
	}
	if err := errors.Join(session.Close(ctx), check(ctx)); err != nil {
		t.Fatal(err)
	}
	postCtx, postCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer postCancel()
	if err := toolchain.Check(postCtx); err != nil {
		t.Fatalf("Windows Go toolchain changed during gopls run: %v", err)
	}
	t.Logf("Windows Go toolchain proof digest %s", toolchain.Digest())
	t.Log("managed Windows gopls verified the pinned binary and AppContainer token, validated two CRLF rename edits, and drained its Job after preview and timeout")
}

func verifyWindowsGoLookup(t *testing.T, ctx context.Context, goRoot string, proof *runtime.GoToolchainProof) *runtime.GoExecutable {
	t.Helper()
	bin := filepath.Join(goRoot, "bin")
	root, err := paths.OpenRoot(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	tool, err := runtime.OpenGoExecutable(ctx, runtime.GoExecutableInput{
		BinRoot: root, ExpectedSHA256: os.Getenv("MCE4D_GO_WIN_SHA256"), MaxBytes: 96 << 20,
		Authorize: func(ctx context.Context) error { return proof.CheckPath(ctx, "bin/go.exe") },
	})
	if err != nil {
		t.Fatalf("Windows Go binary authority: %v", err)
	}
	t.Cleanup(func() { _ = tool.Close() })
	t.Setenv("PATH", bin)
	t.Setenv("PATHEXT", ".COM;.EXE")
	if err := tool.CheckForLoad(ctx, goRoot); !errors.Is(err, runtime.ErrGoToolchain) {
		t.Fatalf("unsafe Windows PATHEXT was accepted: %v", err)
	}
	t.Setenv("PATHEXT", ".EXE")
	if err := tool.CheckForLoad(ctx, goRoot); err != nil {
		t.Fatalf("verified Windows Go executable could not bind loader PATH: %v", err)
	}
	return tool
}

func verifyWindowsCapturedLoad(t *testing.T, ctx context.Context, role appContainer, scratch, workspace, goRoot string,
	inputs map[string]string, proof *runtime.GoToolchainProof, tool *runtime.GoExecutable,
) (*snapshot.Snapshot, func(context.Context, string, string, *snapshot.Snapshot) (bool, error)) {
	t.Helper()
	root, err := paths.OpenRoot(scratch)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	verify := golanganalysis.WindowsLoaderScratchVerifier(root, role.sid, func(ctx context.Context) error {
		return errors.Join(ctx.Err(), root.Verify(), proof.CheckPath(ctx, "bin/go.exe"))
	})
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "windows", Goarch: "amd64", BuildTags: []string{},
		ModuleRoots: []string{"."}, ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{},
		ToolchainDigest: proof.Digest(), ProviderDigest: os.Getenv("MCE4D_GOPLS_WIN_SHA256")}
	profileID, err := model.ProfileDigest(profile, 32768)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111",
		ProfileID: profileID, PolicyDigest: strings.Repeat("c", 64), ProviderDigest: profile.ProviderDigest,
		Files: []model.ManifestFile{}, DependencyInputDigests: []string{proof.Digest()}}
	files := make([]snapshot.File, 0, len(inputs))
	inv := inventory.FileMap{Entries: []inventory.FileMapEntry{}, InputDigest: proof.Digest()}
	for _, name := range []string{"fixture.go", "go.mod"} {
		file, err := snapshot.NewFile(name, []byte(inputs[name]), 64<<10)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
		hash, size := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size})
		inv.Entries = append(inv.Entries, inventory.FileMapEntry{Path: name, Origin: "untracked", State: "candidate", Stages: []uint8{}})
	}
	captured, err := snapshot.Freeze(manifest, files, 64<<10, 128<<10)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(2 << 20)
	if err != nil {
		t.Fatal(err)
	}
	authorizeScope := func(ctx context.Context, worktree, id, digest string, epoch int64) error {
		if worktree != manifest.WorktreeID || id != profileID || digest != manifest.PolicyDigest || epoch != 1 {
			return lsp.ErrLSPAuthority
		}
		return ctx.Err()
	}
	input := golanganalysis.LoaderPreflightInput{Snapshot: captured, Profile: profile, Inventory: inv, PolicyEpoch: 1, Scanner: scanner,
		AuthorizeScope: authorizeScope, AuthorizePath: func(ctx context.Context, worktree, name, digest string, epoch int64) error {
			if _, ok := inputs[name]; !ok {
				return lsp.ErrLSPAuthority
			}
			return authorizeScope(ctx, worktree, profileID, digest, epoch)
		},
		VerifyClosure: func(ctx context.Context, s *snapshot.Snapshot, _ inventory.FileMap) (bool, error) {
			return verify(ctx, scratch, workspace, s)
		},
		VerifyToolchain: golanganalysis.LoaderToolchainVerifier(tool, goRoot, func(ctx context.Context, p *model.GoProfile) (bool, error) {
			err := proof.CheckPath(ctx, "pkg/tool/windows_amd64")
			return err == nil && p.ToolchainDigest == proof.Digest(), err
		}),
		VerifyDependencies: func(ctx context.Context, p *model.GoProfile, digests []string) (bool, error) {
			err := proof.CheckPath(ctx, "src")
			return err == nil && len(p.DependencyRoots) == 0 && slices.Equal(digests, manifest.DependencyInputDigests), err
		},
		MaxManifestBytes: 64 << 10, MaxFileBytes: 64 << 10, MaxSourceBytes: 128 << 10}
	stdRoot, err := paths.OpenRoot(filepath.Join(goRoot, "src"))
	if err != nil {
		t.Fatal(err)
	}
	defer stdRoot.Close()
	allowed := proof.StandardPackages()
	stdlib, err := golanganalysis.NewTrustedStdlib(ctx, golanganalysis.TrustedStdlibInput{SourceRoot: stdRoot, GoRoot: goRoot,
		Profile: profile, Snapshot: captured, DependencyDigest: proof.Digest(), AllowedPackages: allowed, Scanner: scanner,
		MaxManifestBytes: 64 << 10, MaxFileBytes: 2 << 20, MaxFiles: 8192, MaxSourceBytes: 64 << 20,
		AuthorizePath: func(ctx context.Context, name string) error { return proof.CheckPath(ctx, "src/"+name) },
		VerifyAuthority: func(ctx context.Context, root, toolchain, dependencies string, packages []string) (bool, error) {
			err := proof.CheckPath(ctx, "src")
			return err == nil && root == filepath.Join(goRoot, "src") && toolchain == proof.Digest() &&
				dependencies == proof.Digest() && slices.Equal(packages, allowed), err
		}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := golanganalysis.LoadCapturedPackages(ctx, golanganalysis.CapturedLoaderInput{Preflight: input,
		GoRoot: goRoot, ScratchRoot: scratch, WorkspaceRoot: workspace, Tool: tool, Stdlib: stdlib, VerifyScratch: verify,
		MaxOutputBytes: 64 << 10})
	if err != nil || !loaded.BuildSelectionVerified || len(loaded.Packages) != 1 || !slices.Equal(loaded.Packages[0].Files, []string{"fixture.go"}) {
		t.Fatalf("native typed captured loader returned %s, reasons=%v packages=%v error=%v", loaded.Status, loaded.Reasons, loaded.Packages, err)
	}
	unexpected := filepath.Join(workspace, "unbound.go")
	if err := os.WriteFile(unexpected, []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verified, err := verify(ctx, scratch, workspace, captured)
	removeErr := os.Remove(unexpected)
	if verified || err != nil || removeErr != nil {
		t.Fatalf("unexpected file in typed fixture: accepted=%v verify=%v remove=%v", verified, err, removeErr)
	}
	t.Log("native Go packages loader verified the typed captured module and trusted stdlib closure and refused an extra file in its protected input")
	return captured, verify
}

func readWindowsFixtureLSP(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length: ")))
	if err != nil || n < 1 || n > 262144 || !strings.HasPrefix(line, "Content-Length: ") {
		return nil, fmt.Errorf("unexpected gopls frame header")
	}
	blank, err := reader.ReadString('\n')
	if err != nil || blank != "\r\n" {
		return nil, fmt.Errorf("missing gopls frame separator")
	}
	data := make([]byte, n)
	_, err = io.ReadFull(reader, data)
	return data, err
}
