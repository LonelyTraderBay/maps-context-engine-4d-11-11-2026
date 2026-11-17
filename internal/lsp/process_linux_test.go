//go:build linux

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestManagedProcessHelper(t *testing.T) {
	mode := os.Getenv("MCE_LSP_FIXTURE_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "server":
		reader := bufio.NewReader(os.Stdin)
		if _, err := readLSPFrame(reader, 1024); err != nil {
			os.Exit(21)
		}
		frame, _ := encodeLSPFrame([]byte(`{"jsonrpc":"2.0","id":17,"method":"workspace/applyEdit","params":{}}`), 1024)
		_, _ = os.Stdout.Write(frame)
		response, err := readLSPFrame(reader, 1024)
		if err != nil || !strings.Contains(string(response), `"applied":false`) {
			os.Exit(22)
		}
		frame, _ = encodeLSPFrame([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ready":true}}`), 1024)
		_, _ = os.Stdout.Write(frame)
		for {
			time.Sleep(time.Hour)
		}
	case "silent":
		if _, err := readLSPFrame(bufio.NewReader(os.Stdin), 1024); err != nil {
			os.Exit(26)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "stalled", "child":
		for {
			time.Sleep(time.Hour)
		}
	case "writer":
		file, err := os.OpenFile(os.Getenv("MCE_LSP_FIXTURE_WRITER"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(27)
		}
		for {
			_, _ = file.Write([]byte("x"))
			time.Sleep(time.Millisecond)
		}
	case "stderr":
		_, _ = os.Stderr.Write(make([]byte, 1024*1024+16384))
		for {
			time.Sleep(time.Hour)
		}
	case "exit":
		os.Exit(0)
	case "parent":
		path, err := os.Executable()
		if err != nil {
			os.Exit(23)
		}
		child := exec.Command(path, "-test.run=^TestManagedProcessHelper$")
		childMode := os.Getenv("MCE_LSP_FIXTURE_CHILD_MODE")
		if childMode == "" {
			childMode = "child"
		}
		child.Env = []string{"MCE_LSP_FIXTURE_MODE=" + childMode, "MCE_LSP_FIXTURE_WRITER=" + os.Getenv("MCE_LSP_FIXTURE_WRITER")}
		if err := child.Start(); err != nil {
			os.Exit(23)
		}
		if err := os.WriteFile(os.Getenv("MCE_LSP_FIXTURE_PIDFILE"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			os.Exit(24)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(25)
	}
}

func testManagedProcess(t *testing.T, mode string, extra ...string) *managedProcess {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	env := append([]string{"MCE_LSP_FIXTURE_MODE=" + mode}, extra...)
	p, err := startManaged(file, []string{"-test.run=^TestManagedProcessHelper$"}, t.TempDir(), env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.close() })
	return p
}

func TestManagedProcessExchangeAndStop(t *testing.T) {
	p := testManagedProcess(t, "server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := protocolRequest(ctx, p.incoming, p.send, 1, "initialize", nil, nil, nil)
	if err != nil || string(result) != `{"ready":true}` {
		t.Fatalf("protocol: %s %v", result, err)
	}
	if err := p.close(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-p.reaped:
	default:
		t.Fatal("process was not reaped")
	}
}

func TestManagedProcessKeepsExitedPIDUntilGroupCleanup(t *testing.T) {
	p := testManagedProcess(t, "exit")
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture process did not exit")
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", p.cmd.Process.Pid))
	if err != nil || !strings.Contains(string(stat), ") Z ") {
		t.Fatalf("exited process was reaped before group cleanup: %v", err)
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", p.cmd.Process.Pid)); !os.IsNotExist(err) {
		t.Fatalf("fixture process was not reaped: %v", err)
	}
}

func TestManagedProcessRepeatedStartStop(t *testing.T) {
	for i := 0; i < 8; i++ {
		p := testManagedProcess(t, "stalled")
		if err := p.close(); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		select {
		case <-p.reaped:
		default:
			t.Fatalf("iteration %d retained child", i)
		}
	}
}

func TestManagedProcessBlockedInputCanCancel(t *testing.T) {
	p := testManagedProcess(t, "stalled")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := p.send(ctx, map[string]any{"payload": strings.Repeat("x", 1024*1024)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked pipe did not time out: %v", err)
	}
	if err := p.close(); err != nil {
		t.Fatalf("blocked worker did not stop: %v", err)
	}
}

func TestManagedProcessReadTimeoutStopsChild(t *testing.T) {
	p := testManagedProcess(t, "silent")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := protocolRequest(ctx, p.incoming, p.send, 1, "rename", nil, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing read timeout: %v", err)
	}
	if err := p.close(); err != nil {
		t.Fatalf("timed-out process did not stop: %v", err)
	}
}

func TestManagedProcessStderrCap(t *testing.T) {
	p := testManagedProcess(t, "stderr")
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("stderr overflow did not stop process")
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedProcessKillsOwnedDescendants(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	p := testManagedProcess(t, "parent", "MCE_LSP_FIXTURE_PIDFILE="+pidfile)
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidfile)
		if err == nil {
			childPID, _ = strconv.Atoi(string(data))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID < 1 {
		t.Fatal("fixture child did not start")
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPID))
		if os.IsNotExist(err) || errors.Is(unix.Kill(childPID, 0), unix.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fixture descendant %d is still running", childPID)
}

func TestManagedProcessWaitsForWriterDescendant(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "child.pid")
	marker := filepath.Join(dir, "writer.log")
	p := testManagedProcess(t, "parent", "MCE_LSP_FIXTURE_PIDFILE="+pidfile, "MCE_LSP_FIXTURE_CHILD_MODE=writer", "MCE_LSP_FIXTURE_WRITER="+marker)
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidfile)
		if err == nil {
			pid, _ = strconv.Atoi(string(data))
		}
		info, err := os.Stat(marker)
		if pid > 0 && err == nil && info.Size() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid < 1 {
		t.Fatal("fixture writer did not start")
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil && !strings.Contains(string(stat), ") Z ") {
		t.Fatalf("writer still active when close returned: %s", stat)
	}
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	after, err := os.Stat(marker)
	if err != nil || before.Size() != after.Size() {
		t.Fatalf("writer continued after close: %v", err)
	}
}

func TestProcessGroupWaitFailsWhileOwnedGroupIsRunning(t *testing.T) {
	p := testManagedProcess(t, "stalled")
	if err := waitForProcessGroup(p.cmd.Process.Pid, 50*time.Millisecond); !errors.Is(err, ErrLSPProcess) {
		t.Fatalf("live group treated as settled: %v", err)
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedProcessRejectsOversizedOutbound(t *testing.T) {
	p := testManagedProcess(t, "stalled")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value := map[string]any{"payload": strings.Repeat("x", int(resultHardBytes))}
	if _, err := json.Marshal(value); err != nil {
		t.Fatal(err)
	}
	if err := p.send(ctx, value); !errors.Is(err, ErrLSPFrame) {
		t.Fatalf("oversized JSON accepted: %v", err)
	}
}
