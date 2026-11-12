//go:build linux

package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const lockFixtureA = "11111111-1111-4111-8111-111111111111"
const lockFixtureB = "22222222-2222-4222-8222-222222222222"
const lockFixtureJob = "33333333-3333-4333-8333-333333333333"

func lockRoot(t *testing.T, name string) *paths.Root {
	t.Helper()
	if err := os.Mkdir(name, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestLifecycleAttachmentsAndWriterScope(t *testing.T) {
	root := lockRoot(t, filepath.Join(t.TempDir(), "a"))
	ctx := context.Background()
	lifecycle, err := AcquireLifecycle(ctx, root, lockFixtureA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Check(ctx, lockFixtureA, ports.ExclusiveLease); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("shared lease satisfied exclusive request: %v", err)
	}
	hold, err := lifecycle.Hold(ctx)
	if err != nil {
		t.Fatal(err)
	}
	copiedHold := *hold
	if err := lifecycle.Close(); !errors.Is(err, ErrLifecycleInUse) {
		t.Fatalf("lease closed with an attached pool: %v", err)
	}
	writer, err := AcquireWriter(ctx, lifecycle, lockFixtureJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.CheckWriter(ctx, lockFixtureB, lockFixtureJob); err == nil {
		t.Fatal("writer accepted another worktree")
	}
	if err := writer.CheckWriter(ctx, lockFixtureA, lockFixtureB); err == nil {
		t.Fatal("writer accepted another job")
	}
	if err := writer.CheckWriter(ctx, lockFixtureA, lockFixtureJob); err != nil {
		t.Fatal(err)
	}
	guard, err := writer.HoldWriter(ctx, lockFixtureA, lockFixtureJob)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); !errors.Is(err, ErrWriterInUse) {
		t.Fatalf("writer closed during a transaction: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := hold.Close(); err != nil {
		t.Fatal(err)
	}
	if err := copiedHold.Close(); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("copied attachment was released twice: %v", err)
	}
	if err := lifecycle.Close(); !errors.Is(err, ErrLifecycleInUse) {
		t.Fatalf("lease closed before its writer: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Check(ctx, lockFixtureA, ports.SharedLease); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("closed lease accepted: %v", err)
	}
}

func TestIndexStreamReservationIsSharedByWriterCopies(t *testing.T) {
	root := lockRoot(t, filepath.Join(t.TempDir(), "stream"))
	ctx := context.Background()
	lifecycle, err := AcquireLifecycle(ctx, root, lockFixtureA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := AcquireWriter(ctx, lifecycle, lockFixtureJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	copyOfWriter := *writer
	guard, err := writer.HoldIndexStream(ctx, lockFixtureA, lockFixtureJob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copyOfWriter.HoldIndexStream(ctx, lockFixtureA, lockFixtureJob); !errors.Is(err, ports.ErrIndexStreamBusy) {
		t.Fatalf("copied writer acquired second stream: %v", err)
	}
	if err := writer.Close(); !errors.Is(err, ErrWriterInUse) {
		t.Fatalf("writer closed with active stream: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("stream released twice: %v", err)
	}
	guard, err = copyOfWriter.HoldIndexStream(ctx, lockFixtureA, lockFixtureJob)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleRejectsReplacedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	root := lockRoot(t, path)
	lifecycle, err := AcquireLifecycle(context.Background(), root, lockFixtureA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	if err := os.Rename(filepath.Join(path, "lifecycle.lock"), filepath.Join(path, "old.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "lifecycle.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Check(context.Background(), lockFixtureA, ports.SharedLease); !errors.Is(err, ErrLockReplaced) {
		t.Fatalf("replaced lock accepted: %v", err)
	}
}

func TestLockRejectsCancelledAndUnsafeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	root := lockRoot(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireLifecycle(ctx, root, lockFixtureA, ports.SharedLease, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(path, "lifecycle.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled request created lock file: %v", err)
	}
	if err := os.Symlink("other", filepath.Join(path, "lifecycle.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLifecycle(context.Background(), root, lockFixtureA, ports.SharedLease, time.Second); err == nil {
		t.Fatal("symlink lock accepted")
	}
}

func TestLockRejectsDirectoryWithoutOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	root := lockRoot(t, path)
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLifecycle(context.Background(), root, lockFixtureA, ports.SharedLease, time.Second); !errors.Is(err, ErrLockUnsafe) {
		t.Fatalf("non-owner-only lock directory accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(path, "lifecycle.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe lock directory was modified: %v", err)
	}
}

func TestLifecycleRejectsPermissionsChangedAfterAcquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	root := lockRoot(t, path)
	lease, err := AcquireLifecycle(context.Background(), root, lockFixtureA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := lease.Check(context.Background(), lockFixtureA, ports.SharedLease); !errors.Is(err, ErrLockUnsafe) {
		t.Fatalf("directory permission change accepted: %v", err)
	}
}

// The subprocess uses a separate kernel file description and exits without
// cleanup when killed; these tests exercise OS locks, not an in-memory mutex.
func TestLockSubprocess(t *testing.T) {
	if os.Getenv("MCE4D_LOCK_HELPER") != "1" {
		return
	}
	root, err := paths.OpenRoot(os.Getenv("MCE4D_LOCK_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	mode := ports.SharedLease
	if os.Getenv("MCE4D_LOCK_MODE") == "exclusive" {
		mode = ports.ExclusiveLease
	}
	lifecycle, err := AcquireLifecycle(context.Background(), root, lockFixtureA, mode, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	if os.Getenv("MCE4D_LOCK_MODE") == "writer" {
		writer, err := AcquireWriter(context.Background(), lifecycle, lockFixtureJob, 120*time.Millisecond)
		if errors.Is(err, ErrWorktreeBusy) {
			fmt.Fprintln(os.Stdout, "BUSY")
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
	}
	fmt.Fprintln(os.Stdout, "READY")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

func startLockHelper(t *testing.T, dir, mode string) (*exec.Cmd, string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestLockSubprocess$")
	cmd.Env = append(os.Environ(), "MCE4D_LOCK_HELPER=1", "MCE4D_LOCK_DIR="+dir, "MCE4D_LOCK_MODE="+mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("helper failed before response: %q %v", line, err)
	}
	return cmd, line
}

func TestLocksCoordinateProcessesAndWorktrees(t *testing.T) {
	base := t.TempDir()
	aPath := filepath.Join(base, "a")
	a := lockRoot(t, aPath)
	b := lockRoot(t, filepath.Join(base, "b"))
	ctx := context.Background()
	shared, err := AcquireLifecycle(ctx, a, lockFixtureA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	helper, line := startLockHelper(t, aPath, "shared")
	if line != "READY\n" {
		t.Fatalf("shared process: %q", line)
	}
	started := time.Now()
	if _, err := AcquireLifecycle(ctx, a, lockFixtureA, ports.ExclusiveLease, 120*time.Millisecond); !errors.Is(err, ErrStoreMaintenance) {
		t.Fatalf("exclusive lifecycle should be busy: %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("lock wait was not bounded")
	}
	writer, err := AcquireWriter(ctx, shared, lockFixtureJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	blocked, line := startLockHelper(t, aPath, "writer")
	if line != "BUSY\n" {
		t.Fatalf("same worktree writer: %q", line)
	}
	if err := blocked.Wait(); err != nil {
		t.Fatal(err)
	}
	other, err := AcquireLifecycle(ctx, b, lockFixtureB, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherWriter, err := AcquireWriter(ctx, other, lockFixtureJob, time.Second)
	if err != nil {
		t.Fatalf("independent worktree blocked: %v", err)
	}
	if err := otherWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()
}

func TestLifecycleProcessExitReleasesOSLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	root := lockRoot(t, path)
	helper, line := startLockHelper(t, path, "exclusive")
	if line != "READY\n" {
		t.Fatalf("exclusive process: %q", line)
	}
	if _, err := AcquireLifecycle(context.Background(), root, lockFixtureA, ports.SharedLease, 120*time.Millisecond); !errors.Is(err, ErrStoreMaintenance) {
		t.Fatalf("shared lease entered exclusive maintenance: %v", err)
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()
	lease, err := AcquireLifecycle(context.Background(), root, lockFixtureA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatalf("OS did not release exited process lock: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}
