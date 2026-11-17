//go:build linux

package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func fixtureSink(t *testing.T, dir string, files int, size int64, permit *atomic.Bool) (*FileSink, *runtime.Lifecycle) {
	t.Helper()
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lease, err := runtime.AcquireLifecycle(ctx, root, worktree, ports.SharedLease, time.Second)
	if err != nil {
		root.Close()
		t.Fatal(err)
	}
	sink, err := NewFileSink(root, lease, worktree, func(ctx context.Context) error {
		if ctx.Err() != nil || !permit.Load() {
			return ErrUnavailable
		}
		return nil
	}, files, size)
	if err != nil {
		lease.Close()
		root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lease.Close(); err != nil {
			t.Error(err)
		}
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return sink, lease
}

func sinkFixture(t *testing.T, files int, size int64) (string, *FileSink, *atomic.Bool) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	permit := &atomic.Bool{}
	permit.Store(true)
	sink, _ := fixtureSink(t, dir, files, size, permit)
	return dir, sink, permit
}

func logEvent(t *testing.T, sink *FileSink) error {
	t.Helper()
	raw, err := encodeEvent(goodEvent(), worktree, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return sink.appendEvent(ctx, raw)
}

func TestFileSinkRejectsLeaseFromDifferentRoot(t *testing.T) {
	_, existing, _ := sinkFixture(t, 2, 1024)
	other := t.TempDir()
	if err := os.Chmod(other, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(other)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	sink, err := NewFileSink(root, existing.lease, worktree, func(context.Context) error { return nil }, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := logEvent(t, sink); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("accepted lifecycle from another root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrong-root lifecycle created logs")
	}
}

func TestFileSinkRotationAndOwnership(t *testing.T) {
	dir, sink, _ := sinkFixture(t, 3, 512)
	for range 30 {
		if err := logEvent(t, sink); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	observed := 0
	for _, e := range entries {
		if e.Name() == logLock {
			continue
		}
		count++
		p := filepath.Join(dir, "logs", e.Name())
		info, err := os.Stat(p)
		if err != nil || info.Size() > 512 || info.Mode().Perm() != 0600 {
			t.Fatal("bad retention or permissions")
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte(dir)) {
			t.Fatal("unexpected private data")
		}
		scan := bufio.NewScanner(bytes.NewReader(raw))
		lines := 0
		for scan.Scan() {
			var x map[string]any
			if json.Unmarshal(scan.Bytes(), &x) != nil {
				t.Fatal("invalid JSONL")
			}
			lines++
			if x["phase"] == "query" {
				observed++
			}
		}
		if scan.Err() != nil || lines < 2 {
			t.Fatal("bad log")
		}
	}
	if count != 3 || observed == 0 {
		t.Fatalf("retention count %d, events %d", count, observed)
	}
}

func TestFileSinkRejectsUnsafeFilesAndRevocation(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "unmarked", "wrong-owner-marker", "partial", "public-mode"} {
		t.Run(kind, func(t *testing.T) {
			dir, sink, permit := sinkFixture(t, 2, 1024)
			if err := os.Mkdir(filepath.Join(dir, "logs"), 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(dir, "do-not-change")
			if err := os.WriteFile(sentinel, []byte("private sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "logs", slotName(0))
			switch kind {
			case "symlink":
				if err := os.Symlink(sentinel, target); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(sentinel, target); err != nil {
					t.Fatal(err)
				}
			case "unmarked":
				if err := os.WriteFile(target, []byte("unmarked\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-owner-marker":
				if err := os.WriteFile(target, headerBytes("22222222-2222-4222-8222-222222222222", 2, 1), 0600); err != nil {
					t.Fatal(err)
				}
			case "partial":
				if err := os.WriteFile(target, append(headerBytes(worktree, 2, 1), []byte("{partial")...), 0600); err != nil {
					t.Fatal(err)
				}
			case "public-mode":
				if err := os.WriteFile(target, headerBytes(worktree, 2, 1), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(target, 0644); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(sentinel)
			if err := logEvent(t, sink); err == nil {
				t.Fatal("unsafe file accepted")
			}
			after, _ := os.ReadFile(sentinel)
			if !bytes.Equal(before, after) {
				t.Fatal("sentinel modified")
			}
			permit.Store(false)
			if err := logEvent(t, sink); err == nil {
				t.Fatal("revoked permission accepted")
			}
		})
	}
}

func TestFileSinkBusyBoundsAndConfigChanges(t *testing.T) {
	dir, sink, _ := sinkFixture(t, 2, 512)
	if err := logEvent(t, sink); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "logs", logLock), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = logEvent(t, sink)
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("busy logging blocked or succeeded")
	}
	unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	large := goodEvent()
	large.Counts = map[Metric]int64{}
	for name := range numericMetrics {
		large.Counts[name] = maxMetricValue
	}
	raw, err := encodeEvent(large, worktree, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = sink.appendEvent(context.Background(), raw); err == nil {
		t.Fatal("oversize event accepted")
	}
	if err = logEvent(t, sink); err != nil {
		t.Fatalf("oversize corrupted log: %v", err)
	}
	other := *sink
	other.files = 1
	if err = logEvent(t, &other); err == nil {
		t.Fatal("unreviewed retention change accepted")
	}
	if _, err = NewFileSink(sink.root, sink.lease, worktree, sink.authorize, 129, 1024); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unsupported config accepted")
	}
}

func TestFileSinkRejectsLinkedDirectoriesAndLocks(t *testing.T) {
	for _, kind := range []string{"directory", "lock"} {
		t.Run(kind, func(t *testing.T) {
			dir, sink, _ := sinkFixture(t, 2, 1024)
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("do not change"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if err := os.Symlink(outside, filepath.Join(dir, "logs")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(dir, "logs"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(sentinel, filepath.Join(dir, "logs", logLock)); err != nil {
					t.Fatal(err)
				}
			}
			if err := logEvent(t, sink); err == nil {
				t.Fatal("unsafe path accepted")
			}
			raw, err := os.ReadFile(sentinel)
			if err != nil || string(raw) != "do not change" {
				t.Fatal("outside sentinel changed")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 {
				t.Fatal("outside directory modified")
			}
		})
	}
}

func TestFileSinkDirectoryPermissionFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the fixture's permission restriction")
	}
	dir, sink, _ := sinkFixture(t, 2, 1024)
	logs := filepath.Join(dir, "logs")
	if err := os.Mkdir(logs, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logs, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(logs, 0700)
	if err := logEvent(t, sink); err == nil {
		t.Fatal("write to read-only directory succeeded")
	}
	entries, err := os.ReadDir(logs)
	if err != nil || len(entries) != 0 {
		t.Fatal("permission failure wrote data")
	}
}

func TestFileSinkCrossProcess(t *testing.T) {
	if dir := os.Getenv("MCE4D_TELEMETRY_FIXTURE"); dir != "" {
		permit := &atomic.Bool{}
		permit.Store(true)
		sink, _ := fixtureSink(t, dir, 5, 8192, permit)
		for range 10 {
			ok := false
			for range 200 {
				if logEvent(t, sink) == nil {
					ok = true
					break
				}
				time.Sleep(time.Millisecond * 5)
			}
			if !ok {
				t.Fatal("bounded contention retry exhausted")
			}
		}
		return
	}
	dir, _, _ := sinkFixture(t, 5, 8192)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	var outputs [2]bytes.Buffer
	for i := range commands {
		commands[i] = exec.CommandContext(ctx, exe, "-test.run=^TestFileSinkCrossProcess$", "-test.count=1")
		commands[i].Env = append(os.Environ(), "MCE4D_TELEMETRY_FIXTURE="+dir)
		commands[i].Stdout = &outputs[i]
		commands[i].Stderr = &outputs[i]
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range commands {
		if err := c.Wait(); err != nil {
			t.Fatalf("child %d: %v: %s", i, err, outputs[i].String())
		}
	}
	var observed int
	for i := range 5 {
		raw, err := os.ReadFile(filepath.Join(dir, "logs", slotName(i)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var x map[string]any
			if err = json.Unmarshal([]byte(line), &x); err != nil {
				t.Fatal(err)
			}
			if x["phase"] == "query" {
				observed++
			}
		}
	}
	if observed != 20 {
		t.Fatal(fmt.Sprintf("lost events: %d", observed))
	}
}
