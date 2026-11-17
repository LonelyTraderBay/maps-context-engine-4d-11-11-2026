//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run the actual main in a child because it terminates its process. HOME and
// XDG_STATE_HOME point exclusively at this test's private fixture.
func TestMainReadOnlyDefaultConfig(t *testing.T) {
	if os.Getenv("MCE4D_MAIN_CONFIG_TEST_CHILD") == "1" {
		action := os.Getenv("MCE4D_MAIN_CONFIG_TEST_ACTION")
		if action == "doctor" {
			os.Args = []string{"mce4d", "doctor", "--json"}
		} else {
			os.Args = []string{"mce4d", "config", action, "--input-json", "-"}
		}
		main()
		return
	}
	root := t.TempDir()
	home, xdg := filepath.Join(root, "home"), filepath.Join(root, "state")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(xdg, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(xdg, "mce4d")
	run := func(action, input string, wantExit int) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainReadOnlyDefaultConfig$")
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_STATE_HOME="+xdg, "MCE4D_MAIN_CONFIG_TEST_CHILD=1", "MCE4D_MAIN_CONFIG_TEST_ACTION="+action)
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil || stderr.Len() != 0 || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != wantExit || wantExit == 0 && err != nil {
			t.Fatalf("main %s: exit %v, error %v, stderr %q", action, cmd.ProcessState, err, stderr.String())
		}
		var response map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
			t.Fatalf("main %s returned non-JSON: %v", action, err)
		}
		return response
	}
	if r := run("show", `{"action":"show"}`, 0); r["status"] != "ok" {
		t.Fatalf("missing state did not use defaults: %+v", r)
	}
	doctor := run("doctor", "", 0)
	checks, ok := doctor["checks"].(map[string]any)
	if !ok || checks["configuration"] != "available_read_only" || checks["filesystem"] != "not_checked" {
		t.Fatalf("main doctor did not report its observations: %+v", doctor)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("main created state: %v", err)
	}
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	file, data := filepath.Join(state, "runtime.json"), []byte(`{"schema_version":1,"core":{"max_file_bytes":32100}}`)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	shown := run("show", `{"action":"show"}`, 0)
	config, ok := shown["data"].(map[string]any)
	if !ok {
		t.Fatal("main omitted config data")
	}
	values, ok := config["config"].(map[string]any)
	if !ok {
		t.Fatal("main omitted config")
	}
	core, ok := values["core"].(map[string]any)
	if !ok || core["max_file_bytes"] != float64(32100) {
		t.Fatalf("main did not read config: %+v", values)
	}
	blocked := run("set", `{"action":"set","config":{"schema_version":1},"expected_digest":null}`, 3)
	info, ok := blocked["error"].(map[string]any)
	if !ok || info["code"] != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("main accepted config set: %+v", blocked)
	}
	if after, err := os.ReadFile(file); err != nil || !bytes.Equal(after, data) {
		t.Fatal("main changed the config")
	}
	if files, err := os.ReadDir(state); err != nil || len(files) != 1 || files[0].Name() != "runtime.json" {
		t.Fatal("main created an unexpected state file")
	}
	if err := os.WriteFile(filepath.Join(state, ".runtime.json.pending"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	doctor = run("doctor", "", 0)
	checks, ok = doctor["checks"].(map[string]any)
	if !ok || checks["configuration"] != "recovery_required" {
		t.Fatalf("main doctor did not report pending recovery: %+v", doctor)
	}
}
