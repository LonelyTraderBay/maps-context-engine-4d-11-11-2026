//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

func TestConfigReadWaitsForLiveSetterAndPreservesOrphanPending(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	store, err := NewRuntimeConfigStore(root, "runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := store.Read(context.Background(), 4096); err != nil || present {
		t.Fatal("empty configuration read", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("read created state")
	}
	lock, err := os.OpenFile(filepath.Join(dir, runtimeConfigLock), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	pending := filepath.Join(dir, runtimeConfigPendingName)
	want := []byte(`{"schema_version":1,"core":{"max_file_bytes":4096}}`)
	if err := os.WriteFile(pending, want, 0600); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if data, present, err := store.Read(short, 4096); !errors.Is(err, context.DeadlineExceeded) || present || data != nil {
		t.Fatal("reader did not wait for writer lock", err)
	}
	if after, err := os.ReadFile(pending); err != nil || string(after) != string(want) {
		t.Fatal("reader changed pending data")
	}
	if err := os.Rename(pending, filepath.Join(dir, "runtime.json")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if data, present, err := store.Read(context.Background(), 4096); err != nil || !present || string(data) != string(want) {
		t.Fatal("reader missed committed configuration", err)
	}
	if err := os.WriteFile(pending, want, 0600); err != nil {
		t.Fatal(err)
	}
	if data, present, err := store.Read(context.Background(), 4096); !errors.Is(err, ErrConfigPending) || present || data != nil {
		t.Fatal("orphan pending did not require recovery", err)
	}
	if after, err := os.ReadFile(pending); err != nil || string(after) != string(want) {
		t.Fatal("orphan pending was changed")
	}
}
