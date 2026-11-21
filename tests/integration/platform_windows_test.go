package integration

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
	_ "modernc.org/sqlite"
)

var (
	userenv                   = windows.NewLazySystemDLL("userenv.dll")
	createAppContainerProfile = userenv.NewProc("CreateAppContainerProfile")
	deleteAppContainerProfile = userenv.NewProc("DeleteAppContainerProfile")
	getAppContainerFolderPath = userenv.NewProc("GetAppContainerFolderPath")
)

type appContainer struct {
	sid *windows.SID
}

func (c appContainer) localFolder(t *testing.T) string {
	t.Helper()
	sid := windows.StringToUTF16Ptr(c.sid.String())
	var name *uint16
	hr, _, _ := getAppContainerFolderPath.Call(uintptr(unsafe.Pointer(sid)), uintptr(unsafe.Pointer(&name)))
	if uint32(hr) != 0 || name == nil {
		t.Fatalf("GetAppContainerFolderPath failed with HRESULT %#08x", uint32(hr))
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(name))
	path := windows.UTF16PtrToString(name)
	local := os.Getenv("LOCALAPPDATA")
	relative, err := filepath.Rel(local, path)
	if !filepath.IsAbs(local) || err != nil || !filepath.IsLocal(relative) || relative == "." {
		t.Fatal("AppContainer folder is outside local profile storage")
	}
	return path
}

func newAppContainer(t *testing.T, role string) appContainer {
	t.Helper()
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := windows.StringToUTF16Ptr(fmt.Sprintf("MCE4D.LocalFixture.%s.%x", role, nonce))
	var sid *windows.SID
	hr, _, _ := createAppContainerProfile.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(name)), 0, 0, uintptr(unsafe.Pointer(&sid)))
	if uint32(hr) != 0 || sid == nil {
		t.Fatalf("CreateAppContainerProfile(%s) failed with HRESULT %#08x", role, uint32(hr))
	}
	t.Cleanup(func() {
		hr, _, _ := deleteAppContainerProfile.Call(uintptr(unsafe.Pointer(name)))
		if uint32(hr) != 0 {
			t.Errorf("DeleteAppContainerProfile(%s) failed with HRESULT %#08x", role, uint32(hr))
		}
		windows.FreeSid(sid)
	})
	return appContainer{sid: sid}
}

func fixtureDirectory(t *testing.T, path, grants string) string {
	t.Helper()
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	protectFixturePath(t, path, grants)
	return path
}

func protectFixturePath(t *testing.T, path, grants string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")" + grants)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func readableBy(role appContainer) string {
	return "(A;OICI;FRFX;;;" + role.sid.String() + ")"
}

func writableBy(role appContainer) string {
	// File generic read/write/execute and delete, with no write-DACL or write-owner.
	return "(A;OICI;0x1301bf;;;" + role.sid.String() + ")"
}

func markFixtureLowIntegrity(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe"),
		dir, "/setintegritylevel", "(OI)(CI)L")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cannot label fresh fixture directory at low integrity: %s, %v", output, err)
	}
}

type appContainerChild struct {
	process windows.Handle
	job     *fsruntime.WindowsProcessJob
	output  *os.File
}

func (c *appContainerChild) wait(deadline time.Duration) (uint32, error) {
	event, err := windows.WaitForSingleObject(c.process, uint32(deadline/time.Millisecond))
	if err != nil {
		return 0, err
	}
	if event != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("bounded child wait returned %#x", event)
	}
	var status uint32
	err = windows.GetExitCodeProcess(c.process, &status)
	return status, err
}

func (c *appContainerChild) stop() error {
	return c.job.Terminate(71)
}

func startContainerProbe(t *testing.T, role appContainer, executable, control, state, mode string) *appContainerChild {
	t.Helper()
	return startWindowsFixtureJob(t, role, executable, filepath.Dir(executable),
		[]string{"-test.run=^TestWindowsAppContainerWorker$", "-test.timeout=20s"},
		[]string{"MCE4D_WIN_FIXTURE_CONTROL=" + control, "MCE4D_WIN_FIXTURE_MODE=" + mode,
			"MCE4D_WIN_FIXTURE_SID=" + role.sid.String(), "MCE4D_WIN_FIXTURE_STATE=" + state}, nil)
}

func startWindowsFixtureJob(t *testing.T, role appContainer, executable, directory string, arguments, environment []string, input *os.File) *appContainerChild {
	t.Helper()
	job, err := fsruntime.NewWindowsProcessJob(16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	})
	output, sink, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { output.Close() })
	defer sink.Close()
	if input == nil {
		input, err = os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
	}
	environment = append(environment,
		"GOMAXPROCS=2", "LOCALAPPDATA="+os.Getenv("LOCALAPPDATA"), "SystemRoot="+os.Getenv("SystemRoot"),
		"TEMP="+os.TempDir(), "TMP="+os.TempDir(), "USERPROFILE="+os.Getenv("USERPROFILE"))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	process, err := job.StartChild(ctx, fsruntime.WindowsJobChild{
		Executable: executable, Directory: directory, Arguments: arguments, Environment: environment,
		AppContainer: role.sid, Stdin: input, Stdout: sink, Stderr: sink,
	}, func(proc windows.Handle) (resultErr error) {
		if role.sid == nil {
			return nil
		}
		var token windows.Token
		if err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &token); err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, token.Close()) }()
		return checkFixtureContainerSID(token, role.sid.String())
	})
	if err != nil {
		t.Fatalf("cannot start fixture child %s: %v", filepath.Base(executable), err)
	}
	t.Cleanup(func() { windows.CloseHandle(process) })
	child := &appContainerChild{process: process, job: job, output: output}
	t.Cleanup(func() {
		_ = child.stop()
		if _, err := child.wait(5 * time.Second); err != nil {
			t.Error(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := job.WaitEmpty(ctx); err != nil {
			t.Error(err)
		}
	})
	return child
}

func verifyContainerToken(t *testing.T, expected string) {
	t.Helper()
	if err := checkFixtureContainerSID(windows.GetCurrentProcessToken(), expected); err != nil {
		t.Fatal(err)
	}
}

func checkFixtureContainerSID(token windows.Token, expected string) error {
	const tokenAppContainerSID = 31
	var size uint32
	_ = windows.GetTokenInformation(token, tokenAppContainerSID, nil, 0, &size)
	if size == 0 || size > 4096 {
		return errors.New("missing or unbounded AppContainer SID")
	}
	info := make([]byte, size)
	if err := windows.GetTokenInformation(token, tokenAppContainerSID, &info[0], size, &size); err != nil {
		return err
	}
	sid := (*struct{ SID *windows.SID })(unsafe.Pointer(&info[0])).SID
	if sid == nil || sid.String() != expected {
		return errors.New("process does not hold the expected isolated SID")
	}
	return nil
}

const startingIncarnation = "11111111-1111-4111-8111-111111111111"
const restoredIncarnation = "22222222-2222-4222-8222-222222222222"

func TestWindowsAppContainerWorker(t *testing.T) {
	mode := os.Getenv("MCE4D_WIN_FIXTURE_MODE")
	if mode == "" {
		return
	}
	verifyContainerToken(t, os.Getenv("MCE4D_WIN_FIXTURE_SID"))
	control, state := os.Getenv("MCE4D_WIN_FIXTURE_CONTROL"), os.Getenv("MCE4D_WIN_FIXTURE_STATE")
	head := filepath.Join(control, "head")
	if _, err := os.OpenFile(head, os.O_WRONLY, 0); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("container unexpectedly has write access to independent head: %v", err)
	}
	main := filepath.Join(state, "fixture.db")
	if mode == "client" {
		if _, err := os.Open(main); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("client unexpectedly has access to engine state: %v", err)
		}
		fmt.Println("CLIENT_DENIED")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
	defer cancel()
	db := openWindowsFixtureDatabase(t, main)
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA busy_timeout=500", "PRAGMA synchronous=FULL", "PRAGMA wal_autocheckpoint=0"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "writer" {
		var journal string
		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journal); err != nil || journal != "wal" {
			t.Fatalf("journal=%q: %v", journal, err)
		}
		if _, err := db.ExecContext(ctx, "CREATE TABLE captured (id INTEGER PRIMARY KEY, content TEXT NOT NULL); INSERT INTO captured VALUES (1, 'durable')"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "CREATE TABLE identity (incarnation TEXT PRIMARY KEY, epoch INTEGER NOT NULL); INSERT INTO identity VALUES (?, 7)", startingIncarnation); err != nil {
			t.Fatal(err)
		}
		pending, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer pending.Rollback()
		if _, err := pending.ExecContext(ctx, "INSERT INTO captured VALUES (2, 'pending')"); err != nil {
			t.Fatal(err)
		}
		fmt.Println("WAL_WRITER_READY")
		<-ctx.Done()
		t.Fatal("writer was not terminated by the fixture controller")
	}
	if mode != "reader" && mode != "reopen" && mode != "prepare-restored" {
		t.Fatal("unknown fixture mode")
	}
	var count int
	var source string
	if err := db.QueryRowContext(ctx, "SELECT count(*), min(content) FROM captured").Scan(&count, &source); err != nil || count != 1 || source != "durable" {
		t.Fatalf("committed row count=%d content=%q: %v", count, source, err)
	}
	if mode == "reopen" {
		var integrity string
		if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
			t.Fatalf("recovered integrity=%q: %v", integrity, err)
		}
	}
	var incarnation string
	var epoch int
	if err := db.QueryRowContext(ctx, "SELECT incarnation, epoch FROM identity").Scan(&incarnation, &epoch); err != nil || epoch != 7 {
		t.Fatalf("fixture store identity=%q epoch=%d: %v", incarnation, epoch, err)
	}
	if mode == "prepare-restored" {
		next := filepath.Join(state, "restored.db")
		if _, err := db.ExecContext(ctx, "VACUUM INTO ?", next); err != nil {
			t.Fatal(err)
		}
		restored := openWindowsFixtureDatabase(t, next)
		if _, err := restored.ExecContext(ctx, "UPDATE identity SET incarnation=?", restoredIncarnation); err != nil {
			restored.Close()
			t.Fatal(err)
		}
		if err := restored.Close(); err != nil {
			t.Fatal(err)
		}
		fmt.Println("RESTORE_IMAGE_READY")
		return
	}
	fmt.Println("FIXTURE_STORE=" + incarnation)
	fmt.Println("DURABLE_ONLY")
}

func openWindowsFixtureDatabase(t *testing.T, main string) *sql.DB {
	t.Helper()
	fileURI := (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(main)}).String()
	db, err := sql.Open("sqlite", fileURI)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestWindowsAppContainerSQLiteWAL(t *testing.T) {
	base := t.TempDir()
	engine, client := newAppContainer(t, "Engine"), newAppContainer(t, "Client")
	if engine.sid.Equals(client.sid) {
		t.Fatal("fixture roles need separate package SIDs")
	}
	materials := fixtureDirectory(t, filepath.Join(base, "materials"), readableBy(engine)+readableBy(client))
	control := fixtureDirectory(t, filepath.Join(base, "control"), readableBy(engine)+readableBy(client))
	state := fixtureDirectory(t, filepath.Join(base, "state"), writableBy(engine))
	markFixtureLowIntegrity(t, state)
	head := filepath.Join(control, "head")
	if err := os.WriteFile(head, []byte(startingIncarnation), 0600); err != nil {
		t.Fatal(err)
	}
	testExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(materials, "fixture.exe")
	data, err := os.ReadFile(testExe)
	if err != nil || len(data) > 64<<20 {
		t.Fatalf("unexpected fixture executable: %v", err)
	}
	if err := os.WriteFile(executable, data, 0700); err != nil {
		t.Fatal(err)
	}
	writer := startContainerProbe(t, engine, executable, control, state, "writer")
	lines := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(io.LimitReader(writer.output, 4096))
		line, _ := reader.ReadString('\n')
		lines <- strings.TrimSpace(line)
	}()
	select {
	case first := <-lines:
		if first != "WAL_WRITER_READY" {
			_, _ = writer.wait(4 * time.Second)
			t.Fatalf("writer did not reach its crash boundary: %q", first)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("writer did not signal readiness")
	}
	if info, err := os.Stat(filepath.Join(state, "fixture.db-wal")); err != nil || info.Size() <= 32 {
		t.Fatalf("test requires a nonempty real WAL on NTFS: %v", err)
	}
	expect := func(role appContainer, mode string, markers ...string) {
		t.Helper()
		child := startContainerProbe(t, role, executable, control, state, mode)
		exit, err := child.wait(6 * time.Second)
		if err != nil {
			t.Fatal(err)
		}
		output, err := io.ReadAll(io.LimitReader(child.output, 4096))
		if err != nil || exit != 0 {
			t.Fatalf("%s exited %d with %q: %v", mode, exit, output, err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(output), marker) {
				t.Fatalf("%s did not emit %q: %q", mode, marker, output)
			}
		}
	}
	expect(engine, "reader", "DURABLE_ONLY")
	expect(client, "client", "CLIENT_DENIED")
	if err := writer.stop(); err != nil {
		t.Fatal(err)
	}
	if exit, err := writer.wait(6 * time.Second); err != nil || exit != 71 {
		t.Fatalf("writer exit=%d: %v", exit, err)
	}
	expect(engine, "reopen", "DURABLE_ONLY", "FIXTURE_STORE="+startingIncarnation)
	if data, err := os.ReadFile(head); err != nil || string(data) != startingIncarnation {
		t.Fatalf("controller-owned head changed: %v", err)
	}
	t.Log("two Windows package SIDs; isolated NTFS state; committed row visible before and after Job Object termination")
	verifyFixtureRestoreTokens(t, head, func() {
		expect(engine, "prepare-restored", "RESTORE_IMAGE_READY")
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, err := os.Stat(filepath.Join(state, "fixture.db"+suffix)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("SQLite left a sidecar before offline fixture replacement: %v", err)
			}
		}
		from := windows.StringToUTF16Ptr(filepath.Join(state, "restored.db"))
		to := windows.StringToUTF16Ptr(filepath.Join(state, "fixture.db"))
		if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
			t.Fatal(err)
		}
		expect(engine, "reopen", "DURABLE_ONLY", "FIXTURE_STORE="+restoredIncarnation)
		if err := os.WriteFile(head, []byte(restoredIncarnation), 0600); err != nil {
			t.Fatal(err)
		}
	})
}

func verifyFixtureRestoreTokens(t *testing.T, head string, replace func()) {
	t.Helper()
	key, err := contextpack.GenerateCursorKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	guard := func(_ context.Context, binding contextpack.CursorBinding) error {
		current, err := os.ReadFile(head)
		if err != nil {
			return err
		}
		if binding.Scope.Members[0].IndexIncarnation != string(current) {
			return contextpack.ErrCursorInvalid
		}
		return nil
	}
	digest := strings.Repeat("a", 64)
	ancillaryID := "33333333-3333-4333-8333-333333333333"
	position := contextpack.SignedPosition{LastKey: []string{}, NextItem: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	makeBinding := func(purpose, incarnation string) contextpack.CursorBinding {
		t.Helper()
		scope := model.ScopeVector{Kind: "single", CatalogIncarnation: ancillaryID, UnavailableMembers: []model.MemberUnavailable{},
			Members: []model.ScopeMember{{WorktreeID: ancillaryID, ProfileID: digest, GenerationID: ancillaryID,
				SnapshotDigest: digest, PolicyDigest: digest, PolicyEpoch: 7, BindingRevision: 7, RecordEpoch: 7,
				IndexIncarnation: incarnation, RecordsIncarnation: ancillaryID}}}
		scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 32768)
		if err != nil {
			t.Fatal(err)
		}
		return contextpack.CursorBinding{Purpose: purpose, PrincipalDigest: digest, BindingDigest: digest,
			RequestDigest: digest, OrderVersion: "v1", Scope: scope, ResultID: ancillaryID, ResultDigest: digest}
	}
	old := make(map[string]string)
	purposes := []string{"pack", "list", "rename-result"}
	for _, purpose := range purposes {
		token, err := codec.Issue(ctx, makeBinding(purpose, startingIncarnation), position, guard)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := codec.VerifyPrincipal(ctx, token, purpose, digest, guard); err != nil {
			t.Fatal(err)
		}
		old[purpose] = token
	}
	replace()
	for _, purpose := range purposes {
		if _, err := codec.VerifyPrincipal(ctx, old[purpose], purpose, digest, guard); !errors.Is(err, contextpack.ErrCursorInvalid) {
			t.Fatalf("old %s token survived fixture replacement: %v", purpose, err)
		}
		token, err := codec.Issue(ctx, makeBinding(purpose, restoredIncarnation), position, guard)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := codec.VerifyPrincipal(ctx, token, purpose, digest, guard); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("offline NTFS fixture replacement changed its incarnation at the same numeric epoch; three old token purposes refused by the production codec")
}
