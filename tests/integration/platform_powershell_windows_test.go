package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	powershellprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/powershell-provider"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/windows"
)

func testPackagedWindowsPowerShell(t *testing.T, ctx context.Context, bin, installation, target, worktree, material string) {
	t.Helper()
	profile, err := powershellprovider.Profile()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, "mce4d.exe")
	worker := installWindowsPowerShellFixture(t, filepath.Dir(material), installation)
	command := func(ctx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1")
		return cmd
	}
	run := func(want int, payload any, args ...string) []byte {
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
		if want == 0 && err == nil || want != 0 && errors.As(err, &exit) && exit.ExitCode() == want {
			return out
		}
		t.Fatalf("%v expected exit %d: %v %s", args, want, err, out)
		return nil
	}
	psPolicy := model.PolicyConfig{
		SchemaVersion: 1, AllowedRoots: []string{target}, DeniedGlobs: []string{"denied.ps1"},
		TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{},
		MaxFileBytes: 1048576, MaxSourceBytes: 1048576, MaxOutputBytes: 24576, SecretRulesVersion: "builtin-v1",
	}
	policyRequest := model.PolicyAdminRequest{WorktreeID: worktree, ExpectedEpoch: 1, Policy: psPolicy}
	policyArgs := []string{"policy", "set", "--input-json", "-"}
	run(0, policyRequest, policyArgs...)
	profileArgs := []string{"profile", "set", "--worktree-id", worktree, "--input-json", "-"}
	bad := profile
	bad.ParserDigest = strings.Repeat("0", 64)
	request := model.ProfileAdminRequest{WorktreeID: worktree, ProfileName: "powershell", Profile: &bad}
	run(3, request, profileArgs...)
	bad = profile
	bad.AnalysisMode = "typed_when_available"
	run(2, request, profileArgs...)
	request.Profile = &profile
	var registration struct{ Data model.ProfileAdminData }
	if err := json.Unmarshal(run(0, request, profileArgs...), &registration); err != nil || !model.ValidDigest(registration.Data.ProfileID) {
		t.Fatalf("profile registration failed: %v", err)
	}
	profileID := registration.Data.ProfileID
	scope := []string{"--worktree-id", worktree, "--profile-id", profileID}
	indexArgs := append([]string{"index"}, scope...)
	if output := run(3, nil, indexArgs...); !bytes.Contains(output, []byte("capability")) {
		t.Fatal("unapproved PowerShell executable accepted")
	}
	psPolicy.AllowedExecutables = []model.AllowedExecutable{{Path: worker, SHA256: profile.ExecutableDigest}}
	policyRequest.ExpectedEpoch, policyRequest.Policy = 2, psPolicy
	run(0, policyRequest, policyArgs...)

	const prefix = "# 🗺 source café\r\n"
	const definition = "function Read-Map { 'index' }"
	const source = prefix + definition + "\r\nfilter Get-Map { $_ }\r\nNew-Item -Path ./repo-executed -ItemType File\r\n"
	for name, contents := range map[string]string{
		"map.ps1": source, "module.ps1": "using module ./zero.psm1\nfunction ForbiddenNativeMap {}",
		"syntax.ps1": "function BrokenNativeMap {", "denied.ps1": "function DeniedNativeMap {}",
		"module.psm1": "function HiddenNativeMap {}", "large.ps1": "function LargeNativeMap {}\n" + strings.Repeat("#", 65536),
	} {
		if err := os.WriteFile(filepath.Join(target, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := powerShellFixtureHashes(t, target)
	first := run(0, nil, indexArgs...)
	if !bytes.Contains(first, []byte("mce4d-powershell-syntax partial")) {
		t.Fatalf("missing PowerShell coverage: %s", first)
	}
	var status struct {
		Data  model.StatusData
		Scope model.ScopeVector
	}
	query := func(operation string, body any) []byte {
		t.Helper()
		return run(0, body, append(append([]string{}, scope...), "query", operation, "--input-json", "-")...)
	}
	if err := json.Unmarshal(query("status", map[string]any{}), &status); err != nil || status.Data.IndexState != "published" || len(status.Scope.Members) != 1 {
		t.Fatalf("PowerShell index unavailable: %v", err)
	}
	member := status.Scope.Members[0]
	if unchanged := run(0, nil, indexArgs...); !bytes.Contains(unchanged, []byte("unchanged ")) ||
		!bytes.Contains(unchanged, []byte(member.GenerationID)) {
		t.Fatalf("PowerShell no-op changed generation: %s", unchanged)
	}
	want := model.SourceRef{WorktreeID: worktree, ProfileID: profileID, GenerationID: member.GenerationID,
		Path: "map.ps1", ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source))),
		Range: model.ByteRange{StartByte: int64(len(prefix)), EndByte: int64(len(prefix + definition))}}
	checkLocate := func(raw []byte) any {
		t.Helper()
		var reply struct {
			Data struct {
				Candidates []struct {
					Name    string
					Ref     json.RawMessage
					Sources []model.SourceRef `json:"source_refs"`
				}
			}
		}
		if err := json.Unmarshal(raw, &reply); err != nil || len(reply.Data.Candidates) != 1 {
			t.Fatalf("missing unique PowerShell symbol: %v %s", err, raw)
		}
		c := reply.Data.Candidates[0]
		if c.Name != "Read-Map" || len(c.Sources) != 1 || c.Sources[0] != want {
			t.Fatal("PowerShell UTF-8 range or capture identity differs")
		}
		return c.Ref
	}
	locate := map[string]any{"mode": "symbol", "query": "Read-Map", "limit": 4}
	ref := checkLocate(query("locate", locate))
	filter := query("locate", map[string]any{"mode": "symbol", "query": "Get-Map", "limit": 4})
	if !bytes.Contains(filter, []byte(`"name":"Get-Map"`)) {
		t.Fatalf("missing captured filter: %s", filter)
	}
	for _, absent := range []string{"ForbiddenNativeMap", "BrokenNativeMap", "DeniedNativeMap", "HiddenNativeMap", "LargeNativeMap"} {
		raw := query("locate", map[string]any{"mode": "symbol", "query": absent, "limit": 4})
		if !bytes.Contains(raw, []byte(`"candidates":[]`)) {
			t.Fatalf("adapter returned an unsupported declaration: %s", raw)
		}
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "c079-powershell", Version: "1"}, nil)
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
		reply, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || reply == nil || len(reply.Content) != 1 {
			t.Fatalf("PowerShell MCP %s: %v", name, err)
		}
		text, ok := reply.Content[0].(*sdk.TextContent)
		if !ok {
			t.Fatalf("PowerShell MCP %s has no text response", name)
		}
		if reply.IsError {
			t.Fatalf("PowerShell MCP %s rejected the synthetic scope: %s", name, text.Text)
		}
		return []byte(text.Text)
	}
	checkLocate(call("mce4d_locate", locate))
	pack := call("mce4d_context_pack", map[string]any{"task_kind": "explore", "max_output_bytes": 24576,
		"selector": map[string]any{"kind": "ref", "ref": ref, "service_id": nil}})
	if !bytes.Contains(pack, []byte(definition)) || !bytes.Contains(pack, []byte(member.GenerationID)) {
		t.Fatal("PowerShell context missing the captured declaration")
	}
	t.Log("C079 native: pinned syntax, two declarations, UTF-8 ranges, policy and profile refusals, CLI/MCP scope")
	verifyExplorer := func(string, string, []windowsLanguageSource, []windowsLanguageSource) {}
	if driver := os.Getenv("MCE4D_WIN_EXPLORER_DRIVER"); driver != "" {
		verify := startWindowsExplorerWatchBrowser(t, ctx, bin, installation, worktree, profileID, driver)
		baseline := []windowsLanguageSource{{want.Path, "Read-Map", source}, {want.Path, "Get-Map", source}}
		verify("initial", member.GenerationID, baseline, nil)
		verifyExplorer = func(phase, generation string, present, absent []windowsLanguageSource) {
			verify(phase, generation, append(append([]windowsLanguageSource{}, baseline...), present...), absent)
		}
	}
	testPowerShellWatch(t, ctx, member, installation, target, command, query, call, verifyExplorer)
	if _, err := os.Lstat(filepath.Join(filepath.Dir(worker), "repo-executed")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("could not verify canary absence in the worker directory: %v", err)
	}
	if after := powerShellFixtureHashes(t, target); len(after) != len(before) {
		t.Fatal("PowerShell changed the target tree")
	} else {
		for path, hash := range before {
			if after[path] != hash {
				t.Fatal("PowerShell changed fixture source or Git metadata")
			}
		}
	}
	t.Log("C079 read-only: fixture source and Git hashes unchanged after caller edits were reverted, and execution canary absent")
}

func testPowerShellWatch(t *testing.T, parent context.Context, member model.ScopeMember, installation, target string,
	command func(context.Context, ...string) *exec.Cmd, query, call func(string, any) []byte,
	verifyExplorer func(string, string, []windowsLanguageSource, []windowsLanguageSource)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 65*time.Second)
	defer cancel()
	cmd := command(ctx, "watch", "--worktree-id", member.WorktreeID, "--profile-id", member.ProfileID)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(installation, "control", "worktrees", member.WorktreeID, "index-wal", "wal-authority.log")
	head := filepath.Join(installation, "control", "worktrees", member.WorktreeID, "index-control", "wal-head.log")
	readBytes := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	beforeReceipt, beforeHead := readBytes(receipt), readBytes(head)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := cmd.Wait(); err == nil || stdout.Len() != 0 {
			t.Errorf("unexpected foreground watch exit or stdout: %v %q", err, stdout.Bytes())
		}
	}()
	lines := bufio.NewScanner(stderr)
	generation := member.GenerationID
	await := func(action string) {
		t.Helper()
		for lines.Scan() {
			line := lines.Text()
			fields := strings.Fields(line)
			if len(fields) != 8 || fields[0] != "mce4d:" || fields[4] != "generation" ||
				!strings.HasSuffix(line, "mce4d-powershell-syntax partial") {
				t.Fatalf("unexpected watch output: %s", line)
			}
			id := strings.TrimSuffix(fields[5], ";")
			switch {
			case fields[1] == "unchanged" && id == generation:
				if action == "unchanged" {
					return
				}
			case fields[1] == "indexed" && action == "indexed" && id != generation && model.ValidUUID(id):
				generation = id
				return
			default:
				t.Fatalf("unexpected %s while awaiting %s", line, action)
			}
		}
		t.Fatalf("watch stopped while awaiting %s: %v", action, lines.Err())
	}
	await("unchanged")
	await("unchanged")
	if !bytes.Equal(beforeReceipt, readBytes(receipt)) || !bytes.Equal(beforeHead, readBytes(head)) {
		t.Fatal("idle PowerShell Watch advanced the WAL authority")
	}
	t.Logf("C079 watch idle: generation %s and authenticated WAL receipt/head unchanged", generation)

	checkScope := func(raw []byte) {
		t.Helper()
		var result struct {
			Data  model.StatusData
			Scope model.ScopeVector
		}
		if err := json.Unmarshal(raw, &result); err != nil || result.Data.IndexState != "published" || len(result.Scope.Members) != 1 {
			t.Fatalf("watch status has no published scope: %v %s", err, raw)
		}
		scope := result.Scope.Members[0]
		if scope.GenerationID != generation || scope.WorktreeID != member.WorktreeID || scope.ProfileID != member.ProfileID {
			t.Fatalf("status returned a different watch scope: %+v", scope)
		}
	}
	const prefix = "# 🗺 Watch\r\n"
	const suffix = "\r\nNew-Item -Path ./repo-executed -ItemType File\r\n"
	const name = "Read-Watched"
	const declA, declB = "function " + name + " { 'first' }", "function " + name + " { 'final' }"
	const path = "watch.ps1"
	initial := []windowsLanguageSource{{path, name, prefix + declA + suffix}}
	revised := []windowsLanguageSource{{path, name, prefix + declB + suffix}}
	file := filepath.Join(target, path)
	assertLocate := func(name, decl string) {
		t.Helper()
		args := map[string]any{"mode": "symbol", "query": name, "limit": 4}
		for _, raw := range [][]byte{query("locate", args), call("mce4d_locate", args)} {
			var result struct {
				Data struct {
					Candidates []struct {
						Name       string
						SourceRefs []model.SourceRef `json:"source_refs"`
					}
				}
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatalf("watch locate is invalid: %v", err)
			}
			if decl == "" {
				if len(result.Data.Candidates) != 0 {
					t.Fatalf("watch retained stale PowerShell declaration %s: %s", name, raw)
				}
				continue
			}
			want := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: generation,
				Path: path, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(prefix+decl+suffix))),
				Range: model.ByteRange{StartByte: int64(len(prefix)), EndByte: int64(len(prefix + decl))}}
			if len(result.Data.Candidates) != 1 || result.Data.Candidates[0].Name != name ||
				len(result.Data.Candidates[0].SourceRefs) != 1 || result.Data.Candidates[0].SourceRefs[0] != want {
				t.Fatalf("watch locate did not return the new symbol and captured source: %s", raw)
			}
		}
	}
	checkBothScopes := func() {
		t.Helper()
		checkScope(query("status", map[string]any{}))
		checkScope(call("mce4d_workspace_status", map[string]any{}))
	}
	if err := os.WriteFile(file, []byte(prefix+declA+suffix), 0600); err != nil {
		t.Fatal(err)
	}
	await("indexed")
	checkBothScopes()
	assertLocate(name, declA)
	verifyExplorer("add", generation, initial, nil)
	t.Logf("C079 watch add: %s is visible in CLI and the existing MCP session at generation %s", name, generation)
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	await("unchanged")
	if err := os.WriteFile(file, []byte(prefix+declB+suffix), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if edited, err := os.Stat(file); err != nil || edited.Size() != before.Size() || !edited.ModTime().Equal(before.ModTime()) {
		t.Fatal("watch edit did not preserve size and mtime", err)
	}
	await("indexed")
	checkBothScopes()
	assertLocate(name, declB)
	verifyExplorer("edit", generation, revised, nil)
	t.Logf("C079 watch edit: same-size and same-mtime edit captured new source for %s at generation %s", name, generation)
	await("unchanged")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	await("indexed")
	checkBothScopes()
	assertLocate(name, "")
	verifyExplorer("delete", generation, nil, revised)
	await("unchanged")
	t.Logf("C079 watch delete: both readers dropped %s at generation %s", name, generation)
}

func powerShellFixtureHashes(t *testing.T, target string) map[string][32]byte {
	t.Helper()
	hashes := map[string][32]byte{}
	err := filepath.WalkDir(target, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(name)
		if err == nil {
			hashes[name] = sha256.Sum256(raw)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return hashes
}

func installWindowsPowerShellFixture(t *testing.T, source, installation string) string {
	t.Helper()
	return filepath.Join(installWindowsProviderFixture(t, source, installation, "powershell"), "pwsh.exe")
}

func installWindowsProviderFixture(t *testing.T, source, installation, language string) string {
	t.Helper()
	account, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + account.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installation, "control", language)
	var total int64
	err = filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("invalid provider fixture path")
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			err = os.Mkdir(dest, 0700)
		} else {
			if !entry.Type().IsRegular() {
				return fmt.Errorf("nonregular provider material")
			}
			in, err := os.Open(name)
			if err != nil {
				return err
			}
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				_ = in.Close()
				return err
			}
			n, err := io.Copy(out, io.LimitReader(in, (512<<20)-total+1))
			total += n
			if err := errors.Join(err, in.Close(), out.Close()); err != nil || total > 512<<20 {
				return fmt.Errorf("provider fixture material limit: %w", err)
			}
		}
		if err != nil {
			return err
		}
		return windows.SetNamedSecurityInfo(dest, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}
