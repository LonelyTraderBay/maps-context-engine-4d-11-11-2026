package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"golang.org/x/sys/windows"
)

const (
	windowsScopeA = "11111111-1111-4111-8111-111111111111"
	windowsScopeB = "22222222-2222-4222-8222-222222222222"
	windowsJob    = "33333333-3333-4333-8333-333333333333"
)

func windowsLeaseFixture(t *testing.T, extraACE string) *paths.Root {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private")
	owner, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := owner.User.Sid.String()
	descriptor, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;FA;;;SY)(A;;FA;;;" + sid + ")" + extraACE)
	if err != nil {
		t.Fatal(err)
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.CreateDirectory(name, &attributes); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func TestWindowsLeaseChild(t *testing.T) {
	path := os.Getenv("MCE4D_WINDOWS_LEASE_CHILD")
	if path == "" {
		return
	}
	root, err := paths.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	reader, err := AcquireLifecycle(ctx, root, windowsScopeA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := AcquireWriter(ctx, reader, windowsJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	fmt.Println("LEASE_HELD")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestWindowsLeasesAcrossProcessesAndWorktrees(t *testing.T) {
	a, b := windowsLeaseFixture(t, ""), windowsLeaseFixture(t, "")
	path, err := a.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestWindowsLeaseChild$", "-test.timeout=9s")
	child.Env = append(os.Environ(), "MCE4D_WINDOWS_LEASE_CHILD="+path)
	keepOpen, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer keepOpen.Close()
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	if line, err := bufio.NewReader(output).ReadString('\n'); line != "LEASE_HELD\n" || err != nil {
		data, _ := io.ReadAll(output)
		t.Fatalf("child could not acquire the owner lease: %q %q, %v", line, data, err)
	}
	reader, err := AcquireLifecycle(ctx, a, windowsScopeA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := AcquireWriter(ctx, reader, windowsJob, 70*time.Millisecond); !errors.Is(err, ErrWorktreeBusy) {
		t.Fatalf("writer conflict returned %v", err)
	}
	if _, err := AcquireLifecycle(ctx, a, windowsScopeA, ports.ExclusiveLease, 70*time.Millisecond); !errors.Is(err, ErrStoreMaintenance) {
		t.Fatalf("reader conflict returned %v", err)
	}
	other, err := AcquireLifecycle(ctx, b, windowsScopeB, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatalf("unrelated worktree could not proceed: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("fixture child exited normally")
	}
	reopened, err := AcquireLifecycle(ctx, a, windowsScopeA, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatalf("terminated process kept the lease: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsLeaseRejectsWiderACL(t *testing.T) {
	root := windowsLeaseFixture(t, "(A;;FR;;;WD)")
	if _, err := AcquireLifecycle(context.Background(), root, windowsScopeA, ports.SharedLease, time.Second); !errors.Is(err, ErrLockUnsafe) {
		t.Fatalf("broader root ACL returned %v", err)
	}
	if _, err := root.OpenRegular("lifecycle.lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refusal created a lock file: %v", err)
	}
}
