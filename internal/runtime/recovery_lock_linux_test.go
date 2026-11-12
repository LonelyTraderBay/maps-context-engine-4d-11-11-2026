//go:build linux

package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func TestRecoveryWriterRequiresRealPriorWriterRelease(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	root := lockRoot(t, dir)
	ctx := context.Background()
	lease, err := AcquireLifecycle(ctx, root, lockFixtureA, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	child, line := startLockHelper(t, dir, "writer")
	if line != "READY\n" {
		t.Fatalf("writer did not start: %q", line)
	}
	if _, err := AcquireRecoveryWriter(ctx, lease, lockFixtureB, 120*time.Millisecond); !errors.Is(err, ErrWorktreeBusy) {
		t.Fatalf("recovery stole live writer: %v", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("child unexpectedly succeeded")
	}
	recovery, err := AcquireRecoveryWriter(ctx, lease, lockFixtureB, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := recovery.HoldRecovery(ctx, lockFixtureA, lockFixtureB)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.Close(); !errors.Is(err, ErrWriterInUse) {
		t.Fatalf("released recovery during use: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recovery.CheckRecovery(ctx, lockFixtureA, lockFixtureB); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("closed recovery remained valid: %v", err)
	}
}
