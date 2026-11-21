package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func prepareSecondWindowsWorktree(t *testing.T, target string) {
	t.Helper()
	git := filepath.Join(target, ".git")
	if err := os.MkdirAll(filepath.Join(git, "objects"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"go.mod": "module example.com/second\n\ngo 1.27.0\n", "second.go": "package second\nfunc SecondMarker() {}\n"}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(target, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte("ref: refs/heads/main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "index"), trackedIndexFixture(files), 0600); err != nil {
		t.Fatal(err)
	}
}

func testWindowsScopedMaintenance(t *testing.T, ctx context.Context, executable, root, target, a, aProfile, b, bProfile string) {
	t.Helper()
	command := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+root, "MCE4D_WINDOWS_ROLE_IPC=1")
		return cmd
	}
	run := func(want int, cmd *exec.Cmd) []byte {
		t.Helper()
		out, err := cmd.CombinedOutput()
		var failed *exec.ExitError
		if want == 0 && err == nil || want != 0 && errors.As(err, &failed) && failed.ExitCode() == want {
			return out
		}
		t.Fatalf("scoped command %q expected exit %d: %v %s", cmd.Args[1:], want, err, out)
		return nil
	}
	backup := filepath.Join(root, "control", "worktrees", a, "backups", "scoped-maintenance")
	run(0, command("store", "backup", "--target", "worktree", "--worktree-id", a, "--output", backup))
	restore := func(wait string) *exec.Cmd {
		return command("store", "restore", "--manifest", filepath.Join(backup, "backup.json"), "--confirm", "--wait-ms", wait)
	}
	open := func() *sdk.ClientSession {
		t.Helper()
		client := sdk.NewClient(&sdk.Implementation{Name: "mce4d_windows_scoped_maintenance", Version: "1"}, nil)
		session, err := client.Connect(ctx, &sdk.CommandTransport{
			Command: command("mcp", "--worktree-id", a, "--profile-id", aProfile), TerminateDuration: 3 * time.Second,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	requireReader := func(session *sdk.ClientSession, symbol string) {
		t.Helper()
		reply, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "mce4d_locate",
			Arguments: map[string]any{"mode": "symbol", "query": symbol, "limit": 4}})
		if err != nil || reply.IsError || len(reply.Content) != 1 {
			t.Fatalf("worktree A reader unavailable: %v", err)
		}
		if result, ok := reply.Content[0].(*sdk.TextContent); !ok || !strings.Contains(result.Text, `"name":"`+symbol+`"`) {
			t.Fatalf("worktree A did not return the published symbol %q", symbol)
		}
	}
	first, second := open(), open()
	requireReader(first, "PublishMarker")
	requireReader(second, "PublishMarker")
	var restoreOutput bytes.Buffer
	waiting := restore("5500")
	waiting.Stdout, waiting.Stderr = &restoreOutput, &restoreOutput
	if err := waiting.Start(); err != nil {
		t.Fatal(err)
	}
	restoreDone := make(chan error, 1)
	go func() {
		restoreDone <- waiting.Wait()
		close(restoreDone)
	}()
	defer func() {
		_ = waiting.Process.Kill()
		<-restoreDone
	}()
	if out := run(0, command("index", "--worktree-id", b, "--profile-id", bProfile)); !bytes.Contains(out, []byte("indexed 2 files")) {
		t.Fatalf("worktree B failed to index while A had two readers and a waiting restore: %s", out)
	}
	select {
	case err := <-restoreDone:
		t.Fatalf("worktree B did not publish before the wait on A ended: %v %s", err, &restoreOutput)
	default:
	}
	requireReader(second, "PublishMarker")
	var failed *exec.ExitError
	if err := <-restoreDone; !errors.As(err, &failed) || failed.ExitCode() != 4 || !strings.Contains(restoreOutput.String(), "STORE_MAINTENANCE") {
		t.Fatalf("restore passed two active reader leases: %v %s", err, &restoreOutput)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if out := run(4, restore("200")); !bytes.Contains(out, []byte("STORE_MAINTENANCE")) {
		t.Fatalf("restore passed the remaining reader lease: %s", out)
	}
	requireReader(second, "PublishMarker")
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if out := run(0, restore("2500")); !bytes.Contains(out, []byte(`"outcome":"restored"`)) {
		t.Fatalf("restore did not proceed after the last reader closed: %s", out)
	}
	t.Log("C171 Windows: two MCP process leases block A restore, B publishes an independent generation, and A restore succeeds after the last reader closes")

	reader := open()
	requireReader(reader, "PublishMarker")
	path := filepath.Join(target, "marker.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\nfunc LaterMarker() {}\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if out := run(0, command("index", "--worktree-id", a, "--profile-id", aProfile)); !bytes.Contains(out, []byte("indexed ")) {
		t.Fatalf("same-worktree index failed with a live MCP reader: %s", out)
	}
	requireReader(reader, "LaterMarker")
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	t.Log("C021 Windows: A published an updated generation with its MCP reader still connected; the same session queried the new symbol")
}
