package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func startPackagedWindowsExplorer(t *testing.T, parent context.Context, bin, installation, worktree, profile string) (*url.URL, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, filepath.Join(bin, "mce4d.exe"), "explorer", "--worktree-id", worktree, "--profile-id", profile)
	cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1")
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	console := bufio.NewScanner(io.LimitReader(stdout, 512))
	if !console.Scan() || !strings.HasPrefix(console.Text(), "Explorer ") {
		cancel()
		// Synchronize with the stderr copy before reading diagnostics.
		_ = cmd.Wait()
		t.Fatalf("Explorer did not start: %s", diagnostics.String())
	}
	u, err := url.Parse(strings.TrimPrefix(console.Text(), "Explorer "))
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" {
		t.Fatal("Explorer startup URL is outside IPv4 loopback")
	}
	if !console.Scan() || !strings.HasPrefix(console.Text(), "Bootstrap code: ") {
		t.Fatal("Explorer did not issue a bootstrap code")
	}
	return u, strings.TrimPrefix(console.Text(), "Bootstrap code: ")
}

func testPackagedWindowsExplorer(t *testing.T, parent context.Context, bin, installation string, scope model.ScopeVector, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	member := scope.Members[0]
	u, code := startPackagedWindowsExplorer(t, ctx, bin, installation, member.WorktreeID, member.ProfileID)
	origin := u.Scheme + "://" + u.Host
	var bearer string
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	request := func(method, path, host, requestOrigin string, value any, intent bool, want int) []byte {
		t.Helper()
		var body io.Reader
		if value != nil {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, origin+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		req.Header.Set("Origin", requestOrigin)
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		if intent {
			req.Header.Set("X-MCE4D-Intent", "explorer")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil || resp.StatusCode != want || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: got %d, want %d, read error %v", path, resp.StatusCode, want, err)
		}
		return raw
	}
	const api = "/api/explorer/v1"
	request("GET", "/", u.Host, origin, nil, false, 200)
	request("GET", api+"/workspace", u.Host, origin, nil, false, 401)
	bootstrap := map[string]string{"code": code}
	request("POST", api+"/session/bootstrap", u.Host, origin, bootstrap, false, 403)
	var session struct {
		Data model.BootstrapData `json:"data"`
	}
	if err := json.Unmarshal(request("POST", api+"/session/bootstrap", u.Host, origin, bootstrap, true, 200), &session); err != nil || session.Data.Bearer == "" {
		t.Fatal("Explorer did not return a bearer")
	}
	if !slices.Contains(session.Data.Capabilities, "context_pack") {
		t.Fatal("Windows Explorer did not advertise selection context")
	}
	bearer = session.Data.Bearer
	request("GET", api+"/workspace", "localhost:"+u.Port(), origin, nil, false, 403)
	request("GET", api+"/workspace", u.Host, "null", nil, false, 403)
	workspace := request("GET", api+"/workspace?verify=none", u.Host, origin, nil, false, 200)
	if !bytes.Contains(workspace, []byte(member.GenerationID)) || !bytes.Contains(workspace, []byte(`"index_state":"published"`)) {
		t.Fatal("Explorer did not resolve the published SQLite scope")
	}
	graph := request("POST", api+"/graph/query", u.Host, origin, map[string]any{
		"root": map[string]string{"kind": "workspace"}, "scope": map[string]any{"pinned_vector": scope},
		"node_limit": 100, "edge_limit": 100,
	}, true, 200)
	if !bytes.Contains(graph, []byte("marker.go")) {
		t.Fatal("Explorer did not return the fixture's indexed file")
	}
	contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
	raw := request("POST", api+"/source/read", u.Host, origin, map[string]any{"source_ref": map[string]any{
		"worktree_id": member.WorktreeID, "profile_id": member.ProfileID, "generation_id": member.GenerationID,
		"path": "marker.go", "content_hash": contentHash,
		"range": map[string]int{"start_byte": 0, "end_byte": len(source)},
	}}, true, 200)
	var captured struct {
		Data struct{ Content string } `json:"data"`
	}
	if err := json.Unmarshal(raw, &captured); err != nil || captured.Data.Content != source {
		t.Fatal("Explorer did not return the exact captured bytes")
	}
	offset := strings.Index(source, "PublishMarker")
	if offset < 0 {
		t.Fatal("fixture is missing PublishMarker")
	}
	packed := request("POST", api+"/context/pack", u.Host, origin, map[string]any{
		"scope": map[string]any{"pinned_vector": scope},
		"view":  "symbol", "task_kind": "explore", "freshness_mode": "analysis_snapshot", "max_output_bytes": 24576,
		"selector": map[string]any{
			"kind": "source_position", "path": "marker.go", "content_hash": contentHash, "byte_offset": offset,
		},
	}, true, 200)
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	pack, err := registry.DecodeResponse(contracts.HTTP, "POST /context/pack", packed, 24576, 1)
	if err != nil || pack.Scope == nil || pack.Scope.VectorDigest != scope.VectorDigest {
		t.Fatalf("Explorer did not return context in the pinned scope: %v", err)
	}
	data, ok := pack.Data.(model.PackData)
	if !ok || data.Operation != "context_pack" || len(data.Items) == 0 || data.EditPreconditionValidated {
		t.Fatal("Explorer did not return a read-only context pack")
	}
	t.Log("Windows packaged Explorer authenticated loopback and read the graph, exact captured source, and selection context in the published scope")
}

// The browser stays connected while native Watch publishes all fixture generations.
func startWindowsExplorerWatchBrowser(t *testing.T, ctx context.Context, bin, installation, worktree, profile, driver string) func(string, string, []windowsLanguageSource, []windowsLanguageSource) {
	t.Helper()
	node, browser := os.Getenv("MCE4D_WIN_EXPLORER_NODE"), os.Getenv("MCE4D_WIN_EXPLORER_BROWSER")
	if !filepath.IsAbs(driver) || !filepath.IsAbs(node) || !filepath.IsAbs(browser) {
		t.Fatal("Explorer Watch requires absolute driver, Node and browser paths")
	}
	u, code := startPackagedWindowsExplorer(t, ctx, bin, installation, worktree, profile)
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, node, driver, browser, os.Getenv("MCE4D_WIN_EXPLORER_GO_COVERAGE"))
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var waited bool
	t.Cleanup(func() {
		cancel()
		if !waited {
			_ = cmd.Wait()
		}
	})
	scanner, encoder := bufio.NewScanner(output), json.NewEncoder(input)
	send := func(value any) {
		t.Helper()
		if err := encoder.Encode(value); err != nil {
			t.Fatalf("send to Explorer Watch browser: %v", err)
		}
	}
	send(map[string]string{"url": u.String(), "code": code, "worktree": worktree, "profile": profile})
	type source struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Body string `json:"body"`
	}
	describe := func(files []windowsLanguageSource) []source {
		out := make([]source, 0, len(files))
		for _, file := range files {
			out = append(out, source{file.path, file.name, file.body})
		}
		return out
	}
	return func(phase, generation string, present, absent []windowsLanguageSource) {
		t.Helper()
		send(map[string]any{
			"phase": phase, "generation": generation, "present": describe(present), "absent": describe(absent),
		})
		if !scanner.Scan() {
			waited = true
			t.Fatalf("Explorer Watch browser stopped at %s: %v, scan error %v", phase, cmd.Wait(), scanner.Err())
		}
		sourceState := "retained-source"
		if phase == "initial" || phase == "delete" {
			sourceState = "cleared-source"
		}
		if scanner.Text() != phase+" "+generation+" "+sourceState {
			t.Fatalf("Explorer Watch browser did not confirm %s at generation %s", phase, generation)
		}
		if phase == "delete" {
			if err := input.Close(); err != nil {
				t.Fatal(err)
			}
			waited = true
			if err := cmd.Wait(); err != nil {
				t.Fatalf("Explorer Watch browser exit: %v", err)
			}
		}
		t.Logf("Windows Explorer %s: automatic SQLite graph/source at %s (%s)", phase, generation, sourceState)
	}
}
