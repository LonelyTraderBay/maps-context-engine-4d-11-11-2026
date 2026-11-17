//go:build linux

package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// Closing any ordinary descriptor for a file can remove this process's POSIX
// locks on that inode. Observation must not remove locks held by SQLite.
func TestStateObservationPreservesSQLiteLocks(t *testing.T) {
	// The driver's OFD switch is process-wide and fixed at the first lock.
	// Run this oracle in a fresh process with POSIX locking explicitly selected.
	if os.Getenv("MCE4D_STATE_LOCK_SUITE") != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestStateObservationPreservesSQLiteLocks$", "-test.timeout=18s")
		cmd.Env = append(os.Environ(), "MCE4D_STATE_LOCK_SUITE=1", "MODERNC_SQLITE_OFD_LOCK=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("POSIX lock oracle failed: %v: %s", err, out)
		}
		return
	}
	for _, name := range []string{"catalog.sqlite", "catalog.sqlite-shm"} {
		t.Run(name, func(t *testing.T) {
			ctx, root, protected, lease, state, target := stateObservationFixture(t)
			var random [16]byte
			if _, err := rand.Read(random[:]); err != nil {
				t.Fatal(err)
			}
			nonce := hex.EncodeToString(random[:])
			if err := os.WriteFile(filepath.Join(state, "wal-memory.owner"), []byte(nonce), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(state, "catalog.sqlite")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", walOracleDSN(path, "unix", "rwc"))
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			defer db.Close()
			if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE fixture(n INTEGER); BEGIN IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"catalog.sqlite", "catalog.sqlite-wal", "catalog.sqlite-shm"} {
				if err := os.Chmod(filepath.Join(state, name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			probe := func() string {
				t.Helper()
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(ctx, binary, "-test.run=^TestStateObservationLockProbeChild$", "-test.timeout=5s")
				cmd.Env = append(os.Environ(), "MCE4D_STATE_LOCK_PROBE=1", "MCE4D_MEMFD_ORACLE_STATE="+state, "MCE4D_MEMFD_ORACLE_TARGET="+target, "MCE4D_MEMFD_ORACLE_NONCE="+nonce, "MCE4D_STATE_LOCK_NAME="+name)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("lock probe failed: %v: %s", err, out)
				}
				result := strings.TrimSpace(string(out))
				if !strings.HasSuffix(result, "\nPASS") {
					t.Fatalf("unexpected probe output: %q", result)
				}
				return strings.TrimSuffix(result, "\nPASS")
			}
			if got := probe(); got != "BUSY" {
				t.Fatalf("SQLite did not hold its initial lock: %q", got)
			}
			obs, err := ObserveStateFiles(ctx, root, []*paths.Root{protected}, lease, "catalog", "catalog.sqlite", 16<<20, allowState)
			if err != nil {
				t.Fatal(err)
			}
			defer obs.Close()
			if got := probe(); got != "BUSY" {
				t.Fatalf("observation dropped SQLite lock: %q", got)
			}
			if err := obs.Recheck(ctx); err != nil {
				t.Fatal(err)
			}
			if got := probe(); got != "BUSY" {
				t.Fatalf("recheck dropped SQLite lock: %q", got)
			}
			if err := obs.Close(); err != nil {
				t.Fatal(err)
			}
			if got := probe(); got != "BUSY" {
				t.Fatalf("closing observation dropped SQLite lock: %q", got)
			}
			if _, err := db.ExecContext(ctx, "ROLLBACK"); err != nil {
				t.Fatal(err)
			}
			if name == "catalog.sqlite" {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if got := probe(); got != "ACQUIRED" {
				t.Fatalf("lock was not released by SQLite: %q", got)
			}
		})
	}
}

func TestStateObservationLockProbeChild(t *testing.T) {
	if os.Getenv("MCE4D_STATE_LOCK_PROBE") != "1" {
		return
	}
	if err := runStateObservationLockProbe(); err != nil {
		t.Fatal(err)
	}
}

func runStateObservationLockProbe() error {
	root, protected, err := openWALIndexOracleFixture()
	if err != nil {
		return err
	}
	defer root.Close()
	defer protected.Close()
	name := os.Getenv("MCE4D_STATE_LOCK_NAME")
	var start, length int64
	switch name {
	case "catalog.sqlite":
		start, length = 0x40000002, 510
	case "catalog.sqlite-shm":
		start, length = 120, 1
	default:
		return ErrStateUnsafe
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer dir.Close()
	how := &unix.OpenHow{Flags: unix.O_RDWR | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV}
	fd, err := unix.Openat2(int(dir.Fd()), name, how)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "fixture-lock-probe")
	if file == nil {
		_ = unix.Close(fd)
		return ErrStateUnsafe
	}
	defer file.Close()
	if err := stateOwner(file, false); err != nil || root.Verify() != nil {
		return ErrStateUnsafe
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(io.SeekStart), Start: start, Len: length}
	err = unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lock)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) {
		fmt.Println("BUSY")
		return nil
	}
	if err != nil {
		return err
	}
	lock.Type = unix.F_UNLCK
	if err = unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lock); err != nil {
		return err
	}
	fmt.Println("ACQUIRED")
	return nil
}
