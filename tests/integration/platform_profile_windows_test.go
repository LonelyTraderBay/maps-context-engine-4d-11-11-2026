package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/windows"
)

func TestWindowsProfileRoleProcess(t *testing.T) {
	if os.Getenv("MCE4D_PROFILE_ROLE_FIXTURE") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "waiting")
	var one [1]byte
	if _, err := io.ReadFull(os.Stdin, one[:]); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestWindowsPackagedProfileRoleVerification(t *testing.T) {
	bin := os.Getenv("MCE4D_WIN_PACKAGE_FIXTURE")
	if bin == "" {
		t.Skip("requires the packaged state-profile executable")
	}
	if !filepath.IsAbs(bin) || filepath.Clean(bin) != bin {
		t.Fatal("packaged test binaries require an explicit absolute directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 230*time.Second)
	defer cancel()
	engine := newAppContainer(t, "ProfileEngine")
	client := newAppContainer(t, "ProfileClient")
	materials := fixtureDirectory(t, filepath.Join(t.TempDir(), "materials"), readableBy(engine)+readableBy(client))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	helper := filepath.Join(materials, "role-probe.exe")
	file, err := os.OpenFile(helper, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	const limit = 64 << 20
	n, copyErr := io.Copy(file, io.LimitReader(source, limit+1))
	if err := errors.Join(copyErr, file.Close()); err != nil || n < 1 || n > limit {
		t.Fatalf("copying the pinned role process: %d bytes, %v", n, err)
	}
	launch := func(role appContainer) (*appContainerChild, string) {
		t.Helper()
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer read.Close()
		t.Cleanup(func() { write.Close() })
		child := startWindowsFixtureJob(t, role, helper, materials, []string{"-test.run=^TestWindowsProfileRoleProcess$"},
			[]string{"MCE4D_PROFILE_ROLE_FIXTURE=1"}, read)
		ready := make(chan error, 1)
		go func() {
			line, err := bufio.NewReader(io.LimitReader(child.output, 4096)).ReadString('\n')
			if err == nil && line != "waiting\n" {
				err = fmt.Errorf("unexpected role process startup")
			}
			ready <- err
		}()
		select {
		case err := <-ready:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		pid, err := windows.GetProcessId(child.process)
		if err != nil || pid == 0 {
			t.Fatalf("getting live fixture PID: %v", err)
		}
		return child, strconv.FormatUint(uint64(pid), 10)
	}
	engineProcess, enginePID := launch(engine)
	_, clientPID := launch(client)

	parent := filepath.Join(t.TempDir(), "profile-parent")
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
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}

	run := func(want int, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "state-profile.exe"), args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != want || want != 0 && len(out) != 0 {
			t.Fatalf("packaged role command exited %d, expected %d: %s", code, want, stderr.String())
		}
		return out
	}
	root := filepath.Join(parent, "new-profile")
	target := t.TempDir()
	secondTarget := t.TempDir()
	ignore := filepath.Join(t.TempDir(), "git.ignore")
	if err := os.WriteFile(ignore, []byte("globalonly.go\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"prepare", "--root", root, "--target-root", target, "--engine-sid", engine.sid.String(), "--client-sid", client.sid.String(),
		"--target-root", secondTarget, "--git-ignore", ignore}
	if os.Getenv("MCE4D_WIN_RENAME_PUBLIC") != "" {
		lspRole := newAppContainer(t, "ProfileRename")
		args = append(args, "--lsp-sid", lspRole.sid.String())
	}
	// The package namespace includes an aggregate group SID. It cannot be
	// declared as a per-process identity.
	group := append([]string(nil), args...)
	group[6] = "S-1-15-2-1"
	run(3, group...)
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refusal created a profile: %v", err)
	}
	var prepared struct {
		ProfileID string `json:"profile_id"`
	}
	if err := json.Unmarshal(run(0, args...), &prepared); err != nil || !model.ValidUUID(prepared.ProfileID) {
		t.Fatalf("prepare did not produce a valid profile ID: %v", err)
	}
	verify := func(want int, engine, client string) []byte {
		t.Helper()
		return run(want, "verify-roles", "--root", root, "--engine-pid", engine, "--client-pid", client)
	}
	bootstrap := func(want int, engine, client string) []byte {
		t.Helper()
		return run(want, "bootstrap-wal", "--root", root, "--engine-pid", engine, "--client-pid", client)
	}
	var result struct {
		ProfileID      string `json:"profile_id"`
		EngineVerified bool   `json:"engine_verified"`
		ClientVerified bool   `json:"client_verified"`
	}
	var before = run(0, "check", "--root", root)
	if err := json.Unmarshal(verify(0, enginePID, clientPID), &result); err != nil ||
		result.ProfileID != prepared.ProfileID || !result.EngineVerified || !result.ClientVerified {
		t.Fatalf("unexpected role check: %+v, %v", result, err)
	}
	verify(3, clientPID, enginePID)
	verify(3, strconv.Itoa(os.Getpid()), clientPID)
	verify(2, enginePID, enginePID)
	if after := run(0, "check", "--root", root); !bytes.Equal(before, after) {
		t.Fatal("role verification changed the controller profile")
	}
	bootstrap(3, enginePID, clientPID)
	run(0, "bootstrap-catalog", "--root", root)
	bootstrap(3, clientPID, enginePID)
	control := filepath.Join(root, "control")
	stage := filepath.Join(control, "catalog-wal")
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused bootstrap created a WAL stage: %v", err)
	}
	var genesis struct {
		ProfileID           string `json:"profile_id"`
		Stage               string `json:"stage"`
		CatalogIncarnation  string `json:"catalog_incarnation"`
		HeadGenesisVerified bool   `json:"head_genesis_verified"`
	}
	if err := json.Unmarshal(bootstrap(0, enginePID, clientPID), &genesis); err != nil ||
		genesis.ProfileID != prepared.ProfileID || genesis.Stage != "CREATING" ||
		!genesis.HeadGenesisVerified || !model.ValidUUID(genesis.CatalogIncarnation) {
		t.Fatalf("unexpected WAL genesis: %+v, %v", genesis, err)
	}
	bootstrap(3, enginePID, clientPID)
	readyCheck := run(0, "check", "--root", root)
	if !bytes.Contains(readyCheck, []byte(`"trusted_head":false`)) {
		t.Fatal("offline WAL stage incorrectly claimed a trusted runtime head")
	}
	header, err := os.Open(filepath.Join(stage, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	var bytesHeader [100]byte
	_, readErr := io.ReadFull(header, bytesHeader[:])
	if err := errors.Join(readErr, header.Close()); err != nil || bytesHeader[18] != 2 || bytesHeader[19] != 2 {
		t.Fatalf("closed SQLite image is not in WAL format: %v", err)
	}
	headName := filepath.Join(control, "wal-head.log")
	if err := os.Rename(headName, headName+".test"); err != nil {
		t.Fatal(err)
	}
	run(3, "check", "--root", root)
	if err := os.Rename(headName+".test", headName); err != nil {
		t.Fatal(err)
	}
	logName := filepath.Join(stage, "wal-authority.log")
	record, err := os.ReadFile(logName)
	if err != nil || len(record) != 512 {
		t.Fatalf("unexpected authority record: %v", err)
	}
	changed := bytes.Clone(record)
	changed[16] ^= 1
	if err := os.WriteFile(logName, changed, 0600); err != nil {
		t.Fatal(err)
	}
	run(3, "check", "--root", root)
	if err := os.WriteFile(logName, record, 0600); err != nil {
		t.Fatal(err)
	}
	if readBack := run(0, "check", "--root", root); !bytes.Equal(readBack, readyCheck) {
		t.Fatal("checked result changed after reintroducing the pristine fixture record")
	}

	mainName := filepath.Join(stage, "catalog.sqlite")
	readFixture := func(name string) []byte {
		t.Helper()
		value, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	genesisMain, genesisHead := readFixture(mainName), readFixture(headName)
	git := filepath.Join(target, ".git")
	if err := os.MkdirAll(filepath.Join(git, "objects"), 0700); err != nil {
		t.Fatal(err)
	}
	headInput := []byte("ref: refs/heads/main\n")
	if err := os.WriteFile(filepath.Join(git, "HEAD"), headInput, 0600); err != nil {
		t.Fatal(err)
	}
	register := func(want int, engine, client, dest string, ipc bool) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "mce4d.exe"), "repo", "add", "--root", dest)
		ipcMode := "0"
		if ipc {
			ipcMode = "1"
		}
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+root, "MCE4D_WINDOWS_ROLE_IPC="+ipcMode,
			"MCE4D_WINDOWS_STAGED_ENGINE_PID="+engine, "MCE4D_WINDOWS_STAGED_CLIENT_PID="+client)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != want || want != 0 && len(out) != 0 {
			t.Fatalf("protected registration exited %d, expected %d: %s", code, want, stderr.String())
		}
		return out
	}
	if os.Getenv("MCE4D_WIN_RENAME_PUBLIC") == "only" {
		prepareSecondWindowsWorktree(t, secondTarget)
		registered := strings.Fields(string(register(0, "0", "0", secondTarget, true)))
		if len(registered) != 5 || !model.ValidUUID(registered[2]) {
			t.Fatal("rename fixture registration is missing")
		}
		run(0, "worktree-create", "--root", root, "--worktree-id", registered[2])
		testWindowsPublicRename(t, ctx, bin, root, secondTarget, registered[2])
		return
	}
	register(3, clientPID, enginePID, target, false)
	register(3, enginePID, clientPID, t.TempDir(), false)
	if !bytes.Equal(readFixture(logName), record) || !bytes.Equal(readFixture(headName), genesisHead) {
		t.Fatal("rejected admission changed the authority")
	}
	// Neither PID names a live role. The broker must use its own two
	// inherited channels and validate the launched AppContainer tokens.
	registration := strings.Fields(string(register(0, "0", "0", target, true)))
	if len(registration) != 5 || !model.ValidUUID(registration[2]) || !model.ValidDigest(registration[4]) {
		t.Fatalf("unexpected registration result: %q", registration)
	}
	currentMain, currentLog, currentHead := readFixture(mainName), readFixture(logName), readFixture(headName)
	if len(currentLog) != 3*512 || len(currentHead) != 3*192 || !bytes.Equal(currentLog[:512], record) ||
		!bytes.Equal(currentHead[:192], genesisHead) || currentLog[520] != 2 || currentLog[1032] != 5 {
		t.Fatal("successful registration did not append PREPARE and COMMIT to the independent authority")
	}
	verifySQL := func() {
		t.Helper()
		u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(mainName), RawQuery: "mode=ro&immutable=1"}
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM worktrees WHERE worktree_id=?", registration[2]).Scan(&count); err != nil || count != 1 {
			t.Fatalf("registered worktree not in the actual SQLite image: count=%d, err=%v", count, err)
		}
	}
	verifySQL()
	run(0, "check", "--root", root)
	putFixture := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Reinstall the old, mutually consistent DB and mutable receipt bytes.
	// The unchanged independent head must still block admission.
	putFixture(mainName, genesisMain)
	putFixture(logName, record)
	run(3, "check", "--root", root)
	register(3, enginePID, clientPID, target, false)
	if !bytes.Equal(readFixture(headName), currentHead) {
		t.Fatal("rejected stale state changed the head")
	}
	putFixture(mainName, currentMain)
	putFixture(logName, currentLog)
	run(0, "check", "--root", root)
	// PREPARE was published to both files. No outcome is assumed after restart.
	putFixture(logName, currentLog[:2*512])
	putFixture(headName, currentHead[:2*192])
	run(3, "check", "--root", root)
	register(3, enginePID, clientPID, target, false)
	putFixture(logName, currentLog)
	putFixture(headName, currentHead)
	register(3, enginePID, clientPID, target, false)
	aborted := readFixture(logName)
	if len(aborted) != 5*512 || aborted[4*512+8] != 4 {
		t.Fatal("unchanged failed SQL did not publish an abort receipt")
	}
	run(0, "check", "--root", root)
	verifySQL()
	unknown := "99999999-9999-4999-9999-999999999999"
	run(3, "worktree-create", "--root", root, "--worktree-id", unknown)
	if _, err := os.Stat(filepath.Join(control, "worktrees", unknown)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unregistered worktree acquired a staging slot: %v", err)
	}
	var slot struct {
		WorktreeID         string `json:"worktree_id"`
		IndexIncarnation   string `json:"index_incarnation"`
		RecordsIncarnation string `json:"records_incarnation"`
		Verified           bool   `json:"verified"`
	}
	worktree := registration[2]
	out := run(0, "worktree-create", "--root", root, "--worktree-id", worktree)
	if err := json.Unmarshal(out, &slot); err != nil || !slot.Verified || slot.WorktreeID != worktree ||
		!model.ValidUUID(slot.IndexIncarnation) || !model.ValidUUID(slot.RecordsIncarnation) || slot.IndexIncarnation == slot.RecordsIncarnation {
		t.Fatalf("worktree staging returned invalid identities: %s, %v", out, err)
	}
	worktreeDir := filepath.Join(control, "worktrees", worktree)
	if marker := readFixture(filepath.Join(worktreeDir, "worktree.json")); !bytes.Contains(marker, []byte(`"stage":"CREATING"`)) {
		t.Fatal("worktree staging incorrectly entered READY")
	}
	for _, kind := range []string{"index", "records"} {
		main := filepath.Join(worktreeDir, kind+"-wal", kind+".sqlite")
		data := readFixture(main)
		if len(data) < 100 || data[18] != 2 || data[19] != 2 {
			t.Fatalf("%s has no checkpointed SQLite WAL image", kind)
		}
		if len(readFixture(filepath.Join(worktreeDir, kind+"-control", "wal-head.log"))) != 192 ||
			len(readFixture(filepath.Join(worktreeDir, kind+"-wal", "wal-authority.log"))) != 512 {
			t.Fatalf("%s is missing separate genesis authority", kind)
		}
	}
	run(3, "worktree-create", "--root", root, "--worktree-id", worktree)

	checkPools := func(want int, worktree, profile string) []byte {
		t.Helper()
		return run(want, "check-pools", "--root", root, "--worktree-id", worktree, "--profile-id", profile)
	}
	checkPools(3, unknown, registration[4])
	checkPools(3, worktree, strings.Repeat("0", 64))
	var pools struct {
		InstallationID     string `json:"installation_id"`
		Stage              string `json:"stage"`
		ProfileID          string `json:"profile_id"`
		WorktreeID         string `json:"worktree_id"`
		CatalogIncarnation string `json:"catalog_incarnation"`
		IndexIncarnation   string `json:"index_incarnation"`
		RecordsIncarnation string `json:"records_incarnation"`
		WALHeadsVerified   bool   `json:"wal_heads_verified"`
	}
	poolReport := checkPools(0, worktree, registration[4])
	if err := json.Unmarshal(poolReport, &pools); err != nil || pools.Stage != "CREATING" ||
		!model.ValidUUID(pools.InstallationID) || !model.ValidUUID(pools.CatalogIncarnation) ||
		pools.ProfileID != registration[4] || pools.WorktreeID != worktree || !pools.WALHeadsVerified ||
		pools.IndexIncarnation != slot.IndexIncarnation || pools.RecordsIncarnation != slot.RecordsIncarnation {
		t.Fatalf("incorrect scoped pools report: %s, %v", poolReport, err)
	}
	indexHead := filepath.Join(worktreeDir, "index-control", "wal-head.log")
	recordsHead := filepath.Join(worktreeDir, "records-control", "wal-head.log")
	for _, head := range []string{headName, indexHead, recordsHead} {
		original := readFixture(head)
		changed := bytes.Clone(original)
		changed[len(changed)-1] ^= 1
		putFixture(head, changed)
		checkPools(3, worktree, registration[4])
		putFixture(head, original)
	}
	originalRecordsHead := readFixture(recordsHead)
	putFixture(recordsHead, readFixture(indexHead))
	checkPools(3, worktree, registration[4])
	putFixture(recordsHead, originalRecordsHead)
	markerName := filepath.Join(worktreeDir, "worktree.json")
	originalMarker := readFixture(markerName)
	putFixture(markerName, bytes.Replace(originalMarker, []byte(worktree), []byte(unknown), 1))
	checkPools(3, worktree, registration[4])
	putFixture(markerName, originalMarker)
	mainRecords := filepath.Join(worktreeDir, "records-wal", "records.sqlite")
	beforeRecords := readFixture(mainRecords)
	corruptRecords := bytes.Clone(beforeRecords)
	corruptRecords[len(corruptRecords)-1] ^= 1
	putFixture(mainRecords, corruptRecords)
	checkPools(3, worktree, registration[4])
	putFixture(mainRecords, beforeRecords)
	checkPools(0, worktree, registration[4])
	testWindowsInspection(t, ctx, filepath.Join(bin, "mce4d.exe"), root, worktree, registration[4], control, worktreeDir, recordsHead, originalRecordsHead)
	for _, main := range []string{mainName, filepath.Join(worktreeDir, "index-wal", "index.sqlite"), mainRecords} {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, err := os.Stat(main + suffix); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("scoped immutable read left a sidecar: %v", err)
			}
		}
	}
	t.Log("packaged IPC opened three scoped SQLite pools, refused invalid scope, all three modified heads, a swapped head and a changed image")

	digest := sha256.Sum256(readFixture(filepath.Join(bin, "mce4d.exe")))
	pinnedWorker := filepath.Join(control, fmt.Sprintf("ipc-engine-%x", digest), "mce4d.exe")
	approvedWorker := readFixture(pinnedWorker)
	if sha256.Sum256(approvedWorker) != digest {
		t.Fatal("role worker did not use the packaged runtime image")
	}
	badWorker := bytes.Clone(approvedWorker)
	badWorker[len(badWorker)-1] ^= 1
	putFixture(pinnedWorker, badWorker)
	register(3, "0", "0", target, true)
	if !bytes.Equal(readFixture(logName), aborted) {
		t.Fatal("an altered IPC worker allowed catalog mutation")
	}
	putFixture(pinnedWorker, approvedWorker)

	if err := engineProcess.stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := engineProcess.wait(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	verify(3, enginePID, clientPID)
	bootstrap(3, enginePID, clientPID)
	register(3, enginePID, clientPID, target, false)
	if got := readFixture(filepath.Join(git, "HEAD")); !bytes.Equal(got, headInput) {
		t.Fatal("protected profile operations changed the target fixture")
	}
	// An ordinary process cannot act as a protected IPC peer.
	worker := exec.CommandContext(ctx, filepath.Join(bin, "mce4d.exe"), "__mce4d-role-ipc-v1")
	if output, err := worker.Output(); err == nil || len(output) != 0 {
		t.Fatal("direct launch as an ordinary user accepted the worker role")
	}
	t.Log("two actual AppContainer process tokens matched the protected role bindings; swapped, ordinary and exited processes refused")
	t.Log("checkpointed WAL catalog and its independent initial head verified; missing head and altered receipt refused")
	t.Log("packaged repo add used authenticated inherited IPC and committed the SQLite catalog and independent head; stale and pending authority refused")
	t.Log("two worktree WAL stores initialized with separate heads; unregistered scope, duplicate slot and altered worker refused")
	testPackagedWindowsRestore(t, ctx, bin, root, worktree, registration[4])
	testPackagedWindowsIndex(t, ctx, bin, root, target, worktree, registration[4])
	if os.Getenv("MCE4D_WIN_INDEX_CRASH_FIXTURE") == "1" {
		testPackagedWindowsInterruptedIndex(t, ctx, bin, root, worktree, registration[4])
	}
	prepareSecondWindowsWorktree(t, secondTarget)
	second := strings.Fields(string(register(0, "0", "0", secondTarget, true)))
	if len(second) != 5 || !model.ValidUUID(second[2]) || second[2] == worktree || !model.ValidDigest(second[4]) {
		t.Fatalf("unexpected second registration: %q", second)
	}
	run(0, "worktree-create", "--root", root, "--worktree-id", second[2])
	testWindowsScopedMaintenance(t, ctx, filepath.Join(bin, "mce4d.exe"), root, target, worktree, registration[4], second[2], second[4])
	testWindowsCatalogAdmin(t, ctx, filepath.Join(bin, "mce4d.exe"), root, target, worktree)
	if os.Getenv("MCE4D_WIN_RENAME_PUBLIC") == "1" {
		testWindowsPublicRename(t, ctx, bin, root, secondTarget, second[2])
	}
}

func testWindowsCatalogAdmin(t *testing.T, ctx context.Context, executable, installation, target, worktree string) {
	t.Helper()
	run := func(want int, ipc string, input any, args ...string) []byte {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+installation, "MCE4D_WINDOWS_ROLE_IPC="+ipc)
		cmd.Stdin = bytes.NewReader(raw)
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var failure *exec.ExitError
			if !errors.As(err, &failure) {
				t.Fatal(err)
			}
			code = failure.ExitCode()
		}
		if code != want {
			t.Fatalf("Windows catalog admin %v exited %d, expected %d: %s", args, code, want, out)
		}
		return out
	}
	head := filepath.Join(installation, "control", "wal-head.log")
	readHead := func() []byte {
		t.Helper()
		raw, err := os.ReadFile(head)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := readHead()
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	provider := fmt.Sprintf("%x", sha256.Sum256(image))
	p := model.ProfileAdminRequest{WorktreeID: worktree, ProfileName: "typed-windows", Profile: &model.GoProfile{
		SchemaVersion: 1, Language: "go", Goos: "windows", Goarch: "amd64", BuildTags: []string{},
		ModuleRoots: []string{"."}, ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{},
		ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: provider,
	}}
	args := []string{"profile", "set", "--worktree-id", worktree, "--input-json", "-"}
	run(3, "0", p, args...)
	if !bytes.Equal(before, readHead()) {
		t.Fatal("profile operation without authorization advanced WAL")
	}
	out := run(0, "1", p, args...)
	profile, err := model.ProfileDigest(p.Profile, 32768)
	if err != nil || !bytes.Contains(out, []byte(profile)) || len(readHead()) != len(before)+2*192 {
		t.Fatalf("profile digest or committed WAL head missing: %s, %v", out, err)
	}
	run(3, "1", p, args...) // expected revision 0 is now stale
	limits := model.DefaultRuntimeConfig().Core
	config := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{target}, DeniedGlobs: []string{"never.go"},
		TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{},
		MaxFileBytes: limits.MaxFileBytes, MaxSourceBytes: limits.MaxSourceBytesPerSnapshot, MaxOutputBytes: limits.MaxOutputBytes,
		SecretRulesVersion: "builtin-v1"}
	policy := model.PolicyAdminRequest{WorktreeID: worktree, ExpectedEpoch: 1, Policy: config}
	policyArgs := []string{"policy", "set", "--input-json", "-"}
	outside := policy
	outside.Policy.AllowedRoots = []string{filepath.Dir(target)}
	before = readHead()
	run(3, "1", outside, policyArgs...)
	if !bytes.Equal(before, readHead()) {
		t.Fatal("unapproved policy root advanced WAL")
	}
	out = run(0, "1", policy, policyArgs...)
	digest, err := model.PolicyDigest(config, 32768)
	if err != nil || !bytes.Contains(out, []byte(digest)) || len(readHead()) != len(before)+2*192 {
		t.Fatalf("policy digest or committed WAL head missing: %s, %v", out, err)
	}
	out = run(0, "1", nil, "index", "--worktree-id", worktree, "--profile-id", profile)
	if !bytes.Contains(out, []byte("syntax partial")) {
		t.Fatalf("unproved typed profile should publish only syntax: %s", out)
	}
	out = run(0, "1", map[string]any{}, "--worktree-id", worktree, "--profile-id", profile, "query", "status", "--input-json", "-")
	if !bytes.Contains(out, []byte(`"index_state":"published"`)) {
		t.Fatalf("registered profile could not reopen its published index: %s", out)
	}
	t.Log("packaged catalog admin authenticated a typed Go profile and scoped policy, enforced expected versions, appended independent WAL receipts, and reopened the syntax fallback generation")
}

func testWindowsInspection(t *testing.T, ctx context.Context, executable, root, worktree, profile, control, worktreeDir, head string, originalHead []byte) {
	t.Helper()
	command := func(ipc string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+root, "MCE4D_WINDOWS_ROLE_IPC="+ipc)
		return cmd
	}
	query := []string{"--worktree-id", worktree, "--profile-id", profile, "query", "status", "--input-json", "-"}
	for _, ipc := range []string{"0", "1"} {
		cmd := command(ipc, query...)
		cmd.Stdin = strings.NewReader("{}")
		out, err := cmd.Output()
		if ipc == "0" {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 3 || !bytes.Contains(out, []byte("UNSUPPORTED_CAPABILITY")) {
				t.Fatalf("inspection was available without controller opt-in: %s, %v", out, err)
			}
			continue
		}
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				t.Fatalf("packaged CLI inspection failed: %s, %v, %s", out, err, exit.Stderr)
			}
			t.Fatal(err)
		}
		requireInspectionStatus(t, out, worktree, profile)
	}
	cmd := command("1", "mcp", "--worktree-id", worktree, "--profile-id", profile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "mce4d_windows_inspection", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd, TerminateDuration: 3 * time.Second}, nil)
	if err != nil {
		t.Fatalf("packaged protected MCP startup: %v, %s", err, stderr.String())
	}
	closed := false
	defer func() {
		if !closed {
			_ = session.Close()
		}
	}()
	callStatus := func() *sdk.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "mce4d_workspace_status", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	status := callStatus()
	if status.IsError || len(status.Content) != 1 {
		t.Fatalf("unexpected packaged MCP status: %+v", status)
	}
	content, ok := status.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatal("packaged MCP response is missing its text content")
	}
	requireInspectionStatus(t, []byte(content.Text), worktree, profile)

	acquireExclusive := func(name, scope string, busy bool) {
		t.Helper()
		root, err := paths.OpenRoot(name)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		lease, err := runtime.AcquireLifecycle(ctx, root, scope, ports.ExclusiveLease, 120*time.Millisecond)
		if busy {
			if lease != nil {
				_ = lease.Close()
			}
			if !errors.Is(err, runtime.ErrStoreMaintenance) {
				t.Fatalf("exclusive lease did not observe the live MCP pool: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}
	acquireExclusive(control, "catalog", true)
	acquireExclusive(worktreeDir, worktree, true)
	changed := bytes.Clone(originalHead)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(head, changed, 0600); err != nil {
		t.Fatal(err)
	}
	rejected := callStatus()
	if err := os.WriteFile(head, originalHead, 0600); err != nil {
		t.Fatal(err)
	}
	if !rejected.IsError {
		t.Fatal("open MCP session served a result after the protected WAL head changed")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("packaged protected MCP close: %v", err)
	}
	closed = true
	acquireExclusive(worktreeDir, worktree, false)
	acquireExclusive(control, "catalog", false)
	t.Log("packaged CLI and MCP returned authenticated SQLite-backed status; both leases held across MCP requests and released after close; changed head refused")
}

func requireInspectionStatus(t *testing.T, raw []byte, worktree, profile string) {
	t.Helper()
	var result struct {
		Status string           `json:"status"`
		Data   model.StatusData `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Status != "ok" ||
		result.Data.Operation != "status" || result.Data.IndexState != "missing" || len(result.Data.RegisteredScopes) != 1 {
		t.Fatalf("unexpected SQLite-backed workspace status: %s, %v", raw, err)
	}
	scope := result.Data.RegisteredScopes[0]
	if scope.WorktreeID != worktree || scope.ProfileID == nil || *scope.ProfileID != profile ||
		scope.RootStatus != "registered" || scope.IndexState != "missing" {
		t.Fatalf("status returned an unexpected binding: %s", raw)
	}
}
