package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type s18LiveScope struct {
	Worktree   string `json:"worktree"`
	Profile    string `json:"profile"`
	Generation string `json:"generation"`
}

// The disposable checkout uses the same synthetic Git index as the existing
// packaged Go fixture. Its working files are the verified S18 source bytes.
func writeS18Worktree(t *testing.T, source, target string) {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !entry.Type().IsRegular() {
			return errors.New("S18 fixture source must be a regular file")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(content)
		destination := filepath.Join(target, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		return os.WriteFile(destination, content, 0600)
	})
	if err != nil || len(files) == 0 {
		t.Fatalf("prepare S18 worktree %s: %v", source, err)
	}
	git := filepath.Join(target, ".git")
	if err := os.MkdirAll(filepath.Join(git, "objects"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"HEAD":  []byte("ref: refs/heads/main\n"),
		"index": trackedIndexFixture(files),
	} {
		if err := os.WriteFile(filepath.Join(git, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func testPackagedWindowsS18(t *testing.T, ctx context.Context, installation, bundle string, targets [3]string,
	register func(string) []byte, create func(string)) {
	t.Helper()
	requestData, err := os.ReadFile(filepath.Join(bundle, "live-requests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var requests []struct {
		Case  string          `json:"case"`
		Repo  string          `json:"repo"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(requestData, &requests); err != nil || len(requests) != 30 {
		t.Fatalf("expected 30 prepared S18 requests: %v", err)
	}

	cli := func(want int, input []byte, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(bundle, "mce4d.exe"), args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1", "GOMAXPROCS=4")
		cmd.Stdin = bytes.NewReader(input)
		out, err := cmd.CombinedOutput()
		exit := 0
		if err != nil {
			var failed *exec.ExitError
			if !errors.As(err, &failed) {
				t.Fatal(err)
			}
			exit = failed.ExitCode()
		}
		if exit != want {
			t.Fatalf("S18 command %v exited %d, expected %d: %s", args, exit, want, out)
		}
		return out
	}

	type capture struct {
		MapsIndexBytes int64                      `json:"maps_index_bytes"`
		Scopes         map[string]s18LiveScope    `json:"scopes"`
		Results        map[string]json.RawMessage `json:"results"`
	}
	out := capture{Scopes: make(map[string]s18LiveScope), Results: make(map[string]json.RawMessage)}
	digests := make(map[string][sha256.Size]byte)
	readFile := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	for i, repo := range []string{"maps", "uuid", "humanize"} {
		writeS18Worktree(t, filepath.Join(bundle, "s18-source", repo), targets[i])
		row := strings.Fields(string(register(targets[i])))
		if len(row) != 5 || !model.ValidUUID(row[2]) || !model.ValidDigest(row[4]) {
			t.Fatalf("missing S18 registration for %s: %q", repo, row)
		}
		worktree, profile := row[2], row[4]
		create(worktree)
		cli(0, nil, "index", "--worktree-id", worktree, "--profile-id", profile)

		owner := filepath.Join(installation, "control", "worktrees", worktree)
		image := filepath.Join(owner, "index-wal", "index.sqlite")
		imageBytes := readFile(image)
		if repo == "maps" {
			out.MapsIndexBytes = int64(len(imageBytes))
			if out.MapsIndexBytes <= 4<<20 {
				t.Fatalf("S18 Maps fixture must exceed 4 MiB; got %d bytes", out.MapsIndexBytes)
			}
		}
		digests[image] = sha256.Sum256(imageBytes)
		head := filepath.Join(owner, "index-control", "wal-head.log")
		digests[head] = sha256.Sum256(readFile(head))

		raw := cli(0, []byte("{}"), "--worktree-id", worktree, "--profile-id", profile, "query", "status", "--input-json", "-")
		var status struct {
			Data  model.StatusData  `json:"data"`
			Scope model.ScopeVector `json:"scope"`
		}
		if err := json.Unmarshal(raw, &status); err != nil || status.Data.IndexState != "published" || len(status.Scope.Members) != 1 {
			t.Fatalf("S18 %s index reopen failed: %s, %v", repo, raw, err)
		}
		member := status.Scope.Members[0]
		if member.WorktreeID != worktree || member.ProfileID != profile || !model.ValidUUID(member.GenerationID) {
			t.Fatalf("S18 %s reopened an unexpected scope: %+v", repo, member)
		}
		out.Scopes[repo] = s18LiveScope{Worktree: worktree, Profile: profile, Generation: member.GenerationID}
		t.Logf("Opened %s index with %d bytes in the published generation", repo, len(imageBytes))
	}

	for _, req := range requests {
		scope, ok := out.Scopes[req.Repo]
		if !ok || req.Case == "" || out.Results[req.Case] != nil {
			t.Fatalf("unexpected or duplicate S18 case: %+v", req)
		}
		result := cli(6, req.Input, "--worktree-id", scope.Worktree, "--profile-id", scope.Profile,
			"query", "locate", "--input-json", "-")
		if !json.Valid(result) {
			t.Fatalf("S18 %s did not return JSON", req.Case)
		}
		out.Results[req.Case] = result
	}
	for path, hash := range digests {
		if sha256.Sum256(readFile(path)) != hash {
			t.Fatalf("S18 queries changed the published image or independent WAL head: %s", path)
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "logs", "s18-live.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
