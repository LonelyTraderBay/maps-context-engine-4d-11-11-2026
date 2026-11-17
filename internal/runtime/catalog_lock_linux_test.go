//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"golang.org/x/sys/unix"
)

func TestCatalogMutationUsesCatalogLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalog")
	root := lockRoot(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lease, err := AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	writer, err := AcquireCatalogMutation(ctx, lease, lockFixtureJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.CheckWriterRoot(ctx, root, "catalog", lockFixtureJob); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(dir, "catalog.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "writer.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created a worktree lock in catalog")
	}
	if other, err := AcquireCatalogMutation(ctx, lease, lockFixtureB, 25*time.Millisecond); other != nil || !errors.Is(err, ErrWorktreeBusy) {
		t.Fatalf("catalog writers did not contend: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := AcquireCatalogMutation(ctx, lease, lockFixtureB, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(dir, "catalog.lock"))
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("catalog lock inode replaced")
	}
}

func TestCatalogMutationRefusesLegacyLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "legacy")
	root := lockRoot(t, dir)
	legacyPath := filepath.Join(dir, "writer.lock")
	legacy, err := os.OpenFile(legacyPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err := legacy.Write([]byte("synthetic legacy marker")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(legacy.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lease, err := AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if writer, err := AcquireCatalogMutation(ctx, lease, lockFixtureJob, time.Second); writer != nil || !errors.Is(err, ErrCatalogLockLegacy) {
		t.Fatalf("accepted incompatible lock protocol: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created competing lock protocol")
	}
	probe, err := os.OpenFile(legacyPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
		t.Fatalf("legacy lock released by probe: %v", err)
	}
	raw, err := os.ReadFile(legacyPath)
	if err != nil || string(raw) != "synthetic legacy marker" {
		t.Fatal("altered legacy state")
	}
}
