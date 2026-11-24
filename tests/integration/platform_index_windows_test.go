package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func testPackagedWindowsIndex(t *testing.T, ctx context.Context, bin, installation, target, worktree, profile string) {
	t.Helper()
	cli := func(want int, input string, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "mce4d.exe"), args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1")
		cmd.Stdin = strings.NewReader(input)
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
			t.Fatalf("%s exited %d, expected %d: %s", args[0], exit, want, out)
		}
		return out
	}
	track := []string{"index", "--worktree-id", worktree, "--profile-id", profile}
	if out := cli(0, "", track...); !bytes.Contains(out, []byte("indexed 0 files")) {
		t.Fatalf("empty unborn repository: %s", out)
	}
	const source = "package fixture\nfunc PublishMarker() string { return \"bluefixtureterm\" }\n"
	tracked := map[string]string{"go.mod": "module example.com/fixture\n\ngo 1.27.0\n", "marker.go": source,
		"marker_extra.go": "package fixture\nfunc ExtraMarker() {}\n",
		".env":            "FAKE_PRIVATE_VALUE=shouldnotappear"}
	for name, data := range tracked {
		if err := os.WriteFile(filepath.Join(target, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	untracked := map[string]string{
		".gitignore":             "untracked.go\ncontained/\n!contained/reinclude.go\nmarker_extra.go\n",
		".git/info/exclude":      "localonly.go\n",
		".git/config":            "[core]\nrepositoryformatversion=0\nbare=false\nignorecase=true\nautocrlf=input\n[remote \"origin\"]\nurl=/synthetic\nfetch=+refs/heads/*:refs/remotes/origin/*\n",
		"untracked.go":           "package fixture\n// untrackedfixtureterm\n",
		"untracked_allowed.go":   "package fixture\nfunc AllowedMarker() string { return \"allowedfixtureterm\" }\n",
		"localonly.go":           "package fixture\n// infofixtureterm\n",
		"globalonly.go":          "package fixture\n// globalfixtureterm\n",
		"contained/reinclude.go": "package fixture\n// blockedparentfixtureterm\n",
		"notes/.gitignore":       "off.go\n",
		"notes/OFF.go":           "package notes\n// nestedignorefixtureterm\n",
		"foreign/.git/HEAD":      "ref: refs/heads/main\n",
		"foreign/hidden.go":      "package foreign\n// nestedrepo fixtureprivateboundary\n",
	}
	for name, contents := range untracked {
		path := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	indexFile := filepath.Join(target, ".git", "index")
	rawIndex := trackedIndexFixture(tracked)
	if err := os.WriteFile(indexFile, rawIndex, 0600); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(installation, "control", "worktrees", worktree, "index-control", "wal-head.log")
	prior, err := os.ReadFile(head)
	if err != nil {
		t.Fatal(err)
	}
	out := cli(0, "", track...)
	if !bytes.Contains(out, []byte("indexed ")) || !bytes.Contains(out, []byte("syntax partial")) {
		t.Fatalf("partial Windows publication: %s", out)
	}

	bind := []string{"--worktree-id", worktree, "--profile-id", profile, "query"}
	query := func(action, raw string) []byte {
		t.Helper()
		return cli(0, raw, append(append([]string(nil), bind...), action, "--input-json", "-")...)
	}
	var status struct {
		Status string            `json:"status"`
		Data   model.StatusData  `json:"data"`
		Scope  model.ScopeVector `json:"scope"`
	}
	result := query("status", "{}")
	if err := json.Unmarshal(result, &status); err != nil || status.Status != "ok" || status.Data.IndexState != "published" ||
		len(status.Scope.Members) != 1 || status.Scope.Members[0].WorktreeID != worktree || status.Scope.Members[0].ProfileID != profile {
		t.Fatalf("published Windows scope: %s, %v", result, err)
	}
	if os.Getenv("MCE4D_WIN_EXPLORER_FIXTURE") == "1" {
		testPackagedWindowsExplorer(t, ctx, bin, installation, status.Scope, source)
	}
	verification := `{"verify":"workspace"}`
	if checked := query("status", verification); !bytes.Contains(checked, []byte(`"status":"observed_current"`)) {
		t.Fatalf("fresh Windows capture was not observed current: %s", checked)
	}
	changedPath := filepath.Join(target, "untracked_allowed.go")
	unchangedText := untracked["untracked_allowed.go"]
	if err := os.WriteFile(changedPath, []byte(unchangedText+"// changed idle worktree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if raw := cli(6, verification, append(append([]string(nil), bind...), "status", "--input-json", "-")...); !bytes.Contains(raw, []byte("WORKSPACE_CHANGED")) {
		t.Fatalf("Windows workspace check missed an untracked edit: %s", raw)
	}
	if err := os.WriteFile(changedPath, []byte(unchangedText), 0600); err != nil {
		t.Fatal(err)
	}
	locate := query("locate", `{"mode":"symbol","query":"PublishMarker","limit":4}`)
	if !bytes.Contains(locate, []byte("PublishMarker")) || !bytes.Contains(locate, []byte("marker.go")) {
		t.Fatalf("published Go syntax missing: %s", locate)
	}
	for _, term := range []string{"bluefixtureterm", "allowedfixtureterm", "untrackedfixtureterm", "shouldnotappear",
		"infofixtureterm", "globalfixtureterm", "blockedparentfixtureterm", "nestedignorefixtureterm", "fixtureprivateboundary"} {
		var found struct {
			Data struct {
				Candidates []json.RawMessage `json:"candidates"`
			} `json:"data"`
		}
		raw := query("locate", fmt.Sprintf(`{"mode":"text","query":%q,"limit":4}`, term))
		want := term == "bluefixtureterm" || term == "allowedfixtureterm"
		if err := json.Unmarshal(raw, &found); err != nil || (len(found.Data.Candidates) > 0) != want {
			t.Fatalf("unexpected Windows inventory search for %s: %s, %v", term, raw, err)
		}
	}
	if out := cli(0, "", track...); !bytes.Contains(out, []byte("unchanged ")) ||
		!bytes.Contains(out, []byte(status.Scope.Members[0].GenerationID)) {
		t.Fatalf("same tracked inventory changed generation: %s", out)
	}
	advanced, err := os.ReadFile(head)
	if err != nil || len(advanced) != len(prior)+4*192 || !bytes.Equal(prior, advanced[:len(prior)]) {
		t.Fatalf("independent receipt head did not acknowledge both operations: %v", err)
	}
	if current, err := os.ReadFile(indexFile); err != nil || !bytes.Equal(current, rawIndex) {
		t.Fatalf("index command changed the fixture Git index: %v", err)
	}
	if current, err := os.ReadFile(filepath.Join(target, "marker.go")); err != nil || string(current) != source {
		t.Fatalf("index command changed the source file: %v", err)
	}
	t.Log("packaged Windows index published tracked and allowed untracked files, respected directory, metadata and global ignores, skipped private and nested repositories, and reused its unchanged generation")

	first := cli(6, `{"mode":"file","query":"marker","limit":1}`,
		append(append([]string(nil), bind...), "locate", "--input-json", "-")...)
	var page struct {
		Pagination model.Pagination `json:"pagination"`
	}
	if err := json.Unmarshal(first, &page); err != nil || page.Pagination.NextCursor == nil {
		t.Fatalf("expected real CLI cursor: %s, %v", first, err)
	}
	request := fmt.Sprintf(`{"mode":"file","query":"marker","limit":1,"cursor":%q}`, *page.Pagination.NextCursor)
	if next := query("locate", request); !bytes.Contains(next, []byte(`"status":"ok"`)) {
		t.Fatalf("signed cursor did not survive the first CLI restart: %s", next)
	}
	control := filepath.Join(installation, "control")
	key := filepath.Join(control, "cursor.key")
	beforeKey, err := os.ReadFile(key)
	if err != nil || len(beforeKey) != 32 {
		t.Fatalf("protected cursor key: %v", err)
	}
	defer clear(beforeKey)
	resultToken := scopedWindowsToken(t, ctx, installation, worktree, profile, "issue", "")
	scopedWindowsToken(t, ctx, installation, worktree, profile, "validate", resultToken)
	backup := filepath.Join(control, "worktrees", worktree, "backups", "published-index")
	cli(0, "", "store", "backup", "--target", "worktree", "--worktree-id", worktree, "--output", backup)
	cli(0, "", "store", "restore", "--manifest", filepath.Join(backup, "backup.json"), "--confirm", "--wait-ms", "4000")
	afterKey, err := os.ReadFile(key)
	defer clear(afterKey)
	if err != nil || !bytes.Equal(beforeKey, afterKey) {
		t.Fatalf("restore changed the protected cursor key: %v", err)
	}
	failed := cli(3, request, append(append([]string(nil), bind...), "locate", "--input-json", "-")...)
	if !bytes.Contains(failed, []byte("CURSOR_INVALID")) && !bytes.Contains(failed, []byte("STORE_REPLACED")) {
		t.Fatalf("stale result was not rejected for its scope: %s", failed)
	}
	result = query("status", "{}")
	var restored struct {
		Scope model.ScopeVector `json:"scope"`
	}
	if err := json.Unmarshal(result, &restored); err != nil || len(restored.Scope.Members) != 1 ||
		restored.Scope.Members[0].GenerationID != status.Scope.Members[0].GenerationID ||
		restored.Scope.Members[0].IndexIncarnation == status.Scope.Members[0].IndexIncarnation ||
		restored.Scope.Members[0].BindingRevision != status.Scope.Members[0].BindingRevision {
		t.Fatalf("restore failed to preserve generation and rotate its store identity: %s, %v", result, err)
	}
	t.Log("real CLI cursor survived an ordinary restart but was refused after restoring its published index with the same key and generation")
	scopedWindowsToken(t, ctx, installation, worktree, profile, "reject", resultToken)
	t.Log("signed rename spool opened across real NTFS processes and rejected the earlier result token after authenticated worktree restore")
	if driver := os.Getenv("MCE4D_WIN_EXPLORER_DRIVER"); driver != "" {
		t.Run("GoExplorerAutoWatch", func(t *testing.T) {
			member := restored.Scope.Members[0]
			if goRoot := os.Getenv("MCE4D_WIN_GO_TYPED_WATCH_GOROOT"); goRoot != "" {
				member = registerWindowsGoTypedWatchProfile(t, ctx, bin, installation, worktree, profile, goRoot)
			}
			testPackagedWindowsGoExplorerWatch(t, ctx, bin, installation, target, member,
				windowsLanguageSource{"marker.go", "PublishMarker", source}, driver)
		})
	}
	testPackagedWindowsWatch(t, ctx, bin, installation, worktree, profile, changedPath, unchangedText)
	if raw := query("locate", `{"mode":"symbol","query":"WatchMarker","limit":4}`); !bytes.Contains(raw, []byte("WatchMarker")) {
		t.Fatalf("last watch generation did not survive process exit: %s", raw)
	}
	if out := cli(0, "", track...); !bytes.Contains(out, []byte("unchanged")) {
		t.Fatalf("writer did not release its lease after watch exited: %s", out)
	}
	t.Log("foreground watch published an untracked edit and the next CLI process reused its generation")
}

func registerWindowsGoTypedWatchProfile(t *testing.T, ctx context.Context, bin, installation, worktree, syntaxProfile, goRoot string) model.ScopeMember {
	t.Helper()
	proof, err := runtime.InspectGoToolchain(ctx, goRoot)
	if err != nil {
		t.Fatal(err)
	}
	toolchain := proof.Digest()
	if err := proof.Close(); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, "mce4d.exe")
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	profile := &model.GoProfile{
		SchemaVersion: 1, Language: "go", Goos: "windows", Goarch: "amd64", BuildTags: []string{},
		ModuleRoots: []string{"."}, ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{},
		ToolchainDigest: toolchain, ProviderDigest: fmt.Sprintf("%x", sha256.Sum256(image)),
	}
	id, err := model.ProfileDigest(profile, 32768)
	if err != nil || id == syntaxProfile {
		t.Fatalf("invalid typed profile identity: %v", err)
	}
	run := func(input []byte, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1")
		cmd.Stdin = bytes.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("typed profile command %q: %v: %s", args[0], err, out)
		}
		return out
	}
	raw, err := json.Marshal(model.ProfileAdminRequest{WorktreeID: worktree, ProfileName: "go-explorer-typed", Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct{ Data model.ProfileAdminData }
	if out := run(raw, "profile", "set", "--worktree-id", worktree, "--input-json", "-"); json.Unmarshal(out, &registered) != nil ||
		registered.Data.ProfileID != id || registered.Data.WorktreeID != worktree {
		t.Fatalf("typed profile registration disagreed with the digest: %s", out)
	}
	flags := []string{"--worktree-id", worktree, "--profile-id", id}
	if out := run(nil, append([]string{"index"}, flags...)...); !bytes.Contains(out, []byte("syntax partial")) {
		t.Fatalf("expected the documented Windows Go fallback: %s", out)
	}
	var status struct {
		Data  model.StatusData
		Scope model.ScopeVector
	}
	out := run([]byte("{}"), append(flags, "query", "status", "--input-json", "-")...)
	if json.Unmarshal(out, &status) != nil || status.Data.IndexState != "published" || len(status.Scope.Members) != 1 ||
		status.Scope.Members[0].WorktreeID != worktree || status.Scope.Members[0].ProfileID != id ||
		!model.ValidUUID(status.Scope.Members[0].GenerationID) {
		t.Fatalf("typed profile did not reopen its published generation: %s", out)
	}
	t.Logf("Go profile typed_when_available %s, toolchain %s, effective Windows index syntax partial", id, toolchain)
	return status.Scope.Members[0]
}

func testPackagedWindowsGoExplorerWatch(t *testing.T, parent context.Context, bin, installation, target string,
	member model.ScopeMember, baseline windowsLanguageSource, driver string) {
	t.Helper()
	ctx, stop := context.WithTimeout(parent, 90*time.Second)
	defer stop()
	flags := []string{"--worktree-id", member.WorktreeID, "--profile-id", member.ProfileID}
	command := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "mce4d.exe"), args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1")
		return cmd
	}
	verify := startWindowsExplorerWatchBrowser(t, ctx, bin, installation, member.WorktreeID, member.ProfileID, driver)
	generation := member.GenerationID
	verify("initial", generation, []windowsLanguageSource{baseline}, nil)
	before := powerShellFixtureHashes(t, target)

	watch := command(append([]string{"watch"}, flags...)...)
	var stdout bytes.Buffer
	watch.Stdout = &stdout
	stderr, err := watch.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop()
		_ = watch.Wait()
		if stdout.Len() != 0 {
			t.Error("Watch wrote progress to stdout")
		}
	})
	lines := bufio.NewScanner(stderr)
	await := func(action string) {
		t.Helper()
		for lines.Scan() {
			fields := strings.Fields(lines.Text())
			if len(fields) != 8 || fields[0] != "mce4d:" || fields[4] != "generation" ||
				fields[6] != "syntax" || fields[7] != "partial" {
				t.Fatalf("unexpected Go Watch result: %s", lines.Text())
			}
			id := strings.TrimSuffix(fields[5], ";")
			if fields[1] == "unchanged" && id == generation {
				if action == "unchanged" {
					return
				}
				continue
			}
			if fields[1] == action && action == "indexed" && id != generation && model.ValidUUID(id) {
				generation = id
				return
			}
			t.Fatalf("unexpected Go Watch generation while awaiting %s: %s", action, lines.Text())
		}
		t.Fatalf("Go Watch stopped while awaiting %s: %v", action, lines.Err())
	}
	check := func(phase string, present, absent []windowsLanguageSource) {
		t.Helper()
		await("indexed")
		cmd := command(append(append([]string{}, flags...), "query", "status", "--input-json", "-")...)
		cmd.Stdin = strings.NewReader("{}")
		raw, err := cmd.Output()
		var status struct {
			Data  model.StatusData
			Scope model.ScopeVector
		}
		if err != nil || json.Unmarshal(raw, &status) != nil || status.Data.IndexState != "published" ||
			len(status.Scope.Members) != 1 || status.Scope.Members[0].WorktreeID != member.WorktreeID ||
			status.Scope.Members[0].ProfileID != member.ProfileID || status.Scope.Members[0].GenerationID != generation {
			t.Fatalf("CLI status differs from Go Watch generation %s: %v", generation, err)
		}
		verify(phase, generation, append([]windowsLanguageSource{baseline}, present...), absent)
	}
	const path, name = "watch_explorer.go", "ExplorerWatchMarker"
	const prefix = "package fixture\r\n// 🗺 café\r\nfunc " + name + "() string { return "
	first := windowsLanguageSource{path, name, prefix + "\"A\" }\r\n"}
	edited := windowsLanguageSource{path, name, prefix + "\"B\" }\r\n"}
	file := filepath.Join(target, path)
	await("unchanged")
	if err := os.WriteFile(file, []byte(first.body), 0600); err != nil {
		t.Fatal(err)
	}
	check("add", []windowsLanguageSource{first}, nil)
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(edited.body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if changed, err := os.Stat(file); err != nil || changed.Size() != info.Size() || !changed.ModTime().Equal(info.ModTime()) {
		t.Fatalf("Go edit must preserve size and mtime: %v", err)
	}
	check("edit", []windowsLanguageSource{edited}, nil)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	check("delete", nil, []windowsLanguageSource{edited})
	if !maps.Equal(before, powerShellFixtureHashes(t, target)) {
		t.Fatal("Go Watch altered the fixture source or Git metadata")
	}
	t.Log("Go Explorer fixture observed add/edit/delete, preserved file size and mtime on edit, and restored source/Git hashes")
}

func testPackagedWindowsWatch(t *testing.T, ctx context.Context, bin, installation, worktree, profile, path, oldText string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 22*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(bin, "mce4d.exe"), "watch", "--worktree-id", worktree, "--profile-id", profile)
	cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC=1")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := cmd.Wait(); err == nil {
			t.Error("foreground watch exited before it was stopped")
		}
	}()
	lines := bufio.NewScanner(stderr)
	if !lines.Scan() || !strings.Contains(lines.Text(), "unchanged ") {
		t.Fatalf("watch did not begin with the restored generation: %s, %v", lines.Text(), lines.Err())
	}
	if err := os.WriteFile(path, []byte(oldText+"func WatchMarker() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for lines.Scan() {
		if strings.Contains(lines.Text(), "indexed ") {
			return
		}
		if !strings.Contains(lines.Text(), "unchanged ") {
			t.Fatalf("unexpected watch outcome: %s", lines.Text())
		}
	}
	t.Fatalf("watch did not publish the untracked edit: %v", lines.Err())
}

// Generate a standard v2 locator table for disposable tracked paths. The
// product parses this through its ordinary bounded Git index decoder.
func trackedIndexFixture(files map[string]string) []byte {
	list := make([]string, 0, len(files))
	for name := range files {
		list = append(list, name)
	}
	sort.Strings(list)
	data := []byte{'D', 'I', 'R', 'C', 0, 0, 0, 2}
	data = binary.BigEndian.AppendUint32(data, uint32(len(list)))
	for _, path := range list {
		start := len(data)
		var stat [40]byte
		binary.BigEndian.PutUint32(stat[24:28], 0100644)
		binary.BigEndian.PutUint32(stat[36:40], uint32(len(files[path])))
		data = append(data, stat[:]...)
		hash := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(files[path]), files[path])))
		data = append(data, hash[:]...)
		data = binary.BigEndian.AppendUint16(data, uint16(len(path)))
		data = append(data, path...)
		data = append(data, 0)
		for (len(data)-start)%8 != 0 {
			data = append(data, 0)
		}
	}
	hash := sha1.Sum(data)
	return append(data, hash[:]...)
}
