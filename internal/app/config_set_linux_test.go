//go:build linux

package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func privateConfigService(t *testing.T) (string, *RuntimeConfigService) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	service, err := configServiceForTest(context.Background(), root, "runtime.json", 4)
	if err != nil {
		t.Fatal(err)
	}
	return dir, service
}

func configSetRequest(expected *string, value int) []byte {
	digest := "null"
	if expected != nil {
		digest = fmt.Sprintf("%q", *expected)
	}
	return []byte(fmt.Sprintf(`{"action":"set","config":{"schema_version":1,"core":{"max_file_bytes":%d}},"expected_digest":%s}`, value, digest))
}

func TestRuntimeConfigSetCASAndConcurrentWriters(t *testing.T) {
	dir, service := privateConfigService(t)
	first, err := service.Set(context.Background(), configSetRequest(nil, 12345))
	if err != nil || first.State != "applied" || !first.Data.RestartRequired {
		t.Fatalf("create: %+v %v", first, err)
	}
	file := filepath.Join(dir, "runtime.json")
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config mode: %v %v", info, err)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := service.Set(context.Background(), configSetRequest(nil, 23456))
	if stale.State != "not_applied" || !errors.Is(err, model.ErrConfigConflict) {
		t.Fatalf("absent precondition after create: %+v %v", stale, err)
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != string(before) {
		t.Fatalf("conflicting set modified config: %v", err)
	}
	shown, err := service.Show(context.Background())
	if err != nil || shown.ConfigDigest != first.Data.ConfigDigest || shown.Config.Core.MaxFileBytes != 12345 {
		t.Fatalf("show after create: %+v %v", shown, err)
	}

	type outcome struct {
		result RuntimeConfigSetResult
		err    error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for _, value := range []int{34567, 45678} {
		go func(value int) {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := service.Set(ctx, configSetRequest(&first.Data.ConfigDigest, value))
			results <- outcome{result, err}
		}(value)
	}
	close(start)
	var winner RuntimeConfigSetResult
	applied, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		out := <-results
		switch {
		case out.err == nil && out.result.State == "applied":
			applied++
			winner = out.result
		case errors.Is(out.err, model.ErrConfigConflict) && out.result.State == "not_applied":
			conflicts++
		default:
			t.Fatalf("concurrent writer: %+v %v", out.result, out.err)
		}
	}
	if applied != 1 || conflicts != 1 {
		t.Fatalf("applied=%d conflicts=%d", applied, conflicts)
	}
	shown, err = service.Show(context.Background())
	if err != nil || shown.ConfigDigest != winner.Data.ConfigDigest || !shown.RestartRequired {
		t.Fatalf("show after update: %+v %v", shown, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".runtime.json.pending")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending file remains after update: %v", err)
	}
}

// This helper runs in the test binary only when explicitly launched by the
// multi-process fixture. It receives no user state or repository path.
func TestRuntimeConfigCASProcessHelper(t *testing.T) {
	if os.Getenv("MCE4D_CONFIG_TEST_CHILD") != "1" {
		return
	}
	root, err := paths.OpenRoot(os.Getenv("MCE4D_CONFIG_TEST_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	service, err := configServiceForTest(context.Background(), root, "runtime.json", 4)
	if err != nil {
		t.Fatal(err)
	}
	oldDigest := os.Getenv("MCE4D_CONFIG_TEST_DIGEST")
	fmt.Println("READY")
	var start string
	if _, err := fmt.Fscan(os.Stdin, &start); err != nil || start != "SET" {
		t.Fatal("missing process barrier", err)
	}
	fmt.Println("SETTING")
	result, err := service.Set(context.Background(), configSetRequest(&oldDigest, 56789))
	if err == nil && result.State == "applied" {
		fmt.Println("CAS_RESULT=applied")
		return
	}
	if errors.Is(err, model.ErrConfigConflict) && result.State == "not_applied" {
		fmt.Println("CAS_RESULT=conflict")
		return
	}
	t.Fatalf("unexpected child result: %+v %v", result, err)
}

func TestRuntimeConfigCASAcrossProcesses(t *testing.T) {
	dir, service := privateConfigService(t)
	initial, err := service.Set(context.Background(), configSetRequest(nil, 12345))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".runtime.json.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		stdout *bufio.Reader
		stderr *bytes.Buffer
		start  io.WriteCloser
	}
	children := make([]child, 0, 2)
	for i := 0; i < 2; i++ {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeConfigCASProcessHelper$")
		cmd.Env = append(os.Environ(), "MCE4D_CONFIG_TEST_CHILD=1", "MCE4D_CONFIG_TEST_DIR="+dir, "MCE4D_CONFIG_TEST_DIGEST="+initial.Data.ConfigDigest)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		start, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stderr := new(bytes.Buffer)
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, child{cmd, bufio.NewReader(out), stderr, start})
	}
	for _, c := range children {
		line, err := c.stdout.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "READY" {
			t.Fatalf("child not ready: %q %v %s", line, err, c.stderr.String())
		}
	}
	// Initialize both readers before taking the writer lock. Reads now
	// cooperate with setters, so waiting for READY while holding it deadlocks.
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	for _, c := range children {
		_, err := io.WriteString(c.start, "SET\n")
		if err = errors.Join(err, c.start.Close()); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range children {
		line, err := c.stdout.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "SETTING" {
			t.Fatalf("child not setting: %q %v %s", line, err, c.stderr.String())
		}
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	applied, conflicts := 0, 0
	for _, c := range children {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			t.Fatalf("child result: %q %v %s", line, err, c.stderr.String())
		}
		switch strings.TrimSpace(line) {
		case "CAS_RESULT=applied":
			applied++
		case "CAS_RESULT=conflict":
			conflicts++
		default:
			t.Fatalf("unexpected child result: %q %s", line, c.stderr.String())
		}
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("child failed: %v %s", err, c.stderr.String())
		}
	}
	if applied != 1 || conflicts != 1 {
		t.Fatalf("cross-process applied=%d conflicts=%d", applied, conflicts)
	}
}

func TestRuntimeConfigPendingIsPreserved(t *testing.T) {
	dir, service := privateConfigService(t)
	pending := filepath.Join(dir, ".runtime.json.pending")
	if err := os.WriteFile(pending, []byte("recovery fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Show(context.Background()); !errors.Is(err, fsruntime.ErrConfigPending) {
		t.Fatalf("show accepted pending state: %v", err)
	}
	result, err := service.Set(context.Background(), configSetRequest(nil, 12345))
	if result.State != "not_applied" || !errors.Is(err, fsruntime.ErrConfigPending) {
		t.Fatalf("set accepted pending state: %+v %v", result, err)
	}
	data, err := os.ReadFile(pending)
	if err != nil || string(data) != "recovery fixture" {
		t.Fatalf("pending fixture changed: %q %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config created during pending state: %v", err)
	}
}

func TestRuntimeConfigApprovedFileAndImmutableStartup(t *testing.T) {
	dir, defaultService := privateConfigService(t)
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	custom, err := configServiceForTest(context.Background(), root, "approved.json", 4)
	if err != nil {
		t.Fatal(err)
	}
	bootConfig, bootDigest, err := custom.StartupConfig()
	if err != nil {
		t.Fatal(err)
	}
	set, err := custom.Set(context.Background(), configSetRequest(nil, 12345))
	if err != nil || set.State != "applied" || !set.Data.RestartRequired {
		t.Fatalf("set approved config: %+v %v", set, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "approved.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("custom set changed default file: %v", err)
	}
	shown, err := custom.Show(context.Background())
	if err != nil || shown.ConfigDigest != set.Data.ConfigDigest || shown.Config.Core.MaxFileBytes != 12345 {
		t.Fatalf("custom show: %+v %v", shown, err)
	}
	stillBoot, stillDigest, err := custom.StartupConfig()
	if err != nil || stillBoot != bootConfig || stillDigest != bootDigest || stillDigest == shown.ConfigDigest {
		t.Fatalf("startup config was changed: %+v %q %v", stillBoot, stillDigest, err)
	}
	defaultShown, err := defaultService.Show(context.Background())
	if err != nil || defaultShown.ConfigDigest != bootDigest || defaultShown.RestartRequired {
		t.Fatalf("custom config affected default: %+v %v", defaultShown, err)
	}
	for _, name := range []string{"../outside", "sub/config.json", "", ".runtime.json.pending", ".Runtime.JSON.Lock"} {
		if _, err := configServiceForTest(context.Background(), root, name, 4); !errors.Is(err, fsruntime.ErrConfigUnsupported) {
			t.Fatalf("invalid or reserved config name %q accepted: %v", name, err)
		}
	}
}

func TestRuntimeConfigLockLinkAndTimeout(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		dir, service := privateConfigService(t)
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, ".runtime.json.lock")); err != nil {
			t.Fatal(err)
		}
		result, err := service.Set(context.Background(), configSetRequest(nil, 12345))
		if err == nil || result.State != "not_applied" {
			t.Fatalf("symlink lock accepted: %+v %v", result, err)
		}
		data, err := os.ReadFile(outside)
		if err != nil || string(data) != "unchanged" {
			t.Fatalf("outside fixture changed: %q %v", data, err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		dir, service := privateConfigService(t)
		lock, err := os.OpenFile(filepath.Join(dir, ".runtime.json.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		result, err := service.Set(ctx, configSetRequest(nil, 12345))
		if result.State != "not_applied" || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("locked set: %+v %v", result, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("locked set created config: %v", err)
		}
	})
}

func TestConfigRejectsNonOwnerOnlyRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Creation permissions are filtered by the host's umask. Establish the
	// actual shared mode this negative case intends to reject.
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := configServiceForTest(context.Background(), root, "runtime.json", 4); !errors.Is(err, fsruntime.ErrConfigUnsupported) {
		t.Fatalf("shared state root accepted: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("checking shared root changed state: %v %v", entries, err)
	}
}
