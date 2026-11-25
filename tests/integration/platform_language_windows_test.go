package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	pythonprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/python-provider"
	tsprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/ts-provider"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type windowsLanguageSource struct {
	path, name, body string
}

type windowsLanguageFixture struct {
	name, executable, digest, provider string
	profile                            model.EffectiveProfile
	initial                            map[string]string
	base                               windowsLanguageSource
	version                            func(string) []windowsLanguageSource
	canary                             string
}

func testPackagedWindowsTypeScript(t *testing.T, ctx context.Context, bin, installation, target, worktree, material string) {
	t.Helper()
	profile, err := tsprovider.WindowsProfile([]string{"tsconfig.json"}, "typed_when_available")
	if err != nil {
		t.Fatal(err)
	}
	base := windowsLanguageSource{"base.ts", "BaseTS", "export function BaseTS() { return 12; }\n"}
	canary := filepath.Join(target, "repo-executed")
	initial := map[string]string{
		".gitignore":     "node_modules/\ndist/\n",
		"tsconfig.json":  `{"compilerOptions":{"strict":true,"target":"es2022","lib":["es2022"],"types":[],"allowJs":true,"checkJs":true},"include":["*.ts","*.js"]}`,
		base.path:        base.body,
		"side-effect.js": "require('node:fs').writeFileSync(" + fmt.Sprintf("%q", canary) + ", 'canary');\r\n",
	}
	version := func(letter string) []windowsLanguageSource {
		const ts, js = "WatchedType", "WatchedScript"
		return []windowsLanguageSource{{"watch.ts", ts, "// 🗺 café\r\nexport function " + ts + "(): string { return '" + letter + "'; }\r\n"},
			{"watch-script.js", js, "// 🗺 café\r\nexport function " + js + "() { return '" + letter + "'; }\r\n"}}
	}
	testPackagedWindowsLanguage(t, ctx, bin, installation, target, worktree, material, windowsLanguageFixture{
		name: "typescript", executable: "node.exe", digest: profile.NodeDigest, provider: tsprovider.ProviderID,
		profile: profile, initial: initial, base: base, version: version, canary: canary,
	})
}

func testPackagedWindowsPython(t *testing.T, ctx context.Context, bin, installation, target, worktree, material string) {
	t.Helper()
	mode := os.Getenv("MCE4D_WIN_PYTHON_MODE")
	if mode == "" {
		mode = "syntax"
	}
	profile, err := pythonprovider.WindowsProfile([]string{"."}, mode)
	if err != nil {
		t.Fatal(err)
	}
	base := windowsLanguageSource{"base.py", "base_python", "def base_python():\n    return 12\n"}
	version := func(letter string) []windowsLanguageSource {
		name := "watched_python"
		body := "# 🗺 café\r\ndef " + name + "():\r\n    return '" + letter + "'\r\n"
		if mode == "typed_when_available" {
			name = "WATCHED_LITERAL"
			body = "# 🗺 café\r\n" + name + " = '" + letter + "'\r\n"
		}
		return []windowsLanguageSource{{"watch.py", name, body}}
	}
	if mode == "typed_when_available" {
		base.name, base.body = "BASE_LITERAL", "BASE_LITERAL = 12\n"
	}
	canary := filepath.Join(target, "repo-executed")
	sideEffect := "from pathlib import Path\nPath(" + fmt.Sprintf("%q", canary) + ").touch()\n"
	t.Logf("Python mode %s", mode)
	testPackagedWindowsLanguage(t, ctx, bin, installation, target, worktree, material, windowsLanguageFixture{
		name: "python", executable: "python.exe", digest: profile.InterpreterDigest, provider: pythonprovider.ProviderID,
		profile: profile, initial: map[string]string{base.path: base.body, "side_effect.py": sideEffect, "sitecustomize.py": sideEffect},
		base: base, version: version, canary: canary,
	})
}

func testPackagedWindowsLanguage(t *testing.T, ctx context.Context, bin, installation, target, worktree, material string, fixture windowsLanguageFixture) {
	t.Helper()
	tool := filepath.Join(installWindowsProviderFixture(t, material, installation, fixture.name), fixture.executable)
	command := func(ctx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "mce4d.exe"), args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1")
		return cmd
	}
	run := func(expected int, payload any, args ...string) []byte {
		t.Helper()
		cmd := command(ctx, args...)
		if payload != nil {
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stdin = bytes.NewReader(raw)
		}
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if expected == 0 && err == nil || expected != 0 && errors.As(err, &exit) && exit.ExitCode() == expected {
			return out
		}
		t.Fatalf("%v expected exit %d: %v %s", args, expected, err, out)
		return nil
	}
	setPolicy := func(epoch int64, allowed []model.AllowedExecutable) {
		run(0, model.PolicyAdminRequest{WorktreeID: worktree, ExpectedEpoch: epoch, Policy: model.PolicyConfig{
			SchemaVersion: 1, AllowedRoots: []string{target}, DeniedGlobs: []string{}, TrustedRulePaths: []string{},
			DependencyRoots: []string{}, AllowedExecutables: allowed, MaxFileBytes: 1 << 20, MaxSourceBytes: 1 << 20,
			MaxOutputBytes: 24576, SecretRulesVersion: "builtin-v1",
		}}, "policy", "set", "--input-json", "-")
	}
	setPolicy(1, []model.AllowedExecutable{})
	var registration struct{ Data model.ProfileAdminData }
	request := model.ProfileAdminRequest{WorktreeID: worktree, ProfileName: fixture.name, Profile: fixture.profile}
	profileArgs := []string{"profile", "set", "--worktree-id", worktree, "--input-json", "-"}
	raw, err := json.Marshal(request.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var invalid map[string]any
	if err := json.Unmarshal(raw, &invalid); err != nil {
		t.Fatal(err)
	}
	invalid["provider_digest"] = strings.Repeat("0", 64)
	run(3, map[string]any{"worktree_id": worktree, "profile_name": fixture.name, "expected_revision": 0, "profile": invalid}, profileArgs...)
	if err := json.Unmarshal(run(0, request, profileArgs...), &registration); err != nil || !model.ValidDigest(registration.Data.ProfileID) {
		t.Fatalf("profile registration failed: %v", err)
	}
	profile := registration.Data.ProfileID
	scope := []string{"--worktree-id", worktree, "--profile-id", profile}
	indexArgs := append([]string{"index"}, scope...)
	run(3, nil, indexArgs...)
	setPolicy(2, []model.AllowedExecutable{{Path: tool, SHA256: fixture.digest}})
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(target, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range fixture.initial {
		write(path, body)
	}
	before := powerShellFixtureHashes(t, target)
	if out := run(0, nil, indexArgs...); !bytes.Contains(out, []byte(fixture.provider)) || !bytes.Contains(out, []byte("indexed ")) {
		t.Fatalf("initial index did not publish provider coverage: %s", out)
	}
	query := func(name string, args any) []byte {
		t.Helper()
		return run(0, args, append(append([]string{}, scope...), "query", name, "--input-json", "-")...)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "windows-native-language", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: command(ctx, append([]string{"mcp"}, scope...)...), TerminateDuration: 3 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 6 {
		t.Fatalf("unexpected MCP registry: %v", err)
	}
	call := func(name string, args any) []byte {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result == nil || result.IsError || len(result.Content) != 1 {
			t.Fatalf("MCP %s failed: %v %+v", name, err, result)
		}
		return []byte(result.Content[0].(*sdk.TextContent).Text)
	}
	status := func(raw []byte) string {
		t.Helper()
		var reply struct {
			Data  model.StatusData
			Scope model.ScopeVector
		}
		if err := json.Unmarshal(raw, &reply); err != nil || reply.Data.IndexState != "published" || len(reply.Scope.Members) != 1 {
			t.Fatalf("missing generation: %v %s", err, raw)
		}
		member := reply.Scope.Members[0]
		if member.WorktreeID != worktree || member.ProfileID != profile || !model.ValidUUID(member.GenerationID) {
			t.Fatalf("unexpected scope: %+v", member)
		}
		return member.GenerationID
	}
	statusArgs := map[string]any{}
	generation := status(query("status", statusArgs))
	verifyExplorer := func(string, string, []windowsLanguageSource, []windowsLanguageSource) {}
	if driver := os.Getenv("MCE4D_WIN_EXPLORER_DRIVER"); driver != "" {
		verifyExplorer = startWindowsExplorerWatchBrowser(t, ctx, bin, installation, worktree, profile, driver)
	}
	verifyBoth := func(present, absent []windowsLanguageSource) {
		t.Helper()
		if status(query("status", statusArgs)) != generation || status(call("mce4d_workspace_status", statusArgs)) != generation {
			t.Fatal("CLI and existing MCP did not see the reported generation")
		}
		verify := func(src windowsLanguageSource, exists bool) {
			t.Helper()
			args := map[string]any{"mode": "symbol", "query": src.name, "limit": 6}
			for _, raw := range [][]byte{query("locate", args), call("mce4d_locate", args)} {
				var reply struct {
					Data struct {
						Candidates []struct {
							Name    string
							Sources []model.SourceRef `json:"source_refs"`
						}
					}
				}
				if err := json.Unmarshal(raw, &reply); err != nil {
					t.Fatal(err)
				}
				found := 0
				for _, candidate := range reply.Data.Candidates {
					if candidate.Name != src.name {
						continue
					}
					for _, ref := range candidate.Sources {
						found++
						if ref.Path != src.path || ref.GenerationID != generation || ref.ProfileID != profile || ref.WorktreeID != worktree ||
							ref.ContentHash != fmt.Sprintf("%x", sha256.Sum256([]byte(src.body))) || ref.Range.StartByte < 0 ||
							ref.Range.EndByte <= ref.Range.StartByte || ref.Range.EndByte > int64(len(src.body)) ||
							!strings.Contains(src.body[ref.Range.StartByte:ref.Range.EndByte], src.name) {
							t.Fatalf("source reference does not match capture: %s", raw)
						}
					}
				}
				if exists && found != 1 || !exists && found != 0 {
					t.Fatalf("unexpected results for %s (present=%t): %s", src.name, exists, raw)
				}
			}
		}
		for _, source := range present {
			verify(source, true)
		}
		for _, source := range absent {
			verify(source, false)
		}
	}
	verifyBoth([]windowsLanguageSource{fixture.base}, nil)
	verifyExplorer("initial", generation, []windowsLanguageSource{fixture.base}, nil)
	t.Logf("%s index: CLI and MCP verified initial generation %s", fixture.name, generation)
	watchCtx, stop := context.WithTimeout(ctx, 120*time.Second)
	defer stop()
	watch := command(watchCtx, append([]string{"watch"}, scope...)...)
	var stdout bytes.Buffer
	watch.Stdout = &stdout
	stderr, err := watch.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(installation, "control", "worktrees", worktree, "index-control", "wal-head.log")
	beforeHead, err := os.ReadFile(head)
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			stop()
			_ = watch.Wait()
		}
	}()
	type watchLine struct {
		text string
		at   time.Time
	}
	lines := make(chan watchLine, 32)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case lines <- watchLine{scanner.Text(), time.Now()}:
			case <-watchCtx.Done():
				return
			}
		}
	}()
	await := func(action string) {
		t.Helper()
		started := time.Now()
		for line := range lines {
			fields := strings.Fields(line.text)
			if len(fields) != 8 || fields[0] != "mce4d:" || fields[4] != "generation" || fields[6] != fixture.provider {
				t.Fatalf("unexpected watch output: %s", line.text)
			}
			id := strings.TrimSuffix(fields[5], ";")
			if fields[1] == "unchanged" && id == generation {
				// An idle event received before the caller's assertions is
				// not a safe point for stopping the next in-flight poll.
				if action == "unchanged" && !line.at.Before(started) {
					return
				}
				continue
			}
			if fields[1] == action && action == "indexed" && id != generation && model.ValidUUID(id) {
				generation = id
				return
			}
			t.Fatalf("unexpected generation while awaiting %s: %s", action, line.text)
		}
		t.Fatalf("watch stopped while awaiting %s: %v", action, watchCtx.Err())
	}
	await("unchanged")
	await("unchanged")
	if after, err := os.ReadFile(head); err != nil || !bytes.Equal(beforeHead, after) {
		t.Fatalf("idle Watch changed the WAL head: %v", err)
	}
	first, changed := fixture.version("A"), fixture.version("B")
	for _, source := range first {
		write(source.path, source.body)
	}
	await("indexed")
	verifyBoth(first, nil)
	verifyExplorer("add", generation, append([]windowsLanguageSource{fixture.base}, first...), nil)
	t.Logf("%s watch add: CLI/MCP generation %s", fixture.name, generation)
	await("unchanged")
	for _, source := range changed {
		path := filepath.Join(target, source.path)
		old, err := os.Stat(path)
		if err != nil || old.Size() != int64(len(source.body)) {
			t.Fatalf("edit fixture did not preserve file size: %v", err)
		}
		write(source.path, source.body)
		if err := os.Chtimes(path, old.ModTime(), old.ModTime()); err != nil {
			t.Fatal(err)
		}
		if current, err := os.Stat(path); err != nil || current.Size() != old.Size() || !current.ModTime().Equal(old.ModTime()) {
			t.Fatalf("edit fixture did not preserve file metadata: %v", err)
		}
	}
	await("indexed")
	verifyBoth(changed, nil)
	verifyExplorer("edit", generation, append([]windowsLanguageSource{fixture.base}, changed...), nil)
	t.Logf("%s watch edit: CLI/MCP generation %s", fixture.name, generation)
	await("unchanged")
	for _, source := range changed {
		if err := os.Remove(filepath.Join(target, source.path)); err != nil {
			t.Fatal(err)
		}
	}
	await("indexed")
	verifyBoth(nil, changed)
	verifyExplorer("delete", generation, []windowsLanguageSource{fixture.base}, changed)
	t.Logf("%s watch delete: CLI/MCP generation %s", fixture.name, generation)
	await("unchanged")
	stop()
	_ = watch.Wait()
	stopped = true
	if stdout.Len() != 0 {
		t.Fatal("Watch wrote progress to stdout")
	}
	if out := run(0, nil, indexArgs...); !bytes.Contains(out, []byte("unchanged ")) || !bytes.Contains(out, []byte(generation)) {
		t.Fatalf("index could not reuse the generation after Watch: %s", out)
	}
	if !reflect.DeepEqual(before, powerShellFixtureHashes(t, target)) {
		t.Fatal("provider changed the captured worktree or Git metadata")
	}
	if _, err := os.Stat(fixture.canary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture execution marker absence was not verified: %v", err)
	}
	scratch, err := filepath.Glob(filepath.Join(installation, "control", "worktrees", worktree, "scratch", "provider-*"))
	if err != nil || len(scratch) != 0 {
		t.Fatalf("provider scratch retained: %v %v", scratch, err)
	}
	t.Logf("%s: idle WAL head unchanged, source/Git hashes verified, provider scratch empty", fixture.name)
}
