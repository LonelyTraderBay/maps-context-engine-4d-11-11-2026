package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/windows"
)

func testWindowsPublicRename(t *testing.T, ctx context.Context, bin, installation, target, worktree string) {
	t.Helper()
	executable := filepath.Join(bin, "mce4d.exe")
	approved := filepath.Join(t.TempDir(), "gopls.exe")
	raw := approvedWindowsFixtureFile(t, os.Getenv("MCE4D_GOPLS_WIN_FIXTURE"), os.Getenv("MCE4D_GOPLS_WIN_SHA256"), 96<<20)
	if err := os.WriteFile(approved, raw, 0700); err != nil {
		t.Fatal(err)
	}
	goRoot := os.Getenv("MCE4D_GOPLS_WIN_GOROOT")
	proof, err := runtime.InspectGoToolchain(ctx, goRoot)
	if err != nil {
		t.Fatal(err)
	}
	toolchain := proof.Digest()
	if err := proof.Close(); err != nil {
		t.Fatal(err)
	}
	build, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	command := func(tool string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, filepath.Join(bin, tool+".exe"), args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1",
			"PATH="+filepath.Join(goRoot, "bin"), "PATHEXT=.EXE")
		return cmd
	}
	run := func(want int, input any, cmd *exec.Cmd) []byte {
		t.Helper()
		if input != nil {
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stdin = bytes.NewReader(raw)
		}
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if (want == 0 && err == nil) || (want != 0 && errors.As(err, &exit) && exit.ExitCode() == want) {
			return out
		}
		t.Fatalf("packaged command %q expected exit %d: %v %s", cmd.Args[1:], want, err, out)
		return nil
	}
	opts := model.DefaultRuntimeConfig().Core
	policy := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{target}, DeniedGlobs: []string{},
		TrustedRulePaths: []string{}, DependencyRoots: []string{}, SecretRulesVersion: "builtin-v1",
		MaxFileBytes: opts.MaxFileBytes, MaxSourceBytes: opts.MaxSourceBytesPerSnapshot, MaxOutputBytes: opts.MaxOutputBytes,
		AllowedExecutables: []model.AllowedExecutable{
			{Path: filepath.Join(goRoot, "bin", "go.exe"), SHA256: os.Getenv("MCE4D_GO_WIN_SHA256")},
			{Path: approved, SHA256: os.Getenv("MCE4D_GOPLS_WIN_SHA256")},
		}}
	run(0, model.PolicyAdminRequest{WorktreeID: worktree, ExpectedEpoch: 1, Policy: policy},
		command("mce4d", "policy", "set", "--input-json", "-"))
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "windows", Goarch: "amd64", BuildTags: []string{},
		ModuleRoots: []string{"."}, ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{},
		ToolchainDigest: toolchain, ProviderDigest: fmt.Sprintf("%x", sha256.Sum256(build))}
	profileID, err := model.ProfileDigest(profile, 32768)
	if err != nil {
		t.Fatal(err)
	}
	run(0, model.ProfileAdminRequest{WorktreeID: worktree, ProfileName: "public-rename", Profile: profile},
		command("mce4d", "profile", "set", "--worktree-id", worktree, "--input-json", "-"))
	filename := filepath.Join(target, "second.go")
	source := []byte("package second\r\nimport \"strings\"\r\nfunc Original() string { return strings.TrimSpace(\"Tiếng Việt 😀\") }\r\nfunc Calling() string { return Original() }\r\n")
	if err := os.WriteFile(filename, source, 0600); err != nil {
		t.Fatal(err)
	}
	activate := func() *exec.Cmd {
		return command("state-profile", "activate", "--root", installation, "--worktree-id", worktree, "--profile-id", profileID)
	}
	run(3, nil, activate())
	run(0, nil, command("mce4d", "index", "--worktree-id", worktree, "--profile-id", profileID))
	for _, cmd := range []*exec.Cmd{activate(), command("state-profile", "check", "--root", installation)} {
		var ready struct {
			Stage   string `json:"stage"`
			Trusted bool   `json:"trusted_head"`
		}
		if out := run(0, nil, cmd); json.Unmarshal(out, &ready) != nil || ready.Stage != "READY" || !ready.Trusted {
			t.Fatalf("protected profile did not report READY with verified heads: %s", out)
		}
	}
	t.Log("packaged profile refused activation before indexing and reported READY after authenticating all WAL heads")
	query := func(action string) *exec.Cmd {
		return command("mce4d", "--worktree-id", worktree, "--profile-id", profileID, "query", action, "--input-json", "-")
	}
	request := model.RenameInitialRequest{
		Scope: &model.ScopeRequest{WorktreeID: &worktree, ProfileID: &profileID},
		Selector: model.PositionSelector{Kind: "source_position", Path: "second.go", ContentHash: fmt.Sprintf("%x", sha256.Sum256(source)),
			ByteOffset: int64(bytes.Index(source, []byte("Original")))},
		NewName: "Relabeled", PreserveContracts: true}
	checkPreview := func(raw []byte) string {
		t.Helper()
		var response struct {
			Data model.RenameData `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil || response.Data.Applied || response.Data.SourceMutationCount != 0 ||
			response.Data.ResultToken == "" || len(response.Data.Edits) != 2 || !response.Data.PreviewComplete {
			t.Fatalf("public preview did not produce two confined edits: %v %s", err, raw)
		}
		for _, edit := range response.Data.Edits {
			if edit.SourceRef.Path != "second.go" || edit.NewText != "Relabeled" {
				t.Fatal("unexpected public rename edit")
			}
		}
		return response.Data.ResultToken
	}
	start := time.Now()
	cliToken := checkPreview(run(0, request, query("rename-preview")))
	t.Logf("packaged CLI rename completed in %s", time.Since(start).Round(time.Millisecond))
	checkValidation := func(raw []byte) {
		t.Helper()
		var response struct {
			Status string             `json:"status"`
			Data   model.ValidateData `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil || response.Status != "ok" || !response.Data.EditPreconditionValidated {
			t.Fatalf("public validation did not verify edit preconditions: %v %s", err, raw)
		}
	}
	verify := model.ValidateRequest{ResultToken: &cliToken}
	checkValidation(run(0, verify, query("validate")))
	t.Log("a fresh CLI process validated the issued token")
	sdkClient := sdk.NewClient(&sdk.Implementation{Name: "mce4d_windows_public_rename", Version: "1"}, nil)
	session, err := sdkClient.Connect(ctx, &sdk.CommandTransport{
		Command:           command("mce4d", "mcp", "--worktree-id", worktree, "--profile-id", profileID),
		TerminateDuration: 3 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, params any) []byte {
		t.Helper()
		reply, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: params})
		if err != nil || reply.IsError {
			t.Fatalf("MCP %s failed: %v %v", name, err, reply)
		}
		if reply.StructuredContent != nil {
			raw, err := json.Marshal(reply.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}
		if len(reply.Content) == 1 {
			if txt, ok := reply.Content[0].(*sdk.TextContent); ok {
				return []byte(txt.Text)
			}
		}
		t.Fatal("empty MCP result")
		return nil
	}
	checkValidation(call("mce4d_validate_pack", verify))
	mcpToken := checkPreview(call("mce4d_rename_preview", request))
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	checkValidation(run(0, model.ValidateRequest{ResultToken: &mcpToken}, query("validate")))
	if script := os.Getenv("MCE4D_WIN_CODEX_RENAME_PROBE"); script != "" {
		output := os.Getenv("MCE4D_WIN_CODEX_OUTPUT")
		if output == "" {
			output = t.TempDir()
		}
		probe := exec.CommandContext(ctx, os.Getenv("MCE4D_WIN_PROBE_PYTHON"), script,
			"--codex", os.Getenv("MCE4D_WIN_PROBE_CODEX"), "--codex-sha256", os.Getenv("MCE4D_WIN_PROBE_CODEX_SHA256"),
			"--mcp-fixture", executable, "--mcp-sha256", profile.ProviderDigest, "--profile-root", installation,
			"--worktree-id", worktree, "--profile-id", profileID, "--go-root", goRoot, "--output", output)
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		probe.Stdin = bytes.NewReader(raw)
		if output, err := probe.CombinedOutput(); err != nil {
			t.Fatalf("installed Codex rename fixture: %v %s", err, output)
		} else {
			t.Logf("installed Codex rename fixture: %s", output)
		}
	}
	backup := filepath.Join(installation, "control", "worktrees", worktree, "backups", "public-rename")
	run(0, nil, command("mce4d", "store", "backup", "--target", "worktree", "--worktree-id", worktree, "--output", backup))
	run(0, nil, command("mce4d", "store", "restore", "--manifest", filepath.Join(backup, "backup.json"), "--confirm", "--wait-ms", "4000"))
	if out := run(0, nil, command("state-profile", "check", "--root", installation)); !bytes.Contains(out, []byte(`"stage":"READY"`)) {
		t.Fatalf("protected profile lost READY after authorized restore: %s", out)
	}
	for _, token := range []string{cliToken, mcpToken} {
		out := run(3, model.ValidateRequest{ResultToken: &token}, query("validate"))
		if !strings.Contains(string(out), "CURSOR_INVALID") && !strings.Contains(string(out), "STORE_REPLACED") &&
			!strings.Contains(string(out), "SCOPE_CHANGED") {
			t.Fatalf("wrong post-restore token outcome: %s", out)
		}
	}
	if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, source) {
		t.Fatal("public rename changed the synthetic source")
	}
	var prepared struct {
		Installation string `json:"profile_id"`
		Role         string `json:"lsp_sid"`
	}
	metadata, err := os.ReadFile(filepath.Join(installation, "profile.json"))
	if err != nil || json.Unmarshal(metadata, &prepared) != nil || !model.ValidUUID(prepared.Installation) {
		t.Fatalf("reading fixture profile before checking scratch cleanup: %v", err)
	}
	sid, err := windows.StringToSid(prepared.Role)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := runtime.WindowsAppContainerFolder(sid)
	if err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte(prepared.Installation + "/" + worktree))
	paths, err := filepath.Glob(filepath.Join(folder, "mce4d-"+base64.RawURLEncoding.EncodeToString(key[:12]), "lsp-*"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("successful Windows rename left scratch: %v", err)
	}
	t.Log("Windows public CLI and MCP each issued and validated retained rename results, rejected both prior tokens after restore and left source unchanged")
}
